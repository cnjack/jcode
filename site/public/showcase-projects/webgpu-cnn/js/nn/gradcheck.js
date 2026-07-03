// js/nn/gradcheck.js
// Numerical gradient check: central differences vs analytic gradients for a
// spread of parameters across ALL layers.
//
// Two things make naive finite-difference checking of this network unreliable:
//   1. Float32 noise — finite differences subtract two ~O(1) losses, so for
//      small gradients the result drowns in float32 rounding. We therefore
//      recompute the loss for the numerical estimate in FLOAT64 via an
//      independent forward pass (which also cross-checks the conv math).
//   2. ReLU kinks — the loss is piecewise-linear, so a perturbation that flips
//      a ReLU unit gives a central difference that legitimately disagrees with
//      the analytic sub-gradient. We detect such flips definitively by
//      comparing ReLU activation masks and exclude those parameters from the
//      verdict (reporting them separately as kinked).

import { modelForward, modelBackward } from './model.js';

const EPS = 1e-3;
const MAG_FLOOR = 1e-4; // gradients below this are "~0": relative error is meaningless

// ---- independent float64 forward (mirrors the architecture exactly) ---------
function convF64(x, shape, layer, F, k, stride, pad) {
  const [N, C, H, W] = shape;
  const Wt = layer.W;
  const b = layer.b;
  const outH = (((H + 2 * pad - k) / stride) | 0) + 1;
  const outW = (((W + 2 * pad - k) / stride) | 0) + 1;
  const out = new Float64Array(N * F * outH * outW);
  for (let n = 0; n < N; n++) {
    for (let f = 0; f < F; f++) {
      const bo = n * F * outH * outW + f * outH * outW;
      const wo = f * C * k * k;
      const bias = b[f];
      for (let oy = 0; oy < outH; oy++) {
        for (let ox = 0; ox < outW; ox++) {
          let s = bias;
          for (let c = 0; c < C; c++) {
            const co = n * C * H * W + c * H * W;
            const wco = wo + c * k * k;
            for (let ky = 0; ky < k; ky++) {
              const iy = oy * stride + ky - pad;
              if (iy < 0 || iy >= H) continue;
              for (let kx = 0; kx < k; kx++) {
                const ix = ox * stride + kx - pad;
                if (ix < 0 || ix >= W) continue;
                s += Wt[wco + ky * k + kx] * x[co + iy * W + ix];
              }
            }
          }
          out[bo + oy * outW + ox] = s;
        }
      }
    }
  }
  return { data: out, shape: [N, F, outH, outW] };
}
function reluF64(t, masks) {
  const d = t.data;
  const out = new Float64Array(d.length);
  const m = new Uint8Array(d.length);
  for (let i = 0; i < d.length; i++) {
    const v = d[i];
    out[i] = v > 0 ? v : 0;
    m[i] = v > 0 ? 1 : 0;
  }
  masks.push(m);
  return { data: out, shape: t.shape };
}
function maxpoolF64(t, k, stride, masks) {
  const [N, C, H, W] = t.shape;
  const outH = (((H - k) / stride) | 0) + 1;
  const outW = (((W - k) / stride) | 0) + 1;
  const out = new Float64Array(N * C * outH * outW);
  const winners = new Uint8Array(N * C * outH * outW); // local winner index 0..k*k-1
  for (let n = 0; n < N; n++)
    for (let c = 0; c < C; c++) {
      const plane = n * C * H * W + c * H * W;
      const op = n * C * outH * outW + c * outH * outW;
      for (let oy = 0; oy < outH; oy++)
        for (let ox = 0; ox < outW; ox++) {
          let best = -Infinity;
          let bi = 0;
          let p = 0;
          for (let ky = 0; ky < k; ky++)
            for (let kx = 0; kx < k; kx++) {
              const v = t.data[plane + (oy * stride + ky) * W + (ox * stride + kx)];
              if (v > best) {
                best = v;
                bi = p;
              }
              p++;
            }
          out[op + oy * outW + ox] = best;
          winners[op + oy * outW + ox] = bi;
        }
    }
  masks.push(winners);
  return { data: out, shape: [N, C, outH, outW] };
}
function denseF64(t, layer) {
  const [N, inN] = t.shape;
  const outN = layer.outN;
  const Wt = layer.W;
  const b = layer.b;
  const out = new Float64Array(N * outN);
  for (let n = 0; n < N; n++) {
    const xo = n * inN;
    const oo = n * outN;
    for (let o = 0; o < outN; o++) {
      const wo = o * inN;
      let s = b[o];
      for (let i = 0; i < inN; i++) s += Wt[wo + i] * t.data[xo + i];
      out[oo + o] = s;
    }
  }
  return { data: out, shape: [N, outN] };
}
function softmaxCEF64(t, labels) {
  const [N, K] = t.shape;
  let loss = 0;
  for (let n = 0; n < N; n++) {
    const off = n * K;
    let max = -Infinity;
    for (let k = 0; k < K; k++) if (t.data[off + k] > max) max = t.data[off + k];
    let sum = 0;
    for (let k = 0; k < K; k++) sum += Math.exp(t.data[off + k] - max);
    loss += -Math.log(Math.exp(t.data[off + labels[n]] - max) / sum);
  }
  return loss / N;
}
// Full float64 forward; returns {loss, masks} where masks are ReLU sign masks.
function lossF64(model, x, labels) {
  const r = model.refs;
  const masks = [];
  let a = convF64(x.data, x.shape, r.conv1, 8, 3, 1, 1);
  a = reluF64(a, masks);
  a = maxpoolF64(a, 2, 2, masks);
  a = convF64(a.data, a.shape, r.conv2, 16, 3, 1, 1);
  a = reluF64(a, masks);
  a = maxpoolF64(a, 2, 2, masks);
  // flatten (NCHW -> [N,256])
  a = { data: a.data, shape: [a.shape[0], a.data.length / a.shape[0]] };
  a = denseF64(a, r.dense);
  return { loss: softmaxCEF64(a, labels), masks };
}

