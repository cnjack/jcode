// js/nn/cpu.js
// CPU backend: hand-written conv / pool / dense forward + backward kernels on
// Float32Array. Each layer exposes forward(x), backward(dy), getParams() so a
// future GPU backend can implement the same interface.
//
// Performance notes:
//   * NO im2col — convolution is computed directly against the (virtually
//     padded) input, which avoids building a large column matrix every batch.
//   * Buffers (output, input gradient, masks, argmax, probs, ...) are allocated
//     lazily and REUSED across batches (grown only when the batch grows). This
//     removes per-batch allocation / GC pressure from the training hot path.
//   * Inner loops are the kernel/spatial loops with hoisted index arithmetic so
//     weight reads stay in L1/L2 cache.
//
// Tensor convention: { data: Float32Array, shape: number[] }.
// 4-D activations use NCHW layout (n,c,h,w contiguous).

// Return a Float32Array of EXACTLY `len` elements, reusing `arr` only when its
// length already matches. We never keep an over-sized buffer around, because
// several layers (Flatten, ReLU) derive element counts from `x.data.length` —
// an oversized buffer (e.g. left over from a batch-256 eval pass) would make
// them read stale/garbage elements and corrupt the next training step.
// The training hot path uses a constant batch size, so buffers are reused every
// step; only the (rare) eval passes reallocate.
function ensure(arr, len) {
  if (!arr || arr.length !== len) return new Float32Array(len);
  return arr;
}

// Product of all but the first (batch) dims — shape-derived element count.
function flatLen(shape) {
  let n = 1;
  for (let i = 0; i < shape.length; i++) n *= shape[i];
  return n;
}

// ---------------------------------------------------------------------------
// Convolution 2D: stride/pad supported. Direct convolution (no im2col).
// Padding is handled implicitly with out-of-bounds skips in the inner loop.
// ---------------------------------------------------------------------------
export class Conv2D {
  constructor(inCh, outCh, k = 3, stride = 1, pad = 1, rng) {
    this.inCh = inCh;
    this.outCh = outCh;
    this.k = k;
    this.stride = stride;
    this.pad = pad;
    const fanIn = inCh * k * k;
    const std = Math.sqrt(2 / fanIn); // He initialization
    this.W = new Float32Array(outCh * inCh * k * k); // [F, C, k, k]
    for (let i = 0; i < this.W.length; i++) this.W[i] = rng.gauss(0, std);
    this.b = new Float32Array(outCh); // zeros
    this.gW = new Float32Array(this.W.length);
    this.gb = new Float32Array(outCh);
    this.vW = new Float32Array(this.W.length); // momentum velocity
    this.vb = new Float32Array(outCh);
    // reusable buffers
    this._out = null;
    this._dx = null;
    this._x = null;
  }

  getParams() {
    return [
      { name: 'W', value: this.W, grad: this.gW, vel: this.vW },
      { name: 'b', value: this.b, grad: this.gb, vel: this.vb },
    ];
  }

  forward(x) {
    const [N, C, H, Wd] = x.shape;
    const { k, stride, pad, outCh: F } = this;
    const outH = (((H + 2 * pad - k) / stride) | 0) + 1;
    const outW = (((Wd + 2 * pad - k) / stride) | 0) + 1;
    this.inShape = x.shape;
    this._x = x; // remember input for backward
    this._N = N;
    this._C = C;
    this._H = H;
    this._W = Wd;
    this._outH = outH;
    this._outW = outW;

    const OHOW = outH * outW;
    const FOHOW = F * OHOW;
    const out = ensure(this._out, N * FOHOW);
    this._out = out;
    const xd = x.data;
    const W = this.W;
    const b = this.b;

    for (let n = 0; n < N; n++) {
      const nInOff = n * C * H * Wd; // input sample base
      const nOutOff = n * FOHOW; // output sample base
      for (let f = 0; f < F; f++) {
        const wOff = f * C * k * k;
        const bias = b[f];
        const fOutOff = nOutOff + f * OHOW;
        for (let oy = 0; oy < outH; oy++) {
          const oyBase = oy * stride - pad;
          for (let ox = 0; ox < outW; ox++) {
            let s = bias;
            const oxBase = ox * stride - pad;
            for (let c = 0; c < C; c++) {
              const cInOff = nInOff + c * H * Wd;
              const wco = wOff + c * k * k;
              for (let ky = 0; ky < k; ky++) {
                const iy = oyBase + ky;
                if (iy < 0 || iy >= H) continue;
                const iyRow = iy * Wd;
                const wky = wco + ky * k;
                for (let kx = 0; kx < k; kx++) {
                  const ix = oxBase + kx;
                  if (ix < 0 || ix >= Wd) continue;
                  s += W[wky + kx] * xd[cInOff + iyRow + ix];
                }
              }
            }
            out[fOutOff + oy * outW + ox] = s;
          }
        }
      }
    }
    return { data: out, shape: [N, F, outH, outW] };
  }

