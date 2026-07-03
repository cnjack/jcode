// js/nn/gpu.js
// WebGPU compute backend. Exposes the SAME factory surface as js/nn/cpu.js
// (Conv2D / ReLU / MaxPool2D / Flatten / Dense / SoftmaxCE) so model.js's
// buildModel() can swap backends unchanged. GPU layers have ASYNC forward/
// backward (return Promise<{data: GPUBuffer, shape}>); the worker drives them
// via modelForwardAsync / modelBackwardAsync from model.js, where `await` also
// transparently handles the synchronous CPU layers.
//
// Design:
//   * Weights (W, b), velocities (vW, vb) and grad slots (gW, gb) live in
//     PERSISTENT GPU storage buffers, uploaded once at construction. The
//     GpuMomentumSGD optimizer updates them in place on the GPU, so weights
//     never round-trip to the CPU during training.
//   * Activations are passed between layers as {data: GPUBuffer, shape} — no
//     copies. Readback happens only for the scalar loss (every batch) and for
//     accuracy / parity inspection (rarely).
//   * All forward/backward kernels share one {N:u32} uniform per batch; static
//     layer dims are baked into each WGSL module (see wgsl/kernels.js).
//
// IMPORTANT: this module's top level performs NO WebGPU access and has no side
// effects, so importing it is always safe — even when WebGPU is absent. All GPU
// work is gated behind probeGpu()/init, which fail cleanly and report a reason.

import * as K from './wgsl/kernels.js';

const U = (typeof GPUBufferUsage !== 'undefined') ? GPUBufferUsage : null;

function alignUp(x, a) { return Math.ceil(x / a) * a; }

// Try to acquire a WebGPU device. Never throws — returns {ok, device?, reason}.
export async function probeGpu() {
  try {
    if (typeof navigator === 'undefined' || !navigator.gpu) {
      return { ok: false, reason: 'navigator.gpu undefined' };
    }
    const adapter = await navigator.gpu.requestAdapter();
    if (!adapter) return { ok: false, reason: 'no WebGPU adapter available' };
    let device;
    try {
      device = await adapter.requestDevice();
    } catch (e) {
      return { ok: false, reason: 'requestDevice rejected: ' + (e && e.message ? e.message : String(e)) };
    }
    return { ok: true, device, reason: '' };
  } catch (e) {
    return { ok: false, reason: 'WebGPU init failed: ' + (e && e.message ? e.message : String(e)) };
  }
}

