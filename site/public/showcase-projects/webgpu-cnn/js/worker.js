// js/worker.js
// Runs dataset generation + training off the main thread. Cooperative loop:
// processes batches within a time budget then yields via setTimeout so it can
// receive pause/reset messages. A runId guards against stale UI updates.
//
// Backend selection: at startup the worker probes WebGPU (navigator.gpu). The
// active backend is chosen from the user preference ('auto' | 'cpu' | 'webgpu')
// applied on Reset. On ANY failure (no navigator.gpu, null adapter, device
// rejection, device.lost) it falls back to CPU and reports a human reason. The
// CPU path is the synchronous, proven round-2 loop; the GPU path is async
// (weights/velocities stay resident on the GPU between batches).

import { RNG } from './rng.js';
import {
  createDataset,
  displaySamples,
  IMGLEN,
  NUM_CLASSES,
} from './dataset.js';
import {
  buildModel,
  modelForward,
  modelBackward,
  modelForwardAsync,
  modelBackwardAsync,
  allParams,
  MomentumSGD,
  accuracy,
} from './nn/model.js';
import { cpuBackend } from './nn/cpu.js';
import { probeGpu, GpuBackend, makeGpuBackend, GpuMomentumSGD } from './nn/gpu.js';
import { gradCheck } from './nn/gradcheck.js';

let dataset = null;
let model = null;
let optimizer = null;

// CPU-only mirror model used for live inference + visualizations. Always CPU so
// inference works regardless of the training backend and never disturbs the
// training model's forward/backward state. Weights are synced from the active
// training model on demand (cheap CPU copies, or GPU readback when webgpu).
let inferModel = buildModel(cpuBackend, new RNG(123456789 >>> 0));
function rebuildInferModel() {
  inferModel = buildModel(cpuBackend, new RNG(123456789 >>> 0));
}

// --- backend state ----------------------------------------------------------
let preference = 'auto'; // 'auto' | 'cpu' | 'webgpu' (from UI, applied on reset)
let activeBackend = 'cpu'; // 'cpu' | 'webgpu' — what is actually running
let backendReason = 'initializing';
let gpuProbe = { ok: false, device: null, reason: 'navigator.gpu undefined' };
let gpuBackend = null; // GpuBackend instance (only when webgpu active)

function isGpu() { return activeBackend === 'webgpu' && gpuBackend && !gpuBackend.lost; }

// Probe WebGPU once at startup (non-fatal if unavailable).
(async () => {
  try {
    gpuProbe = await probeGpu();
  } catch (e) {
    gpuProbe = { ok: false, device: null, reason: String((e && e.message) || e) };
  }
  if (gpuProbe.ok) console.info('[NeuraLab] WebGPU adapter available — selectable via the backend control.');
  else console.info('[NeuraLab] WebGPU unavailable (' + gpuProbe.reason + ') — using CPU backend.');
  applyBackend();
  if (isGpu()) freshModel(); // rebuild for the GPU backend once it's chosen
  self.postMessage({ type: 'backend-info', backend: activeBackend, backendReason, webgpuAvailable: !!gpuProbe.ok });
})();

// Resolve the preference + probe into an active backend + reason.
function applyBackend() {
  if (preference === 'cpu') {
    activeBackend = 'cpu';
    backendReason = 'CPU selected';
    return;
  }
  // 'auto' or 'webgpu'
  const wantGpu = gpuProbe.ok && gpuProbe.device && !(gpuProbeBackendLost());
  if (wantGpu) {
    try {
      gpuBackend = gpuBackend || new GpuBackend(gpuProbe.device);
      activeBackend = 'webgpu';
      backendReason = '';
      return;
    } catch (e) {
      backendReason = 'WebGPU init failed: ' + String((e && e.message) || e);
    }
  } else {
    backendReason = preference === 'webgpu'
      ? 'WebGPU unavailable: ' + gpuProbe.reason
      : gpuProbe.reason || 'WebGPU unavailable';
  }
  activeBackend = 'cpu';
}

function gpuProbeBackendLost() {
  return !!(gpuBackend && gpuBackend.lost);
}