  backward(dy) {
    const { k, stride, pad, outCh: F, _N: N, _C: C, _H: H, _W: Wd, _outH: outH, _outW: outW } = this;
    const OHOW = outH * outW;
    const FOHOW = F * OHOW;
    const x = this._x;
    const xd = x.data;
    const W = this.W;
    const gW = this.gW;
    const gb = this.gb;
    gW.fill(0);
    gb.fill(0);
    const dx = ensure(this._dx, N * C * H * Wd);
    this._dx = dx;
    dx.fill(0);

    // Single pass over the output: accumulates gb, gW and scatters into dx.
    // dx[n,c,iy,ix] += sum_f sum_{(oy,ox,ky,kx) -> (iy,ix)} W[f,c,ky,kx]*dy[n,f,oy,ox]
    // gW[f,c,ky,kx] += sum_{n,oy,ox} dy[n,f,oy,ox] * x[n,c,iy,ix]
    for (let n = 0; n < N; n++) {
      const nInOff = n * C * H * Wd;
      const nDyOff = n * FOHOW;
      for (let f = 0; f < F; f++) {
        const wOff = f * C * k * k;
        const fDyOff = nDyOff + f * OHOW;
        for (let oy = 0; oy < outH; oy++) {
          const oyBase = oy * stride - pad;
          const oyRow = oy * outW;
          for (let ox = 0; ox < outW; ox++) {
            const g = dy.data[fDyOff + oyRow + ox];
            if (g === 0) continue; // common for ReLU-masked routes
            gb[f] += g;
            const oxBase = ox * stride - pad;
            for (let c = 0; c < C; c++) {
              const cInOff = nInOff + c * H * Wd;
              const wco = wOff + c * k * k;
              for (let ky = 0; ky < k; ky++) {
                const iy = oyBase + ky;
                if (iy < 0 || iy >= H) continue;
                const iyRow = iy * Wd;
                const wky = wco + ky * k;
                for (let kx = 0; kx < k; kx++) {
                  const ix = oxBase + kx;
                  if (ix < 0 || ix >= Wd) continue;
                  const xv = xd[cInOff + iyRow + ix];
                  gW[wky + kx] += g * xv;
                  dx[cInOff + iyRow + ix] += W[wky + kx] * g;
                }
              }
            }
          }
        }
      }
    }
    return { data: dx, shape: [N, C, H, Wd] };
  }
}

// ---------------------------------------------------------------------------
// ReLU activation.
// ---------------------------------------------------------------------------
export class ReLU {
  getParams() {
    return [];
  }
  forward(x) {
    const len = flatLen(x.shape);
    const m = this._mask && this._mask.length === len ? this._mask : new Uint8Array(len);
    this._mask = m;
    const out = ensure(this._out, len);
    this._out = out;
    const d = x.data;
    for (let i = 0; i < len; i++) {
      const v = d[i];
      const on = v > 0;
      out[i] = on ? v : 0;
      m[i] = on ? 1 : 0;
    }
    return { data: out, shape: x.shape.slice() };
  }
  backward(dy) {
    const len = flatLen(dy.shape);
    const out = ensure(this._dx, len);
    this._dx = out;
    const m = this._mask;
    const d = dy.data;
    for (let i = 0; i < len; i++) out[i] = d[i] * m[i];
    return { data: out, shape: dy.shape.slice() };
  }
}