// Low-level device wrapper: pipeline cache, buffer helpers, dispatch.
export class GpuBackend {
  constructor(device) {
    this.device = device;
    this.lost = false;
    this._pipes = new Map();
    this._nBuf = this._uniform16();
    device.lost.then((info) => {
      this.lost = true;
      console.info('[NeuraLab] WebGPU device lost: ' + (info && info.message ? info.message : 'unknown') + ' — training will fall back to CPU on reset.');
    });
  }
  _uniform16() {
    return this.device.createBuffer({ size: 16, usage: U.UNIFORM | U.COPY_DST });
  }
  storage(byteLen) {
    return this.device.createBuffer({ size: alignUp(byteLen, 4), usage: U.STORAGE | U.COPY_SRC | U.COPY_DST });
  }
  upload(buf, view) { this.device.queue.writeBuffer(buf, 0, view); }
  // Write the batch size N into the shared uniform; called once per forward pass.
  writeN(n) {
    const a = new Uint32Array(4);
    a[0] = n >>> 0;
    this.device.queue.writeBuffer(this._nBuf, 0, a);
  }
  // Compile + cache a compute pipeline keyed by its WGSL source.
  pipeline(code) {
    let p = this._pipes.get(code);
    if (!p) {
      const mod = this.device.createShaderModule({ code });
      p = this.device.createComputePipeline({ layout: 'auto', compute: { module: mod, entryPoint: 'main' } });
      this._pipes.set(code, p);
    }
    return p;
  }
  // Encode + submit a single compute dispatch. entries: [{binding, resource:{buffer}}].
  run(code, entries, nWorkgroups) {
    const pipe = this.pipeline(code);
    const bg = this.device.createBindGroup({ layout: pipe.getBindGroupLayout(0), entries });
    const enc = this.device.createCommandEncoder();
    const pass = enc.beginComputePass();
    pass.setPipeline(pipe);
    pass.setBindGroup(0, bg);
    pass.dispatchWorkgroups(Math.max(1, nWorkgroups));
    pass.end();
    this.device.queue.submit([enc.finish()]);
  }
  // Encode + submit several dispatches sharing the SAME bind group layout per code.
  runMany(jobs) {
    const enc = this.device.createCommandEncoder();
    const pass = enc.beginComputePass();
    for (const job of jobs) {
      const pipe = this.pipeline(job.code);
      const bg = this.device.createBindGroup({ layout: pipe.getBindGroupLayout(0), entries: job.entries });
      pass.setPipeline(pipe);
      pass.setBindGroup(0, bg);
      pass.dispatchWorkgroups(Math.max(1, job.nwg));
    }
    pass.end();
    this.device.queue.submit([enc.finish()]);
  }
  async readF32(buf, len) {
    const ab = await this._read(buf, len * 4);
    return new Float32Array(ab);
  }
  async readU32(buf, len) {
    const ab = await this._read(buf, len * 4);
    return new Uint32Array(ab);
  }
  async _read(buf, byteLen) {
    const tmp = this.device.createBuffer({ size: alignUp(byteLen, 4), usage: U.MAP_READ | U.COPY_DST });
    const enc = this.device.createCommandEncoder();
    enc.copyBufferToBuffer(buf, 0, tmp, 0, byteLen);
    this.device.queue.submit([enc.finish()]);
    await tmp.mapAsync(GPUMapMode.READ);
    const ab = tmp.getMappedRange(0, byteLen).slice(0);
    tmp.destroy();
    return ab;
  }
  // Bounding fence: resolves once all submitted work so far is done. Used to keep
  // the per-batch queue from growing without bound (the JS loop otherwise runs
  // ahead of the GPU).
  async fence() {
    if (this.device.queue.onSubmittedWorkDone) await this.device.queue.onSubmittedWorkDone();
  }
  destroy() {
    for (const buf of this._allBufs || []) { try { buf.destroy(); } catch (e) {} }
    this._allBufs = null;
  }
}