// ---- mask change detection -------------------------------------------------
function masksChanged(a, b) {
  if (a.length !== b.length) return true;
  for (let i = 0; i < a.length; i++) {
    const x = a[i];
    const y = b[i];
    if (x.length !== y.length) return true;
    for (let j = 0; j < x.length; j++) if (x[j] !== y[j]) return true;
  }
  return false;
}

// Fixed spread of parameters across every layer (weights + biases).
function selectCandidates(model) {
  const refs = model.refs;
  const c1 = refs.conv1.getParams();
  const c2 = refs.conv2.getParams();
  const dd = refs.dense.getParams();
  return [
    { param: c1[0], idx: 0, label: 'conv1.W[0]' },
    { param: c1[0], idx: 17, label: 'conv1.W[17]' },
    { param: c1[0], idx: 40, label: 'conv1.W[40]' },
    { param: c1[0], idx: 71, label: 'conv1.W[71]' },
    { param: c1[1], idx: 0, label: 'conv1.b[0]' },
    { param: c1[1], idx: 4, label: 'conv1.b[4]' },
    { param: c2[0], idx: 0, label: 'conv2.W[0]' },
    { param: c2[0], idx: 200, label: 'conv2.W[200]' },
    { param: c2[0], idx: 700, label: 'conv2.W[700]' },
    { param: c2[0], idx: 1151, label: 'conv2.W[1151]' },
    { param: c2[1], idx: 0, label: 'conv2.b[0]' },
    { param: c2[1], idx: 9, label: 'conv2.b[9]' },
    { param: dd[0], idx: 0, label: 'dense.W[0]' },
    { param: dd[0], idx: 500, label: 'dense.W[500]' },
    { param: dd[0], idx: 1300, label: 'dense.W[1300]' },
    { param: dd[0], idx: 2559, label: 'dense.W[2559]' },
    { param: dd[1], idx: 0, label: 'dense.b[0]' },
    { param: dd[1], idx: 4, label: 'dense.b[4]' },
    { param: dd[1], idx: 7, label: 'dense.b[7]' },
    { param: dd[1], idx: 9, label: 'dense.b[9]' },
  ];
}

export function gradCheck(model, xBatch, labels, eps = EPS) {
  // Analytic pass (float32) populates gradients.
  const logits = modelForward(model, xBatch);
  model.smce.forward(logits, labels);
  modelBackward(model);
  // Base masks (float64, unperturbed) for kink detection.
  const baseMasks = lossF64(model, xBatch, labels).masks;

  const candidates = selectCandidates(model);
  const details = [];
  let maxRel = 0;
  let sumRel = 0;
  let used = 0;
  let skippedZero = 0;
  let kinked = 0;

  for (const c of candidates) {
    const { param, idx, label } = c;
    const orig = param.value[idx];

    param.value[idx] = orig + eps;
    const up = lossF64(model, xBatch, labels);
    param.value[idx] = orig - eps;
    const dn = lossF64(model, xBatch, labels);
    param.value[idx] = orig; // restore

    const num = (up.loss - dn.loss) / (2 * eps);
    const ana = param.grad[idx];
    const mag = Math.max(Math.abs(num), Math.abs(ana));

    if (masksChanged(baseMasks, up.masks) || masksChanged(baseMasks, dn.masks)) {
      details.push({ label, analytic: ana, numerical: num, relErr: null, kinked: true });
      kinked++;
      continue;
    }
    if (mag < MAG_FLOOR) {
      details.push({ label, analytic: ana, numerical: num, relErr: 0, kinked: false, nearZero: true });
      skippedZero++;
      continue;
    }
    const rel = Math.abs(num - ana) / Math.max(mag, 1e-12);
    details.push({ label, analytic: ana, numerical: num, relErr: rel, kinked: false });
    if (rel > maxRel) maxRel = rel;
    sumRel += rel;
    used++;
  }

  const meanRel = used > 0 ? sumRel / used : 0;
  const pass = used > 0 && maxRel < 1e-2;
  return { maxRelErr: maxRel, meanRelErr: meanRel, pass, used, kinked, skipped: skippedZero, details };
}