// ---------------------------------------------------------------------------
// Max pooling (k x k, given stride). Stores argmax for the backward route.
// ---------------------------------------------------------------------------
export class MaxPool2D {
  constructor(k = 2, stride = 2) {
    this.k = k;
    this.stride = stride;
  }
  getParams() {
    return [];
  }
  forward(x) {
    const [N, C, H, Wd] = x.shape;
    const { k, stride } = this;
    const outH = (((H - k) / stride) | 0) + 1;
    const outW = (((Wd - k) / stride) | 0) + 1;
    const HW = H * Wd;
    const OHOW = outH * outW;
    const out = ensure(this._out, N * C * OHOW);
    this._out = out;
    const argmax =
      this._argmax && this._argmax.length === N * C * OHOW
        ? this._argmax
        : new Int32Array(N * C * OHOW);
    this._argmax = argmax;
    this.inShape = x.shape.slice();
    this._outH = outH;
    this._outW = outW;
    const xd = x.data;
    for (let n = 0; n < N; n++) {
      for (let c = 0; c < C; c++) {
        const plane = n * C * HW + c * HW;
        const op = n * C * OHOW + c * OHOW;
        for (let oy = 0; oy < outH; oy++) {
          const oyRow = oy * outW;
          for (let ox = 0; ox < outW; ox++) {
            let best = -Infinity;
            let bi = -1;
            for (let ky = 0; ky < k; ky++) {
              const rowBase = plane + (oy * stride + ky) * Wd;
              for (let kx = 0; kx < k; kx++) {
                const idx = rowBase + ox * stride + kx;
                const v = xd[idx];
                if (v > best) {
                  best = v;
                  bi = idx;
                }
              }
            }
            out[op + oyRow + ox] = best;
            argmax[op + oyRow + ox] = bi;
          }
        }
      }
    }
    return { data: out, shape: [N, C, outH, outW] };
  }
  backward(dy) {
    const [N, C, H, Wd] = this.inShape;
    const { _outH: outH, _outW: outW } = this;
    const HW = H * Wd;
    const OHOW = outH * outW;
    const out = ensure(this._dx, N * C * HW);
    this._dx = out;
    out.fill(0);
    const argmax = this._argmax;
    const dd = dy.data;
    for (let n = 0; n < N; n++) {
      for (let c = 0; c < C; c++) {
        const op = n * C * OHOW + c * OHOW;
        for (let i = 0; i < OHOW; i++) {
          const g = dd[op + i];
          if (g !== 0) out[argmax[op + i]] += g;
        }
      }
    }
    return { data: out, shape: [N, C, H, Wd] };
  }
}

// ---------------------------------------------------------------------------
// Flatten: reshape NCHW -> [N, C*H*W] (no copy; data layout is identical).
// ---------------------------------------------------------------------------
export class Flatten {
  getParams() {
    return [];
  }
  forward(x) {
    this.inShape = x.shape.slice();
    const N = x.shape[0];
    // Derive the flattened width from the shape (not data.length, which may be a
    // reused buffer of a different batch size).
    let rest = 1;
    for (let i = 1; i < x.shape.length; i++) rest *= x.shape[i];
    return { data: x.data, shape: [N, rest] };
  }
  backward(dy) {
    return { data: dy.data, shape: this.inShape.slice() };
  }
}

