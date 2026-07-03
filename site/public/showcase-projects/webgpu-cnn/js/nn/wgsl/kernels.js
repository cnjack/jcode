// js/nn/wgsl/kernels.js
// WGSL compute kernels for the WebGPU backend. Each export is a GENERATOR that
// returns a self-contained WGSL module string (one `main` compute entry point),
// with this layer's STATIC dimensions baked in as module-scope constants. Only
// the batch size N is dynamic (passed via a shared {N:u32} uniform at binding 0),
// which keeps uniform/buffer management trivial and lets all forward/backward
// dispatches in a batch share one uniform buffer.
//
// All tensors are f32 storage buffers in NCHW (4-D) or row-major (2-D) layout,
// EXACTLY matching js/nn/cpu.js, so the two backends are directly comparable.
// Dispatch model: every kernel uses @workgroup_size(64) over a 1-D grid; the JS
// side computes nWorkgroups = ceil(total / 64). Each thread decodes its
// (n, f, oy, ox) — or (n, c, iy, ix) — from its linear id.
//
// NOTE: module-scope declarations use `const`. WGSL removed module-scope `let`
// (older Chrome tolerated it; current Chrome rejects the shader, which would make
// every dispatch a silent no-op). Function-scope `let` below is still valid.

// Shared uniform struct (16 bytes) used by every forward/backward kernel.
const UN = `
struct U { N: u32, _p0: u32, _p1: u32, _p2: u32 };
@group(0) @binding(0) var<uniform> u: U;
`;

// ---------------------------------------------------------------------------
// Conv2D forward: out[n,f,oy,ox] = b[f] + sum_{c,ky,kx} W[f,c,ky,kx]*x[n,c,iy,ix]
// One thread per output element. K=3, pad=1, stride=1 baked.
// ---------------------------------------------------------------------------
export function convForward(C, F, H, W, OH, OW, K, pad, stride) {
  return `
${UN}
const C:u32 = ${C}u; const F:u32 = ${F}u; const H:u32 = ${H}u; const W:u32 = ${W}u;
const OH:u32 = ${OH}u; const OW:u32 = ${OW}u; const K:u32 = ${K}u;
const PAD:i32 = ${pad}; const STRIDE:u32 = ${stride}u;
const OHOW:u32 = OH*OW; const FOW:u32 = F*OHOW;
@group(0) @binding(1) var<storage, read> x: array<f32>;
@group(0) @binding(2) var<storage, read> Wt: array<f32>;
@group(0) @binding(3) var<storage, read> b: array<f32>;
@group(0) @binding(4) var<storage, read_write> out: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let idx = gid.x;
  let total = u.N * FOW;
  if (idx >= total) { return; }
  let n = idx / FOW;
  let r0 = idx % FOW;
  let f = r0 / OHOW;
  let r1 = r0 % OHOW;
  let oy = r1 / OW;
  let ox = r1 % OW;
  var s: f32 = b[f];
  let wOff = f * C * K * K;
  let nInOff = n * C * H * W;
  for (var c:u32 = 0u; c < C; c = c + 1u) {
    let cInOff = nInOff + c * H * W;
    let wco = wOff + c * K * K;
    for (var ky:u32 = 0u; ky < K; ky = ky + 1u) {
      let iy = i32(oy * STRIDE + ky) - PAD;
      if (iy < 0 || iy >= i32(H)) { continue; }
      for (var kx:u32 = 0u; kx < K; kx = kx + 1u) {
        let ix = i32(ox * STRIDE + kx) - PAD;
        if (ix < 0 || ix >= i32(W)) { continue; }
        s = s + Wt[wco + ky*K + kx] * x[cInOff + u32(iy)*W + u32(ix)];
      }
    }
  }
  out[idx] = s;
}
`;
}