// ---------------------------------------------------------------------------
// Conv2D (3x3, pad 1, stride 1). W: [F,C,k,k], b: [F]. Buffers persistent.
// ---------------------------------------------------------------------------
class GConv2D {
  constructor(inCh, outCh, k, stride, pad, rng, gb) {
    this.inCh = inCh; this.outCh = outCh; this.k = k; this.stride = stride; this.pad = pad; this.gb = gb;
    const fanIn = inCh * k * k;
    const std = Math.sqrt(2 / fanIn);
    // CPU-side Float32Array mirrors (source of truth at init; parity inspection).
    this.W = new Float32Array(outCh * inCh * k * k);
    for (let i = 0; i < this.W.length; i++) this.W[i] = rng.gauss(0, std);
    this.b = new Float32Array(outCh);
    this.gW = new Float32Array(this.W.length);
    this.gb_ = new Float32Array(outCh);
    this.vW = new Float32Array(this.W.length);
    this.vb = new Float32Array(outCh);
    // Persistent GPU buffers.
    this._wBuf = gb.storage(this.W.byteLength); gb.upload(this._wBuf, this.W);
    this._bBuf = gb.storage(this.b.byteLength); gb.upload(this._bBuf, this.b);
    this._gWBuf = gb.storage(this.gW.byteLength);
    this._gbBuf = gb.storage(this.gb_.byteLength);
    this._vWBuf = gb.storage(this.vW.byteLength);
    this._vbBuf = gb.storage(this.vb.byteLength);
    this._compiled = null;
    this._outBuf = null; this._dxBuf = null;
  }
  getParams() {
    return [
      { name: 'W', value: this.W, grad: this.gW, vel: this.vW, gpuValue: this._wBuf, gpuGrad: this._gWBuf, gpuVel: this._vWBuf, n: this.W.length },
      { name: 'b', value: this.b, grad: this.gb_, vel: this.vb, gpuValue: this._bBuf, gpuGrad: this._gbBuf, gpuVel: this._vbBuf, n: this.b.length },
    ];
  }
  _compile(C, H, OH, OW) {
    const { outCh: F, k, pad, stride } = this;
    this._F = F; this._C = C; this._H = H; this._OH = OH; this._OW = OW;
    this._codeFwd = K.convForward(C, F, H, H, OH, OW, k, pad, stride);
    this._codeDW = K.convDW(C, F, H, H, OH, OW, k, pad, stride);
    this._codeDB = K.convDB(F, OH, OW);
    this._codeDX = K.convDX(C, F, H, H, OH, OW, k, pad, stride);
    this._compiled = { C, H, OH, OW };
  }
  async forward(x) {
    const [N, C, H, W] = x.shape;
    const OH = (((H + 2 * this.pad - this.k) / this.stride) | 0) + 1;
    const OW = (((W + 2 * this.pad - this.k) / this.stride) | 0) + 1;
    this._x = x; this._N = N;
    if (!this._compiled || this._compiled.H !== H || this._compiled.C !== C) this._compile(C, H, OH, OW);
    const outLen = N * this._F * OH * OW;
    if (!this._outBuf || this._outBuf.size < outLen * 4) {
      if (this._outBuf) this._outBuf.destroy();
      this._outBuf = this.gb.storage(outLen * 4);
    }
    const Nn = this.gb;
    const entries = [
      { binding: 0, resource: { buffer: Nn._nBuf } },
      { binding: 1, resource: { buffer: x.data } },
      { binding: 2, resource: { buffer: this._wBuf } },
      { binding: 3, resource: { buffer: this._bBuf } },
      { binding: 4, resource: { buffer: this._outBuf } },
    ];
    this.gb.run(this._codeFwd, entries, Math.ceil(outLen / 64));
    return { data: this._outBuf, shape: [N, this._F, OH, OW] };
  }
  async backward(dy) {
    const { _F: F, _C: C, _H: H, _OH: OH, _OW: OW, _N: N } = this;
    // dW
    const gWTotal = F * C * this.k * this.k;
    this.gb.run(this._codeDW, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: this._x.data } },
      { binding: 2, resource: { buffer: dy.data } },
      { binding: 3, resource: { buffer: this._gWBuf } },
    ], Math.ceil(gWTotal / 64));
    // db
    this.gb.run(this._codeDB, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: dy.data } },
      { binding: 2, resource: { buffer: this._gbBuf } },
    ], Math.ceil(F / 64));
    // dx
    const dxLen = N * C * H * H;
    if (!this._dxBuf || this._dxBuf.size < dxLen * 4) {
      if (this._dxBuf) this._dxBuf.destroy();
      this._dxBuf = this.gb.storage(dxLen * 4);
    }
    this.gb.run(this._codeDX, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: dy.data } },
      { binding: 2, resource: { buffer: this._wBuf } },
      { binding: 3, resource: { buffer: this._dxBuf } },
    ], Math.ceil(dxLen / 64));
    return { data: this._dxBuf, shape: [N, C, H, H] };
  }
  // Pull current grads into the Float32Array mirrors (for parity / inspection).
  async syncGrads() {
    this.gW = await this.gb.readF32(this._gWBuf, this.gW.length);
    this.gb_ = await this.gb.readF32(this._gbBuf, this.gb_.length);
  }
}

// ---------------------------------------------------------------------------
// ReLU. mask is a persistent u32 buffer.
// ---------------------------------------------------------------------------
class GReLU {
  constructor(gb) { this.gb = gb; this._outBuf = null; this._dxBuf = null; this._maskBuf = null; this._per = 0; }
  getParams() { return []; }
  async forward(x) {
    const [N] = x.shape;
    let per = 1; for (let i = 1; i < x.shape.length; i++) per *= x.shape[i];
    this._per = per; this._x = x; this._N = N;
    const len = N * per;
    if (!this._outBuf || this._outBuf.size < len * 4) {
      if (this._outBuf) this._outBuf.destroy();
      this._outBuf = this.gb.storage(len * 4);
    }
    if (!this._maskBuf || this._maskBuf.size < len * 4) {
      if (this._maskBuf) this._maskBuf.destroy();
      this._maskBuf = this.gb.storage(len * 4);
    }
    this._code = K.reluForward(per);
    this.gb.run(this._code, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: x.data } },
      { binding: 2, resource: { buffer: this._outBuf } },
      { binding: 3, resource: { buffer: this._maskBuf } },
    ], Math.ceil(len / 64));
    return { data: this._outBuf, shape: x.shape.slice() };
  }
  async backward(dy) {
    const len = this._N * this._per;
    if (!this._dxBuf || this._dxBuf.size < len * 4) {
      if (this._dxBuf) this._dxBuf.destroy();
      this._dxBuf = this.gb.storage(len * 4);
    }
    this._codeB = K.reluBackward(this._per);
    this.gb.run(this._codeB, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: dy.data } },
      { binding: 2, resource: { buffer: this._maskBuf } },
      { binding: 3, resource: { buffer: this._dxBuf } },
    ], Math.ceil(len / 64));
    return { data: this._dxBuf, shape: dy.shape.slice() };
  }
}