// Hyperparameters.
let lr = 0.05;
let momentum = 0.9;
let batchSize = 32;
let maxNorm = 2.0; // global grad-norm clip
const LR_DECAY_EVERY = 1200;
const LR_DECAY_FACTOR = 0.5;
const LR_MIN_FACTOR = 0.1;
function effectiveLr(gb) {
  const halves = Math.floor(gb / LR_DECAY_EVERY);
  return Math.max(lr * LR_MIN_FACTOR, lr * Math.pow(LR_DECAY_FACTOR, halves));
}

// Training state.
let running = false;
let runId = 0;
let epoch = 0;
let batchInEpoch = 0;
let globalBatch = 0;
let cursor = 0;
let order = null;
let lastEvalTrainAcc = 0;
let lastEvalTestAcc = 0;
let lastLoss = 0;

// samples/sec meter — TRAIN ONLY (eval time excluded via pause/resume).
let meterSamples = 0;
let meterSegStart = 0;
let meterAccumMs = 0;
function meterReset() { meterSamples = 0; meterAccumMs = 0; meterSegStart = performance.now(); }
function meterPause() { meterAccumMs += performance.now() - meterSegStart; }
function meterResume() { meterSegStart = performance.now(); }
function samplesPerSec() {
  const t = meterAccumMs + (performance.now() - meterSegStart);
  if (t <= 0) return 0;
  return meterSamples / (t / 1000);
}

function freshModel() {
  const rng = new RNG(123456789 >>> 0); // fixed model init seed (reproducible weights)
  if (isGpu()) {
    const factory = makeGpuBackend(gpuBackend);
    model = buildModel(factory, rng);
    optimizer = new GpuMomentumSGD(lr, momentum, maxNorm);
  } else {
    model = buildModel(cpuBackend, rng);
    optimizer = new MomentumSGD(lr, momentum, maxNorm);
  }
}

freshModel();

// --- inference model weight sync --------------------------------------------
// Copy current weights from the active training model into inferModel. For the
// GPU backend the weights live in GPU buffers and are read back; reading stale
// (mid-update) weights is harmless for a live preview. CPU just array-copies.
async function syncInferWeights() {
  const im = inferModel;
  if (!model) return;
  const c1 = model.refs.conv1, c2 = model.refs.conv2, d = model.refs.dense;
  const imc1 = im.refs.conv1, imc2 = im.refs.conv2, imd = im.refs.dense;
  if (isGpu()) {
    const [c1W, c1b, c2W, c2b, dW, db] = await Promise.all([
      gpuBackend.readF32(c1._wBuf, c1.W.length),
      gpuBackend.readF32(c1._bBuf, c1.b.length),
      gpuBackend.readF32(c2._wBuf, c2.W.length),
      gpuBackend.readF32(c2._bBuf, c2.b.length),
      gpuBackend.readF32(d._wBuf, d.W.length),
      gpuBackend.readF32(d._bBuf, d.b.length),
    ]);
    imc1.W.set(c1W); imc1.b.set(c1b);
    imc2.W.set(c2W); imc2.b.set(c2b);
    imd.W.set(dW); imd.b.set(db);
  } else {
    imc1.W.set(c1.W); imc1.b.set(c1.b);
    imc2.W.set(c2.W); imc2.b.set(c2.b);
    imd.W.set(d.W); imd.b.set(d.b);
  }
}

// Clean staged forward capturing activations (independent of buffer reuse quirks).
function inferForwardStaged(pixels) {
  const x = { data: pixels, shape: [1, 1, 16, 16] };
  const z1 = inferModel.refs.conv1.forward(x);   // [1,8,16,16]
  const a1 = inferModel.refs.relu1.forward(z1);
  const p1 = inferModel.refs.pool1.forward(a1);  // [1,8,8,8]
  const z2 = inferModel.refs.conv2.forward(p1);  // [1,16,8,8]
  const a2 = inferModel.refs.relu2.forward(z2);
  const p2 = inferModel.refs.pool2.forward(a2);  // [1,16,4,4]
  const f = inferModel.refs.flat.forward(p2);
  const logits = inferModel.refs.dense.forward(f);
  // buildModel() returns smce as a SIBLING of refs (not inside refs); use it
  // directly. Passing a label of 0 is harmless — we only need the probs.
  const { probs } = inferModel.smce.forward(logits, new Int32Array([0]));
  return {
    probs: Array.from(probs),
    conv1: Array.from(a1.data),  // 8*16*16
    conv2: Array.from(a2.data),  // 16*8*8
    filters: { conv1: Array.from(inferModel.refs.conv1.W) },
  };
}