// Conv2D weight gradient: gW[f,c,ky,kx] = sum_{n,oy,ox} dy[n,f,oy,ox]*x[n,c,iy,ix].
// One thread per WEIGHT element (F*C*K*K) — no races, each writes its own gW slot.
export function convDW(C, F, H, W, OH, OW, K, pad, stride) {
  return `
${UN}
const C:u32 = ${C}u; const F:u32 = ${F}u; const H:u32 = ${H}u; const W:u32 = ${W}u;
const OH:u32 = ${OH}u; const OW:u32 = ${OW}u; const K:u32 = ${K}u;
const PAD:i32 = ${pad}; const STRIDE:u32 = ${stride}u; const OHOW:u32 = OH*OW;
const KK:u32 = K*K;
@group(0) @binding(1) var<storage, read> x: array<f32>;
@group(0) @binding(2) var<storage, read> dy: array<f32>;
@group(0) @binding(3) var<storage, read_write> gW: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let idx = gid.x;
  let total = F * C * KK;
  if (idx >= total) { return; }
  let f = idx / (C * KK);
  let r0 = idx % (C * KK);
  let c = r0 / KK;
  let r1 = r0 % KK;
  let ky = r1 / K;
  let kx = r1 % K;
  var acc: f32 = 0.0;
  let nInStride = C * H * W;
  for (var n:u32 = 0u; n < u.N; n = n + 1u) {
    let nInOff = n * nInStride + c * H * W;
    let nDyOff = n * F * OHOW + f * OHOW;
    for (var oy:u32 = 0u; oy < OH; oy = oy + 1u) {
      let iy = i32(oy * STRIDE + ky) - PAD;
      if (iy < 0 || iy >= i32(H)) { continue; }
      for (var ox:u32 = 0u; ox < OW; ox = ox + 1u) {
        let ix = i32(ox * STRIDE + kx) - PAD;
        if (ix < 0 || ix >= i32(W)) { continue; }
        acc = acc + dy[nDyOff + oy*OW + ox] * x[nInOff + u32(iy)*W + u32(ix)];
      }
    }
  }
  gW[idx] = acc;
}
`;
}

// Conv2D bias gradient: gb[f] = sum_{n,oy,ox} dy[n,f,oy,ox]. One thread per filter.
export function convDB(F, OH, OW) {
  return `
${UN}
const F:u32 = ${F}u; const OH:u32 = ${OH}u; const OW:u32 = ${OW}u; const OHOW:u32 = OH*OW;
@group(0) @binding(1) var<storage, read> dy: array<f32>;
@group(0) @binding(2) var<storage, read_write> gb: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let f = gid.x;
  if (f >= F) { return; }
  var acc: f32 = 0.0;
  for (var n:u32 = 0u; n < u.N; n = n + 1u) {
    let off = n * F * OHOW + f * OHOW;
    for (var i:u32 = 0u; i < OHOW; i = i + 1u) { acc = acc + dy[off + i]; }
  }
  gb[f] = acc;
}
`;
}

// Conv2D input gradient: dx[n,c,iy,ix] = sum_f sum_{ky,kx} W[f,c,ky,kx]*dy[n,f,oy,ox]
// where (oy,ox) is the unique output position reading input (iy,ix) via tap (ky,kx).
// One thread per INPUT element — no races.
export function convDX(C, F, H, W, OH, OW, K, pad, stride) {
  return `
${UN}
const C:u32 = ${C}u; const F:u32 = ${F}u; const H:u32 = ${H}u; const W:u32 = ${W}u;
const OH:u32 = ${OH}u; const OW:u32 = ${OW}u; const K:u32 = ${K}u;
const PAD:i32 = ${pad}; const STRIDE:i32 = ${stride}; const OHOW:u32 = OH*OW;
@group(0) @binding(1) var<storage, read> dy: array<f32>;
@group(0) @binding(2) var<storage, read> Wt: array<f32>;
@group(0) @binding(3) var<storage, read_write> dx: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let idx = gid.x;
  let HW = H * W;
  let total = u.N * C * HW;
  if (idx >= total) { return; }
  let n = idx / (C * HW);
  let r0 = idx % (C * HW);
  let c = r0 / HW;
  let r1 = r0 % HW;
  let iy = r1 / W;
  let ix = r1 % W;
  var acc: f32 = 0.0;
  for (var f:u32 = 0u; f < F; f = f + 1u) {
    let wOff = f * C * K * K + c * K * K;
    let nDyOff = n * F * OHOW + f * OHOW;
    for (var ky:u32 = 0u; ky < K; ky = ky + 1u) {
      let oyi = i32(iy) - i32(ky) + PAD;
      if ((oyi % STRIDE) != 0) { continue; }
      let oy = u32(oyi / STRIDE);
      if (oy >= OH) { continue; }
      for (var kx:u32 = 0u; kx < K; kx = kx + 1u) {
        let oxi = i32(ix) - i32(kx) + PAD;
        if ((oxi % STRIDE) != 0) { continue; }
        let ox = u32(oxi / STRIDE);
        if (ox >= OW) { continue; }
        acc = acc + Wt[wOff + ky*K + kx] * dy[nDyOff + oy*OW + ox];
      }
    }
  }
  dx[idx] = acc;
}
`;
}