// ---------------------------------------------------------------------------
// MaxPool 2x2. argmax is a persistent u32 buffer; dx zeroed before scatter.
// ---------------------------------------------------------------------------
class GMaxPool2D {
  constructor(k, stride, gb) { this.k = k; this.stride = stride; this.gb = gb; this._outBuf = null; this._dxBuf = null; this._argmaxBuf = null; }
  getParams() { return []; }
  async forward(x) {
    const [N, C, H, W] = x.shape;
    const OH = (((H - this.k) / this.stride) | 0) + 1;
    const OW = (((W - this.k) / this.stride) | 0) + 1;
    this._inShape = x.shape; this._x = x; this._N = N; this._C = C; this._OH = OH; this._OW = OW;
    const outLen = N * C * OH * OW;
    if (!this._outBuf || this._outBuf.size < outLen * 4) { if (this._outBuf) this._outBuf.destroy(); this._outBuf = this.gb.storage(outLen * 4); }
    if (!this._argmaxBuf || this._argmaxBuf.size < outLen * 4) { if (this._argmaxBuf) this._argmaxBuf.destroy(); this._argmaxBuf = this.gb.storage(outLen * 4); }
    this._codeF = K.poolForward(C, H, W, OH, OW, this.k, this.stride);
    this.gb.run(this._codeF, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: x.data } },
      { binding: 2, resource: { buffer: this._outBuf } },
      { binding: 3, resource: { buffer: this._argmaxBuf } },
    ], Math.ceil(outLen / 64));
    return { data: this._outBuf, shape: [N, C, OH, OW] };
  }
  async backward(dy) {
    const [N, C, H, W] = this._inShape;
    const OH = this._OH, OW = this._OW;
    const HW = H * W;
    const dxLen = N * C * HW;
    if (!this._dxBuf || this._dxBuf.size < dxLen * 4) { if (this._dxBuf) this._dxBuf.destroy(); this._dxBuf = this.gb.storage(dxLen * 4); }
    // zero dx, then scatter (separate submits so ordering is guaranteed).
    const zc = K.zeroFill();
    this.gb.run(zc, [
      { binding: 0, resource: { buffer: this._zeroN(dxLen) } },
      { binding: 1, resource: { buffer: this._dxBuf } },
    ], Math.ceil(dxLen / 64));
    this._codeB = K.poolBackward(C, OH, OW);
    this.gb.run(this._codeB, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: dy.data } },
      { binding: 2, resource: { buffer: this._argmaxBuf } },
      { binding: 3, resource: { buffer: this._dxBuf } },
    ], Math.ceil(N * C * OH * OW / 64));
    return { data: this._dxBuf, shape: [N, C, H, W] };
  }
  // tiny uniform {n} for zeroFill (16 bytes).
  _zeroN(n) {
    if (!this._zBuf) { this._zBuf = this.gb._uniform16(); this._zArr = new Uint32Array(4); }
    this._zArr[0] = n >>> 0;
    this.gb.device.queue.writeBuffer(this._zBuf, 0, this._zArr);
    return this._zBuf;
  }
}

// Flatten is purely a reshape — data buffer passes through unchanged.
class GFlatten {
  getParams() { return []; }
  async forward(x) { this.inShape = x.shape.slice(); const N = x.shape[0]; let rest = 1; for (let i = 1; i < x.shape.length; i++) rest *= x.shape[i]; return { data: x.data, shape: [N, rest] }; }
  async backward(dy) { return { data: dy.data, shape: this.inShape.slice() }; }
}

