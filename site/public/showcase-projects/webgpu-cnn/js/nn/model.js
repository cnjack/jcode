// js/nn/model.js
// Backend-agnostic model definition + training utilities. The actual layer
// math is supplied by a backend (js/nn/cpu.js) that implements the same
// forward/backward/getParams interface.

export const IMG = 16;
export const IN_CH = 1;
export const NUM_CLASSES = 10;

// Build the canonical CNN:
//  1x16x16 -> conv3x3(8,p1) -> relu -> pool2 -> conv3x3(16,p1) -> relu -> pool2
//  -> flatten(256) -> dense(10) -> softmax+cross-entropy
export function buildModel(backend, rng) {
  const conv1 = backend.Conv2D(IN_CH, 8, 3, 1, 1, rng);
  const relu1 = backend.ReLU();
  const pool1 = backend.MaxPool2D(2, 2);
  const conv2 = backend.Conv2D(8, 16, 3, 1, 1, rng);
  const relu2 = backend.ReLU();
  const pool2 = backend.MaxPool2D(2, 2);
  const flat = backend.Flatten();
  const dense = backend.Dense(256, NUM_CLASSES, rng);
  const smce = backend.SoftmaxCE();
  const layers = [conv1, relu1, pool1, conv2, relu2, pool2, flat, dense];
  return { layers, smce, refs: { conv1, relu1, pool1, conv2, relu2, pool2, flat, dense } };
}

// Run forward through all layers; returns logits tensor.
export function modelForward(model, x) {
  let a = x;
  for (const l of model.layers) a = l.forward(a);
  return a; // logits [N, NUM_CLASSES]
}

// Backprop through all layers; smce.backward() seeds the gradient.
export function modelBackward(model) {
  let g = model.smce.backward();
  for (let i = model.layers.length - 1; i >= 0; i--) g = model.layers[i].backward(g);
  return g;
}

// --- async variants ---------------------------------------------------------
// `await` transparently handles BOTH CPU layers (forward returns a plain
// tensor) and GPU layers (forward returns a Promise<tensor>), so the worker's
// training loop uses these regardless of backend. The synchronous versions
// above are kept for the CPU-only numerical gradient check.
export async function modelForwardAsync(model, x) {
  let a = x;
  for (const l of model.layers) a = await l.forward(a);
  return a;
}
export async function modelBackwardAsync(model) {
  let g = await model.smce.backward();
  for (let i = model.layers.length - 1; i >= 0; i--) g = await model.layers[i].backward(g);
  return g;
}

// Collect all trainable parameters (each: { value, grad, vel }).
export function allParams(model) {
  const ps = [];
  for (const l of model.layers) {
    const lp = l.getParams();
    for (let i = 0; i < lp.length; i++) ps.push(lp[i]);
  }
  return ps;
}

// SGD with momentum (PyTorch-style velocity: v = m*v + g; w -= lr*v) and
// optional global-norm gradient clipping for training stability.
export class MomentumSGD {
  constructor(lr = 0.05, momentum = 0.9, maxNorm = 5) {
    this.lr = lr;
    this.momentum = momentum;
    this.maxNorm = maxNorm;
  }
  step(params) {
    // Global L2 norm gradient clipping (prevents divergence on bad batches).
    if (this.maxNorm) {
      let sq = 0;
      for (const p of params) {
        const g = p.grad;
        for (let i = 0; i < g.length; i++) sq += g[i] * g[i];
      }
      const n = Math.sqrt(sq);
      if (n > this.maxNorm) {
        const s = this.maxNorm / (n + 1e-12);
        for (const p of params) {
          const g = p.grad;
          for (let i = 0; i < g.length; i++) g[i] *= s;
        }
      }
    }
    const m = this.momentum;
    const lr = this.lr;
    for (const p of params) {
      const v = p.vel;
      const g = p.grad;
      const val = p.value;
      for (let i = 0; i < v.length; i++) {
        v[i] = m * v[i] + g[i];
        val[i] -= lr * v[i];
      }
    }
  }
}

// Classification accuracy over a dataset slice (no gradient state needed).
export function accuracy(model, X, Y, imgLen, batchSize = 256) {
  const n = Y.length;
  let correct = 0;
  for (let s = 0; s < n; s += batchSize) {
    const bs = Math.min(batchSize, n - s);
    const xb = new Float32Array(bs * imgLen);
    const yb = new Int32Array(bs);
    for (let i = 0; i < bs; i++) {
      xb.set(X.subarray((s + i) * imgLen, (s + i + 1) * imgLen), i * imgLen);
      yb[i] = Y[s + i];
    }
    const logits = modelForward(model, { data: xb, shape: [bs, IN_CH, IMG, IMG] });
    for (let i = 0; i < bs; i++) {
      let mx = -Infinity;
      let mi = 0;
      const off = i * NUM_CLASSES;
      for (let k = 0; k < NUM_CLASSES; k++) {
        if (logits.data[off + k] > mx) {
          mx = logits.data[off + k];
          mi = k;
        }
      }
      if (mi === yb[i]) correct++;
    }
  }
  return correct / n;
}