// ---------------------------------------------------------------------------
// MaxPool 2x2 forward: records absolute argmax flat index into x. One thread
// per output element (n,c,oy,ox). Windows are disjoint (k=stride=2) so the
// backward scatter is race-free.
// ---------------------------------------------------------------------------
export function poolForward(C, H, W, OH, OW, K, stride) {
  return `
${UN}
const C:u32 = ${C}u; const H:u32 = ${H}u; const W:u32 = ${W}u;
const OH:u32 = ${OH}u; const OW:u32 = ${OW}u; const K:u32 = ${K}u;
const STRIDE:u32 = ${stride}u; const OHOW:u32 = OH*OW; const HW:u32 = H*W;
@group(0) @binding(1) var<storage, read> x: array<f32>;
@group(0) @binding(2) var<storage, read_write> out: array<f32>;
@group(0) @binding(3) var<storage, read_write> argmax: array<u32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let idx = gid.x;
  let total = u.N * C * OHOW;
  if (idx >= total) { return; }
  let n = idx / (C * OHOW);
  let r0 = idx % (C * OHOW);
  let c = r0 / OHOW;
  let r1 = r0 % OHOW;
  let oy = r1 / OW;
  let ox = r1 % OW;
  let plane = n * C * HW + c * HW;
  var best: f32 = -3.402823e+38;
  var bi: u32 = 0u;
  for (var ky:u32 = 0u; ky < K; ky = ky + 1u) {
    for (var kx:u32 = 0u; kx < K; kx = kx + 1u) {
      let ii = plane + (oy*STRIDE + ky)*W + (ox*STRIDE + kx);
      let v = x[ii];
      if (v > best) { best = v; bi = ii; }
    }
  }
  out[idx] = best;
  argmax[idx] = bi;
}
`;
}

// MaxPool backward: dx[argmax] += dy. Windows are disjoint (k=stride=2) so the
// scatter is race-free; dx is zeroed first (see zeroFill). One thread per output.
export function poolBackward(C, OH, OW) {
  return `
${UN}
const C:u32 = ${C}u; const OHOW:u32 = ${OH * OW}u;
@group(0) @binding(1) var<storage, read> dy: array<f32>;
@group(0) @binding(2) var<storage, read> argmax: array<u32>;
@group(0) @binding(3) var<storage, read_write> dx: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let idx = gid.x;
  let total = u.N * C * OHOW;
  if (idx >= total) { return; }
  let a = argmax[idx];
  dx[a] = dx[a] + dy[idx];
}
`;
}