// Dense. W: [outN, inN].
class GDense {
  constructor(inN, outN, rng, gb) {
    this.inN = inN; this.outN = outN; this.gb = gb;
    const std = Math.sqrt(2 / inN);
    this.W = new Float32Array(outN * inN);
    for (let i = 0; i < this.W.length; i++) this.W[i] = rng.gauss(0, std);
    this.b = new Float32Array(outN);
    this.gW = new Float32Array(outN * inN);
    this.gb_ = new Float32Array(outN);
    this.vW = new Float32Array(outN * inN);
    this.vb = new Float32Array(outN);
    this._wBuf = gb.storage(this.W.byteLength); gb.upload(this._wBuf, this.W);
    this._bBuf = gb.storage(this.b.byteLength); gb.upload(this._bBuf, this.b);
    this._gWBuf = gb.storage(this.gW.byteLength);
    this._gbBuf = gb.storage(this.gb_.byteLength);
    this._vWBuf = gb.storage(this.vW.byteLength);
    this._vbBuf = gb.storage(this.vb.byteLength);
    this._outBuf = null; this._dxBuf = null;
    this._codeF = K.denseForward(inN, outN);
    this._codeDW = K.denseDW(inN, outN);
    this._codeDB = K.denseDB(outN);
    this._codeDX = K.denseDX(inN, outN);
  }
  getParams() {
    return [
      { name: 'W', value: this.W, grad: this.gW, vel: this.vW, gpuValue: this._wBuf, gpuGrad: this._gWBuf, gpuVel: this._vWBuf, n: this.W.length },
      { name: 'b', value: this.b, grad: this.gb_, vel: this.vb, gpuValue: this._bBuf, gpuGrad: this._gbBuf, gpuVel: this._vbBuf, n: this.b.length },
    ];
  }
  async forward(x) {
    const [N] = x.shape; this._x = x; this._N = N;
    const outLen = N * this.outN;
    if (!this._outBuf || this._outBuf.size < outLen * 4) { if (this._outBuf) this._outBuf.destroy(); this._outBuf = this.gb.storage(outLen * 4); }
    this.gb.run(this._codeF, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: x.data } },
      { binding: 2, resource: { buffer: this._wBuf } },
      { binding: 3, resource: { buffer: this._bBuf } },
      { binding: 4, resource: { buffer: this._outBuf } },
    ], Math.ceil(outLen / 64));
    return { data: this._outBuf, shape: [N, this.outN] };
  }
  async backward(dy) {
    const { inN, outN } = this;
    this.gb.run(this._codeDW, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: this._x.data } },
      { binding: 2, resource: { buffer: dy.data } },
      { binding: 3, resource: { buffer: this._gWBuf } },
    ], Math.ceil(outN * inN / 64));
    this.gb.run(this._codeDB, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: dy.data } },
      { binding: 2, resource: { buffer: this._gbBuf } },
    ], Math.ceil(outN / 64));
    const dxLen = this._N * inN;
    if (!this._dxBuf || this._dxBuf.size < dxLen * 4) { if (this._dxBuf) this._dxBuf.destroy(); this._dxBuf = this.gb.storage(dxLen * 4); }
    this.gb.run(this._codeDX, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: dy.data } },
      { binding: 2, resource: { buffer: this._wBuf } },
      { binding: 3, resource: { buffer: this._dxBuf } },
    ], Math.ceil(dxLen / 64));
    return { data: this._dxBuf, shape: [this._N, inN] };
  }
  async syncGrads() {
    this.gW = await this.gb.readF32(this._gWBuf, this.gW.length);
    this.gb_ = await this.gb.readF32(this._gbBuf, this.gb_.length);
  }
}