// GPU input buffer (reused across batches, resized if the batch grows).
let gpuInputBuf = null;
function ensureInputBuf(byteLen) {
  if (!gpuInputBuf || gpuInputBuf.size < byteLen) {
    if (gpuInputBuf) gpuInputBuf.destroy();
    gpuInputBuf = gpuBackend.storage(byteLen);
  }
  return gpuInputBuf;
}

function shuffleOrder() {
  const n = dataset.trainY.length;
  order = new Int32Array(n);
  for (let i = 0; i < n; i++) order[i] = i;
  const rng = new RNG((123456789 ^ (epoch * 2654435761)) >>> 0);
  rng.shuffle(order);
}

function nextBatch() {
  const n = dataset.trainY.length;
  const idxs = new Int32Array(batchSize);
  for (let i = 0; i < batchSize; i++) {
    if (cursor >= n) {
      epoch++;
      batchInEpoch = 0;
      cursor = 0;
      shuffleOrder();
    }
    idxs[i] = order[cursor++];
  }
  const xb = new Float32Array(batchSize * IMGLEN);
  const yb = new Int32Array(batchSize);
  for (let i = 0; i < batchSize; i++) {
    const src = idxs[i];
    xb.set(dataset.trainX.subarray(src * IMGLEN, (src + 1) * IMGLEN), i * IMGLEN);
    yb[i] = dataset.trainY[src];
  }
  return { xb, yb };
}

function postTrain(extra) {
  self.postMessage(
    Object.assign(
      {
        type: 'train',
        runId,
        epoch,
        batch: batchInEpoch,
        globalBatch,
        loss: lastLoss,
        trainAcc: lastEvalTrainAcc,
        testAcc: lastEvalTestAcc,
        samplesPerSec: samplesPerSec(),
        effLr: optimizer.lr,
        backend: activeBackend,
        backendReason,
      },
      extra || {}
    )
  );
}

// --- CPU training step (proven round-2 path, synchronous) -------------------
function trainStepBatchCpu() {
  const prevEpoch = epoch;
  const { xb, yb } = nextBatch();
  const logits = modelForward(model, { data: xb, shape: [batchSize, 1, 16, 16] });
  const { loss } = model.smce.forward(logits, yb);
  lastLoss = loss;
  modelBackward(model);
  optimizer.lr = effectiveLr(globalBatch);
  optimizer.step(allParams(model));
  batchInEpoch++;
  globalBatch++;
  meterSamples += batchSize;
  let accPoint = null;
  if (globalBatch % 200 === 0 || epoch !== prevEpoch) {
    meterPause();
    evalBothCpu();
    meterResume();
    accPoint = { accBatch: globalBatch };
  }
  postTrain({ lossBatch: globalBatch, accPoint });
}

function evalBothCpu() {
  lastEvalTrainAcc = accuracy(model, dataset.trainX.subarray(0, 1000 * IMGLEN), dataset.trainY.subarray(0, 1000), IMGLEN);
  lastEvalTestAcc = accuracy(model, dataset.testX, dataset.testY, IMGLEN);
}

// --- GPU training step (async; weights/velocities resident on GPU) ----------
async function trainStepBatchGpu() {
  const prevEpoch = epoch;
  const { xb, yb } = nextBatch();
  gpuBackend.writeN(batchSize);
  const inp = ensureInputBuf(batchSize * IMGLEN * 4);
  gpuBackend.upload(inp, xb);
  const logits = await modelForwardAsync(model, { data: inp, shape: [batchSize, 1, 16, 16] });
  const { loss } = await model.smce.forward(logits, yb);
  lastLoss = loss;
  await modelBackwardAsync(model);
  optimizer.lr = effectiveLr(globalBatch);
  await optimizer.step(allParams(model), gpuBackend);
  batchInEpoch++;
  globalBatch++;
  meterSamples += batchSize;
  let accPoint = null;
  if (globalBatch % 200 === 0 || epoch !== prevEpoch) {
    meterPause();
    await evalBothGpu();
    meterResume();
    accPoint = { accBatch: globalBatch };
  }
  postTrain({ lossBatch: globalBatch, accPoint });
}

async function evalBothGpu() {
  lastEvalTrainAcc = await evalAccGpu(dataset.trainX.subarray(0, 1000 * IMGLEN), dataset.trainY.subarray(0, 1000));
  lastEvalTestAcc = await evalAccGpu(dataset.testX, dataset.testY);
}