// ---------------------------------------------------------------------------
// Dense (fully connected). W is [outN, inN].
// ---------------------------------------------------------------------------
export function denseForward(inN, outN) {
  return `
${UN}
const inN:u32 = ${inN}u; const outN:u32 = ${outN}u;
@group(0) @binding(1) var<storage, read> x: array<f32>;
@group(0) @binding(2) var<storage, read> Wt: array<f32>;
@group(0) @binding(3) var<storage, read> b: array<f32>;
@group(0) @binding(4) var<storage, read_write> out: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let idx = gid.x;
  let total = u.N * outN;
  if (idx >= total) { return; }
  let n = idx / outN;
  let o = idx % outN;
  var s: f32 = b[o];
  let wo = o * inN;
  let xo = n * inN;
  for (var i:u32 = 0u; i < inN; i = i + 1u) { s = s + Wt[wo + i] * x[xo + i]; }
  out[idx] = s;
}
`;
}
export function denseDW(inN, outN) {
  return `
${UN}
const inN:u32 = ${inN}u; const outN:u32 = ${outN}u;
@group(0) @binding(1) var<storage, read> x: array<f32>;
@group(0) @binding(2) var<storage, read> dy: array<f32>;
@group(0) @binding(3) var<storage, read_write> gW: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let idx = gid.x;
  let total = outN * inN;
  if (idx >= total) { return; }
  let o = idx / inN;
  let i = idx % inN;
  var acc: f32 = 0.0;
  for (var n:u32 = 0u; n < u.N; n = n + 1u) {
    acc = acc + dy[n*outN + o] * x[n*inN + i];
  }
  gW[idx] = acc;
}
`;
}
export function denseDB(outN) {
  return `
${UN}
const outN:u32 = ${outN}u;
@group(0) @binding(1) var<storage, read> dy: array<f32>;
@group(0) @binding(2) var<storage, read_write> gb: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let o = gid.x;
  if (o >= outN) { return; }
  var acc: f32 = 0.0;
  for (var n:u32 = 0u; n < u.N; n = n + 1u) { acc = acc + dy[n*outN + o]; }
  gb[o] = acc;
}
`;
}
export function denseDX(inN, outN) {
  return `
${UN}
const inN:u32 = ${inN}u; const outN:u32 = ${outN}u;
@group(0) @binding(1) var<storage, read> dy: array<f32>;
@group(0) @binding(2) var<storage, read> Wt: array<f32>;
@group(0) @binding(3) var<storage, read_write> dx: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let idx = gid.x;
  let total = u.N * inN;
  if (idx >= total) { return; }
  let n = idx / inN;
  let i = idx % inN;
  var acc: f32 = 0.0;
  for (var o:u32 = 0u; o < outN; o = o + 1u) { acc = acc + dy[n*outN + o] * Wt[o*inN + i]; }
  dx[idx] = acc;
}
`;
}

// ---------------------------------------------------------------------------
// ReLU forward (records u32 mask: 1 if x>0) and backward (dx = dy*mask).
// ---------------------------------------------------------------------------
export function reluForward(per) {
  return `
${UN}
const PER:u32 = ${per}u; // elements per sample (everything but the batch dim)
@group(0) @binding(1) var<storage, read> x: array<f32>;
@group(0) @binding(2) var<storage, read_write> out: array<f32>;
@group(0) @binding(3) var<storage, read_write> mask: array<u32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let i = gid.x;
  if (i >= u.N * PER) { return; }
  let v = x[i];
  out[i] = select(0.0, v, v > 0.0);
  mask[i] = select(0u, 1u, v > 0.0);
}
`;
}
export function reluBackward(per) {
  return `
${UN}
const PER:u32 = ${per}u;
@group(0) @binding(1) var<storage, read> dy: array<f32>;
@group(0) @binding(2) var<storage, read> mask: array<u32>;
@group(0) @binding(3) var<storage, read_write> dx: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let i = gid.x;
  if (i >= u.N * PER) { return; }
  dx[i] = dy[i] * f32(mask[i]);
}
`;
}