// Softmax + cross-entropy. forward returns {loss, probs}; loss is read back.
class GSoftmaxCE {
  constructor(gb, numCls) { this.gb = gb; this.K = numCls; this._probsBuf = null; this._rowlossBuf = null; this._dlBuf = null; this._codeF = K.smForward(numCls); this._codeB = K.smBackward(numCls); }
  getParams() { return []; }
  async forward(logits, labels) {
    const [N, Kc] = logits.shape;
    this._N = N; this._K = Kc;
    if (!this._probsBuf || this._probsBuf.size < N * Kc * 4) { if (this._probsBuf) this._probsBuf.destroy(); this._probsBuf = this.gb.storage(N * Kc * 4); }
    if (!this._rowlossBuf || this._rowlossBuf.size < N * 4) { if (this._rowlossBuf) this._rowlossBuf.destroy(); this._rowlossBuf = this.gb.storage(N * 4); }
    if (!this._lblBuf || this._lblBuf.size < N * 4) { this._lblBuf = this.gb.storage(N * 4); }
    const lbl = new Uint32Array(labels.length);
    for (let i = 0; i < labels.length; i++) lbl[i] = labels[i] >>> 0;
    this.gb.upload(this._lblBuf, lbl);
    this.gb.run(this._codeF, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: logits.data } },
      { binding: 2, resource: { buffer: this._lblBuf } },
      { binding: 3, resource: { buffer: this._probsBuf } },
      { binding: 4, resource: { buffer: this._rowlossBuf } },
    ], Math.ceil(N / 64));
    const rl = await this.gb.readF32(this._rowlossBuf, N);
    let loss = 0; for (let i = 0; i < N; i++) loss += rl[i];
    return { loss: loss / N, probs: this._probsBuf };
  }
  async backward() {
    const N = this._N, K = this._K;
    if (!this._dlBuf || this._dlBuf.size < N * K * 4) { if (this._dlBuf) this._dlBuf.destroy(); this._dlBuf = this.gb.storage(N * K * 4); }
    this.gb.run(this._codeB, [
      { binding: 0, resource: { buffer: this.gb._nBuf } },
      { binding: 1, resource: { buffer: this._probsBuf } },
      { binding: 2, resource: { buffer: this._lblBuf } },
      { binding: 3, resource: { buffer: this._dlBuf } },
    ], Math.ceil(N * K / 64));
    return { data: this._dlBuf, shape: [N, K] };
  }
}

// ---------------------------------------------------------------------------
// GPU-side SGD with momentum + global-norm gradient clipping. Keeps weights and
// velocities resident on the GPU; only the (tiny) per-param norm scalars are
// read back (4 bytes each) to compute the global clip factor.
// ---------------------------------------------------------------------------
export class GpuMomentumSGD {
  constructor(lr = 0.05, momentum = 0.9, maxNorm = 2.0) { this.lr = lr; this.momentum = momentum; this.maxNorm = maxNorm; }
  async step(params, gb) {
    // 1) global L2 norm across all params.
    let sq = 0;
    for (const p of params) {
      const outBuf = gb.storage(16); // scalar
      gb.run(K.reduceSq(p.n), [
        { binding: 0, resource: { buffer: p.gpuGrad } },
        { binding: 1, resource: { buffer: outBuf } },
      ], 1);
      const s = await gb.readF32(outBuf, 1);
      sq += s[0];
      outBuf.destroy();
    }
    const n = Math.sqrt(sq);
    const scale = this.maxNorm && n > this.maxNorm ? this.maxNorm / (n + 1e-12) : 1.0;
    // 2) per-tensor momentum update on the GPU.
    const u = new Float32Array(4);
    u[0] = this.lr; u[1] = this.momentum; u[2] = scale; u[3] = 0;
    for (const p of params) {
      const ub = gb._uniform16();
      gb.upload(ub, u);
      gb.run(K.sgdUpdate(p.n), [
        { binding: 0, resource: { buffer: ub } },
        { binding: 1, resource: { buffer: p.gpuValue } },
        { binding: 2, resource: { buffer: p.gpuGrad } },
        { binding: 3, resource: { buffer: p.gpuVel } },
      ], Math.ceil(p.n / 64));
    }
    await gb.fence();
  }
}

// Factory: same surface as cpuBackend. Takes a ready GpuBackend instance.
export function makeGpuBackend(gb) {
  return {
    Conv2D: (inCh, outCh, k, stride, pad, rng) => new GConv2D(inCh, outCh, k, stride, pad, rng, gb),
    ReLU: () => new GReLU(gb),
    MaxPool2D: (k, stride) => new GMaxPool2D(k, stride, gb),
    Flatten: () => new GFlatten(),
    Dense: (inN, outN, rng) => new GDense(inN, outN, rng, gb),
    SoftmaxCE: () => new GSoftmaxCE(gb, 10),
  };
}