async function evalAccGpu(X, Y) {
  const n = Y.length;
  const imgLen = IMGLEN;
  let correct = 0;
  const bs = 256;
  for (let s = 0; s < n; s += bs) {
    const b = Math.min(bs, n - s);
    const xb = new Float32Array(b * imgLen);
    for (let i = 0; i < b; i++) xb.set(X.subarray((s + i) * imgLen, (s + i + 1) * imgLen), i * imgLen);
    gpuBackend.writeN(b);
    const inp = ensureInputBuf(b * imgLen * 4);
    gpuBackend.upload(inp, xb);
    const logits = await modelForwardAsync(model, { data: inp, shape: [b, 1, 16, 16] });
    const ld = await gpuBackend.readF32(logits.data, b * NUM_CLASSES);
    for (let i = 0; i < b; i++) {
      let mx = -Infinity, mi = 0;
      const off = i * NUM_CLASSES;
      for (let k = 0; k < NUM_CLASSES; k++) { if (ld[off + k] > mx) { mx = ld[off + k]; mi = k; } }
      if (mi === Y[s + i]) correct++;
    }
  }
  return correct / n;
}

// Unified step dispatcher (kept tiny so the loop reads cleanly).
async function trainStepBatch() {
  if (isGpu()) await trainStepBatchGpu();
  else trainStepBatchCpu();
}

// Time-sliced training loop. For CPU this is a tight sync loop yielding on
// setTimeout; for GPU each step awaits GPU completion.
async function trainLoop() {
  if (!running) return;
  const budget = 8; // ms before yielding
  const t0 = performance.now();
  while (running) {
    await trainStepBatch();
    if (performance.now() - t0 > budget) break;
  }
  if (running) setTimeout(trainLoop, 0);
}

async function handleGenerate(msg) {
  try {
    const seed = (msg.seed | 0) || 42;
    dataset = await createDataset({
      seed,
      onProgress: (p) => self.postMessage({ type: 'gen-progress', pct: p }),
      yieldFn: () => new Promise((r) => setTimeout(r, 0)),
    });
    const display = displaySamples(dataset, 8);
    self.postMessage({
      type: 'dataset-ready',
      display,
      seed,
      trainSize: dataset.trainY.length,
      testSize: dataset.testY.length,
      backend: activeBackend,
      backendReason,
      webgpuAvailable: !!gpuProbe.ok,
    });
  } catch (e) {
    self.postMessage({ type: 'error', where: 'generate', message: String((e && e.message) || e) });
  }
}

function startTraining() {
  if (!dataset) return;
  if (running) return;
  running = true;
  runId++;
  if (!order) shuffleOrder();
  meterReset();
  self.postMessage({ type: 'started', runId, backend: activeBackend, backendReason });
  setTimeout(trainLoop, 0);
}

function pauseTraining() {
  running = false;
  self.postMessage({ type: 'paused', runId });
}

function resetTraining() {
  running = false;
  runId++;
  epoch = 0;
  batchInEpoch = 0;
  globalBatch = 0;
  cursor = 0;
  order = null;
  lastEvalTrainAcc = 0;
  lastEvalTestAcc = 0;
  lastLoss = 0;
  meterReset();
  freshModel();
  rebuildInferModel();
  self.postMessage({
    type: 'reset-done',
    runId,
    backend: activeBackend,
    backendReason,
    webgpuAvailable: !!gpuProbe.ok,
  });
}