// ---------------------------------------------------------------------------
// Fully connected / dense layer.
// ---------------------------------------------------------------------------
export class Dense {
  constructor(inN, outN, rng) {
    this.inN = inN;
    this.outN = outN;
    const std = Math.sqrt(2 / inN); // He init
    this.W = new Float32Array(outN * inN); // [outN, inN]
    for (let i = 0; i < this.W.length; i++) this.W[i] = rng.gauss(0, std);
    this.b = new Float32Array(outN);
    this.gW = new Float32Array(outN * inN);
    this.gb = new Float32Array(outN);
    this.vW = new Float32Array(outN * inN);
    this.vb = new Float32Array(outN);
    this._out = null;
    this._dx = null;
  }
  getParams() {
    return [
      { name: 'W', value: this.W, grad: this.gW, vel: this.vW },
      { name: 'b', value: this.b, grad: this.gb, vel: this.vb },
    ];
  }
  forward(x) {
    const [N, inN] = x.shape;
    const { outN, W, b } = this;
    this._x = x;
    this._N = N;
    const out = ensure(this._out, N * outN);
    this._out = out;
    const xd = x.data;
    for (let n = 0; n < N; n++) {
      const xo = n * inN;
      const oo = n * outN;
      for (let o = 0; o < outN; o++) {
        const wo = o * inN;
        let s = b[o];
        for (let i = 0; i < inN; i++) s += W[wo + i] * xd[xo + i];
        out[oo + o] = s;
      }
    }
    return { data: out, shape: [N, outN] };
  }
  backward(dy) {
    const [N, inN] = this._x.shape;
    const { outN, W, gW, gb } = this;
    gW.fill(0);
    gb.fill(0);
    const dd = dy.data;
    // gradients w.r.t. weights/biases
    for (let n = 0; n < N; n++) {
      const xo = n * inN;
      const oo = n * outN;
      for (let o = 0; o < outN; o++) {
        const g = dd[oo + o];
        if (g === 0) continue;
        gb[o] += g;
        const wo = o * inN;
        for (let i = 0; i < inN; i++) gW[wo + i] += g * this._x.data[xo + i];
      }
    }
    // gradient w.r.t. input
    const dx = ensure(this._dx, N * inN);
    this._dx = dx;
    for (let n = 0; n < N; n++) {
      const oo = n * outN;
      const xo = n * inN;
      for (let i = 0; i < inN; i++) {
        let s = 0;
        for (let o = 0; o < outN; o++) s += dd[oo + o] * W[o * inN + i];
        dx[xo + i] = s;
      }
    }
    return { data: dx, shape: [N, inN] };
  }
}

// ---------------------------------------------------------------------------
// Softmax + cross-entropy combined for numerical stability.
// forward(logits, labels) -> { loss, probs }; backward() -> dlogits.
// ---------------------------------------------------------------------------
export class SoftmaxCE {
  getParams() {
    return [];
  }
  forward(logits, labels) {
    const [N, K] = logits.shape;
    this._N = N;
    this._K = K;
    this._labels = labels;
    const probs = ensure(this._probs, N * K);
    this._probs = probs;
    const ld = logits.data;
    let loss = 0;
    for (let n = 0; n < N; n++) {
      const off = n * K;
      let max = -Infinity;
      for (let k = 0; k < K; k++) {
        const v = ld[off + k];
        if (v > max) max = v;
      }
      let sum = 0;
      for (let k = 0; k < K; k++) {
        const e = Math.exp(ld[off + k] - max);
        probs[off + k] = e;
        sum += e;
      }
      const inv = 1 / sum;
      for (let k = 0; k < K; k++) probs[off + k] *= inv;
      loss += -Math.log(probs[off + labels[n]] + 1e-12);
    }
    return { loss: loss / N, probs };
  }
  backward() {
    const { _N: N, _K: K, _labels: labels, _probs: probs } = this;
    const dlogits = ensure(this._dx, N * K);
    this._dx = dlogits;
    const inv = 1 / N;
    for (let n = 0; n < N; n++) {
      const off = n * K;
      const lbl = labels[n];
      for (let k = 0; k < K; k++) {
        dlogits[off + k] = (probs[off + k] - (k === lbl ? 1 : 0)) * inv;
      }
    }
    return { data: dlogits, shape: [N, K] };
  }
}

// Backend factory so model.js stays backend-agnostic.
export const cpuBackend = {
  Conv2D: (inCh, outCh, k, stride, pad, rng) =>
    new Conv2D(inCh, outCh, k, stride, pad, rng),
  ReLU: () => new ReLU(),
  MaxPool2D: (k, stride) => new MaxPool2D(k, stride),
  Flatten: () => new Flatten(),
  Dense: (inN, outN, rng) => new Dense(inN, outN, rng),
  SoftmaxCE: () => new SoftmaxCE(),
};