// ---------------------------------------------------------------------------
// Softmax + cross-entropy. forward: per-row softmax into `probs` and per-row
// loss into `rowloss`; the JS side sums rowloss/N for the scalar loss. backward:
// dlogits[n,k] = (probs[n,k] - 1{k==label}) / N. K (num classes) is baked.
// ---------------------------------------------------------------------------
export function smForward(K) {
  return `
${UN}
const K:u32 = ${K}u;
@group(0) @binding(1) var<storage, read> logits: array<f32>;
@group(0) @binding(2) var<storage, read> labels: array<u32>;
@group(0) @binding(3) var<storage, read_write> probs: array<f32>;
@group(0) @binding(4) var<storage, read_write> rowloss: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let n = gid.x;
  if (n >= u.N) { return; }
  let off = n * K;
  var mx: f32 = -3.402823e+38;
  for (var k:u32 = 0u; k < K; k = k + 1u) {
    if (logits[off + k] > mx) { mx = logits[off + k]; }
  }
  var s: f32 = 0.0;
  for (var k:u32 = 0u; k < K; k = k + 1u) {
    let e = exp(logits[off + k] - mx);
    probs[off + k] = e;
    s = s + e;
  }
  let inv = 1.0 / s;
  for (var k:u32 = 0u; k < K; k = k + 1u) { probs[off + k] = probs[off + k] * inv; }
  let lbl = labels[n];
  rowloss[n] = -log(probs[off + lbl] + 1.0e-12);
}
`;
}
export function smBackward(K) {
  return `
${UN}
const K:u32 = ${K}u;
@group(0) @binding(1) var<storage, read> probs: array<f32>;
@group(0) @binding(2) var<storage, read> labels: array<u32>;
@group(0) @binding(3) var<storage, read_write> dlogits: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let idx = gid.x;
  if (idx >= u.N * K) { return; }
  let n = idx / K;
  let k = idx % K;
  let t = select(0.0, 1.0, k == labels[n]);
  dlogits[idx] = (probs[idx] - t) / f32(u.N);
}
`;
}

// ---------------------------------------------------------------------------
// Generic zero-fill (f32) — used to clear maxpool's dx before the scatter-add.
// ---------------------------------------------------------------------------
export function zeroFill() {
  return `
struct ZN { n: u32, _p0: u32, _p1: u32, _p2: u32 };
@group(0) @binding(0) var<uniform> u: ZN;
@group(0) @binding(1) var<storage, read_write> buf: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let i = gid.x;
  if (i >= u.n) { return; }
  buf[i] = 0.0;
}
`;
}

// ---------------------------------------------------------------------------
// Global-norm reduction: out[0] = sum_i g[i]*g[i]. One workgroup of 256 with
// strided accumulation covers any param size (max here is 2560 elements).
// ---------------------------------------------------------------------------
export function reduceSq(n) {
  return `
const NELS:u32 = ${n}u;
@group(0) @binding(0) var<storage, read> g: array<f32>;
@group(0) @binding(1) var<storage, read_write> out: array<f32>;
var<workgroup> sh: array<f32, 256>;
@compute @workgroup_size(256)
fn main(@builtin(local_invocation_id) lid: vec3<u32>) {
  let i = lid.x;
  var acc: f32 = 0.0;
  for (var j:u32 = i; j < NELS; j = j + 256u) { acc = acc + g[j] * g[j]; }
  sh[i] = acc;
  workgroupBarrier();
  for (var s:u32 = 128u; s > 0u; s = s >> 1u) {
    if (i < s) { sh[i] = sh[i] + sh[i + s]; }
    workgroupBarrier();
  }
  if (i == 0u) { out[0] = sh[0]; }
}
`;
}

// ---------------------------------------------------------------------------
// SGD-with-momentum update (PyTorch-style): v = m*v + scale*g; w -= lr*v.
// scale is the global-norm clip factor (1.0 when no clip). n is baked per param.
// Weights AND velocities stay resident on the GPU between batches.
// ---------------------------------------------------------------------------
export function sgdUpdate(n) {
  return `
const NELS:u32 = ${n}u;
struct S { lr: f32, momentum: f32, scale: f32, _p: u32 };
@group(0) @binding(0) var<uniform> u: S;
@group(0) @binding(1) var<storage, read_write> w: array<f32>;
@group(0) @binding(2) var<storage, read> g: array<f32>;
@group(0) @binding(3) var<storage, read_write> v: array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
  let i = gid.x;
  if (i >= NELS) { return; }
  let gi = g[i] * u.scale;
  let vi = u.momentum * v[i] + gi;
  v[i] = vi;
  w[i] = w[i] - u.lr * vi;
}
`;
}