// Backend parity test: run one forward+backward on a fixed seeded batch (16) on
// BOTH backends and report per-tensor max abs diff. If WebGPU is unavailable,
// returns {available:false} so the UI shows a graceful notice.
async function runParity() {
  if (!gpuProbe.ok || !gpuProbe.device) {
    return { available: false };
  }
  try {
    let ds = dataset;
    if (!ds) ds = await createDataset({ seed: 7, trainSize: 64, testSize: 16 });
    const N = 16;
    const xb = new Float32Array(N * IMGLEN);
    const yb = new Int32Array(N);
    for (let i = 0; i < N; i++) {
      xb.set(ds.trainX.subarray(i * IMGLEN, (i + 1) * IMGLEN), i * IMGLEN);
      yb[i] = ds.trainY[i];
    }
    // CPU model.
    const cpuModel = buildModel(cpuBackend, new RNG(987654321 >>> 0));
    const cLogits = modelForward(cpuModel, { data: xb, shape: [N, 1, 16, 16] });
    const { loss: cLoss } = cpuModel.smce.forward(cLogits, yb);
    modelBackward(cpuModel);
    // GPU model (identical weights: same seed + same init order).
    const gb = new GpuBackend(gpuProbe.device);
    const factory = makeGpuBackend(gb);
    const gpuModel = buildModel(factory, new RNG(987654321 >>> 0));
    gb.writeN(N);
    const inp = gb.storage(N * IMGLEN * 4);
    gb.upload(inp, xb);
    const gLogits = await modelForwardAsync(gpuModel, { data: inp, shape: [N, 1, 16, 16] });
    const { loss: gLoss } = await gpuModel.smce.forward(gLogits, yb);
    await modelBackwardAsync(gpuModel);
    // read back grads for comparison
    await gpuModel.refs.conv1.syncGrads();
    await gpuModel.refs.conv2.syncGrads();
    await gpuModel.refs.dense.syncGrads();
    const gLogitsArr = await gb.readF32(gLogits.data, N * NUM_CLASSES);
    const cLogitsArr = cLogits.data;
    const detail = {
      logits: maxAbsDiff(cLogitsArr, gLogitsArr),
      loss: Math.abs(cLoss - gLoss),
      'conv1.dW': maxAbsDiff(cpuModel.refs.conv1.gW, gpuModel.refs.conv1.gW),
      'conv2.dW': maxAbsDiff(cpuModel.refs.conv2.gW, gpuModel.refs.conv2.gW),
      'dense.dW': maxAbsDiff(cpuModel.refs.dense.gW, gpuModel.refs.dense.gW),
    };
    const maxDiff = Math.max(detail.logits, detail.loss, detail['conv1.dW'], detail['conv2.dW'], detail['dense.dW']);
    return { available: true, pass: maxDiff < 1e-3, maxDiff, detail };
  } catch (e) {
    return { available: false, error: String((e && e.message) || e) };
  }
}

function maxAbsDiff(a, b) {
  const n = Math.min(a.length, b.length);
  let m = 0;
  for (let i = 0; i < n; i++) { const d = Math.abs(a[i] - b[i]); if (d > m) m = d; }
  return m;
}

async function runGradCheck(msg) {
  try {
    let ds = dataset;
    if (!ds) ds = await createDataset({ seed: 7, trainSize: 80, testSize: 10 });
    const gcModel = buildModel(cpuBackend, new RNG(987654321 >>> 0));
    const xb = new Float32Array(8 * IMGLEN);
    const yb = new Int32Array(8);
    for (let i = 0; i < 8; i++) {
      xb.set(ds.trainX.subarray(i * IMGLEN, (i + 1) * IMGLEN), i * IMGLEN);
      yb[i] = ds.trainY[i];
    }
    const res = gradCheck(gcModel, { data: xb, shape: [8, 1, 16, 16] }, yb);
    self.postMessage({ type: 'gradcheck', runId: msg.runId, result: res });
  } catch (e) {
    self.postMessage({ type: 'gradcheck', runId: msg.runId, result: { pass: false, error: String((e && e.message) || e) } });
  }
}

// --- inference / playground requests (handled async, never throw) -----------
const inferCallbacks = new Map();
let inferSeq = 0;

async function handleInfer(msg) {
  const seq = msg.seq != null ? msg.seq : ++inferSeq;
  try {
    let pixels = msg.pixels;
    if (!pixels) pixels = new Float32Array(IMGLEN);
    else pixels = Float32Array.from(pixels);
    if (pixels.length !== IMGLEN) {
      // pad/truncate defensively
      const p = new Float32Array(IMGLEN);
      p.set(pixels.subarray(0, Math.min(pixels.length, IMGLEN)));
      pixels = p;
    }
    await syncInferWeights();
    const res = inferForwardStaged(pixels);
    self.postMessage({ type: 'infer', seq, result: res });
  } catch (e) {
    // Mirror handleConfusion: never let an inference rejection go unreported,
    // otherwise the main-thread rpc waits forever and the bars stay "—".
    self.postMessage({ type: 'infer', seq, result: { error: String((e && e.message) || e) } });
  }
}

async function handleStamp(msg) {
  if (!dataset) {
    self.postMessage({ type: 'stamp', seq: msg.seq, pixels: null });
    return;
  }
  // collect test indices of the requested class, pick one at (seeded) random
  const cls = (msg.classIndex | 0) % NUM_CLASSES;
  const idxs = [];
  for (let i = 0; i < dataset.testY.length; i++) if (dataset.testY[i] === cls) idxs.push(i);
  const pick = idxs.length ? idxs[Math.floor(Math.random() * idxs.length)] : 0;
  const px = dataset.testX.slice(pick * IMGLEN, (pick + 1) * IMGLEN);
  self.postMessage({ type: 'stamp', seq: msg.seq, classIndex: cls, pixels: Array.from(px) });
}

// Chunked confusion matrix over the test set, evaluated on inferModel (CPU).
async function handleConfusion(msg) {
  try {
    if (!dataset) {
      self.postMessage({ type: 'confusion', seq: msg.seq, result: { matrix: null } });
      return;
    }
    await syncInferWeights();
    const n = dataset.testY.length;
    const matrix = Array.from({ length: NUM_CLASSES }, () => new Int32Array(NUM_CLASSES));
    const bs = 64;
    const imgLen = IMGLEN;
    for (let s = 0; s < n; s += bs) {
      const b = Math.min(bs, n - s);
      const xb = new Float32Array(b * imgLen);
      for (let i = 0; i < b; i++) xb.set(dataset.testX.subarray((s + i) * imgLen, (s + i + 1) * imgLen), i * imgLen);
      const logits = modelForward(inferModel, { data: xb, shape: [b, 1, 16, 16] });
      for (let i = 0; i < b; i++) {
        let mx = -Infinity, mi = 0;
        const off = i * NUM_CLASSES;
        for (let k = 0; k < NUM_CLASSES; k++) if (logits.data[off + k] > mx) { mx = logits.data[off + k]; mi = k; }
        matrix[dataset.testY[s + i]][mi]++;
      }
      // yield so the worker stays responsive on large sets
      if ((s & 255) === 0) await new Promise((r) => setTimeout(r, 0));
    }
    self.postMessage({ type: 'confusion', seq: msg.seq, result: { matrix: matrix.map((r) => Array.from(r)) } });
  } catch (e) {
    self.postMessage({ type: 'confusion', seq: msg.seq, result: { error: String((e && e.message) || e) } });
  }
}

self.onmessage = (ev) => {
  const msg = ev.data || {};
  try {
    switch (msg.type) {
      case 'generate':
        handleGenerate(msg);
        break;
      case 'start':
        startTraining();
        break;
      case 'pause':
        pauseTraining();
        break;
      case 'reset':
        // apply the user's backend preference, then reset.
        if (msg.preference) preference = msg.preference;
        applyBackend();
        resetTraining();
        break;
      case 'setBackend':
        // just record preference (applied on next reset) + re-report state.
        if (msg.preference) preference = msg.preference;
        self.postMessage({ type: 'backend-info', backend: activeBackend, backendReason, webgpuAvailable: !!gpuProbe.ok });
        break;
      case 'getBackendInfo':
        self.postMessage({ type: 'backend-info', backend: activeBackend, backendReason, webgpuAvailable: !!gpuProbe.ok });
        break;
      case 'parity':
        runParity().then((r) => self.postMessage({ type: 'parity', seq: msg.seq, result: r }));
        break;
      case 'setHyper':
        if (typeof msg.lr === 'number') lr = msg.lr;
        if (typeof msg.momentum === 'number') momentum = msg.momentum;
        if (typeof msg.batchSize === 'number') batchSize = msg.batchSize;
        if (typeof msg.maxNorm === 'number') maxNorm = msg.maxNorm;
        if (optimizer) {
          optimizer.lr = effectiveLr(globalBatch);
          optimizer.momentum = momentum;
          optimizer.maxNorm = maxNorm;
        }
        break;
      case 'gradcheck':
        runGradCheck(msg);
        break;
      case 'infer':
        handleInfer(msg);
        break;
      case 'stamp':
        handleStamp(msg);
        break;
      case 'confusion':
        handleConfusion(msg);
        break;
      default:
        break;
    }
  } catch (e) {
    self.postMessage({ type: 'error', where: 'message', message: String((e && e.message) || e) });
  }
};

// Kick off dataset generation as soon as the worker loads.
handleGenerate({ seed: 42 });
