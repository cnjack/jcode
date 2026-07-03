// js/main.js
// UI wiring + app state. Talks to the worker, keeps window.__trainState fresh,
// exposes window.__app and window.__gradCheck for automation, draws the dataset
// explorer and the live charts.

import { LineChart } from './charts.js';
import { IMG } from './dataset.js';
import { initLab } from './lab.js';

const worker = new Worker('./js/worker.js', { type: 'module' });

const trainState = {
  running: false,
  epoch: 0,
  batch: 0,
  loss: 0,
  trainAcc: 0,
  testAcc: 0,
  samplesPerSec: 0,
  bestTestAcc: 0,
  lr: 0,
  datasetReady: false,
  backend: 'cpu',
  backendReason: 'initializing',
  webgpuAvailable: false,
};
window.__trainState = trainState;

const els = {
  epoch: document.getElementById('stat-epoch'),
  batch: document.getElementById('stat-batch'),
  loss: document.getElementById('stat-loss'),
  trainAcc: document.getElementById('stat-train-acc'),
  testAcc: document.getElementById('stat-test-acc'),
  bestAcc: document.getElementById('stat-best-acc'),
  sps: document.getElementById('stat-sps'),
  lrEl: document.getElementById('stat-lr'),
  status: document.getElementById('train-status'),
  startBtn: document.getElementById('btn-start'),
  pauseBtn: document.getElementById('btn-pause'),
  resetBtn: document.getElementById('btn-reset'),
  gcBtn: document.getElementById('btn-gradcheck'),
  gcResult: document.getElementById('gradcheck-result'),
  seedInput: document.getElementById('seed-input'),
  regenBtn: document.getElementById('btn-regenerate'),
  genBar: document.getElementById('gen-bar'),
  genPct: document.getElementById('gen-pct'),
  explorer: document.getElementById('explorer'),
  lossChartEl: document.getElementById('loss-chart'),
  accChartEl: document.getElementById('acc-chart'),
  badge: document.getElementById('backend-badge'),
  backendRadios: document.getElementsByName('backend-radio'),
  gpuRadio: document.getElementById('backend-radio-webgpu'),
  gpuRadioWrap: document.getElementById('backend-radio-webgpu-wrap'),
  parityBtn: document.getElementById('btn-parity'),
  parityResult: document.getElementById('parity-result'),
};

let activeRunId = 0;
let lossEma = 0;
let bestTestAcc = 0;
let lossChart, accChart;

function initCharts() {
  lossChart = new LineChart(els.lossChartEl, {
    series: [{ name: 'loss (smoothed)', color: '#5ad1ff' }],
    ylabel: 'cross-entropy',
    xlabel: 'batch',
    yfmt: (v) => v.toFixed(2),
  });
  accChart = new LineChart(els.accChartEl, {
    series: [
      { name: 'train', color: '#7CFFB2' },
      { name: 'test', color: '#FFB454' },
      { name: 'best', color: '#5b6b88' },
    ],
    ylabel: 'accuracy',
    xlabel: 'batch',
    yMin: 0,
    yMax: 1,
    yfmt: (v) => v.toFixed(2),
  });
}

function setText(el, t) {
  if (el) el.textContent = t;
}

function updateStatsUI() {
  setText(els.epoch, String(trainState.epoch));
  setText(els.batch, String(trainState.batch));
  setText(els.loss, trainState.loss ? trainState.loss.toFixed(4) : '—');
  setText(els.trainAcc, trainState.trainAcc ? (trainState.trainAcc * 100).toFixed(1) + '%' : '—');
  setText(els.testAcc, trainState.testAcc ? (trainState.testAcc * 100).toFixed(1) + '%' : '—');
  setText(els.sps, trainState.samplesPerSec ? Math.round(trainState.samplesPerSec) + ' /s' : '—');
  setText(els.bestAcc, bestTestAcc ? (bestTestAcc * 100).toFixed(1) + '%' : '—');
  setText(els.lrEl, trainState.lr ? trainState.lr.toFixed(4) : '—');
}

// --- backend badge + control ------------------------------------------------
function setBackendState(backend, reason, webgpuAvailable) {
  if (backend) trainState.backend = backend;
  if (typeof reason === 'string') trainState.backendReason = reason;
  if (typeof webgpuAvailable === 'boolean') trainState.webgpuAvailable = webgpuAvailable;
  renderBadge();
  // Disable the WebGPU option (with a tooltip) when the GPU is unavailable.
  if (els.gpuRadio && els.gpuRadioWrap) {
    els.gpuRadio.disabled = !trainState.webgpuAvailable;
    els.gpuRadioWrap.title = trainState.webgpuAvailable
      ? 'Use the WebGPU compute backend'
      : 'WebGPU is not available in this browser';
    els.gpuRadioWrap.classList.toggle('disabled', !trainState.webgpuAvailable);
  }
}

function renderBadge() {
  if (!els.badge) return;
  const gpu = trainState.backend === 'webgpu';
  // Three truthful states:
  //  - WebGPU active
  //  - CPU (selected)        — user picked CPU while WebGPU IS available
  //  - CPU fallback …         — WebGPU genuinely absent
  if (gpu) {
    els.badge.textContent = 'WebGPU active';
  } else if (trainState.webgpuAvailable) {
    els.badge.textContent = 'CPU (selected)';
  } else {
    els.badge.textContent = 'CPU fallback — WebGPU unavailable';
  }
  els.badge.classList.toggle('badge-gpu', gpu);
  els.badge.classList.toggle('badge-cpu', !gpu);
  els.badge.title = gpu
    ? 'Training on the GPU via WebGPU'
    : trainState.webgpuAvailable
      ? 'CPU backend selected by the user (WebGPU is available)'
      : (trainState.backendReason || 'WebGPU unavailable — running on CPU');
}

// --- dataset explorer -------------------------------------------------------
function drawThumb(canvas, data) {
  const dpr = window.devicePixelRatio || 1;
  const size = canvas.clientWidth || 44; // fallback when not yet laid out
  canvas.width = size * dpr;
  canvas.height = size * dpr;
  const ctx = canvas.getContext('2d');
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.imageSmoothingEnabled = false;
  // Render into an ImageData at native 16x16, then upscale via drawImage.
  const src = document.createElement('canvas');
  src.width = IMG;
  src.height = IMG;
  const sctx = src.getContext('2d');
  const img = sctx.createImageData(IMG, IMG);
  for (let i = 0; i < data.length; i++) {
    const v = Math.max(0, Math.min(255, (data[i] * 255) | 0));
    img.data[i * 4] = v;
    img.data[i * 4 + 1] = v;
    img.data[i * 4 + 2] = v;
    img.data[i * 4 + 3] = 255;
  }
  sctx.putImageData(img, 0, 0);
  ctx.fillStyle = '#000';
  ctx.fillRect(0, 0, size, size);
  ctx.drawImage(src, 0, 0, IMG, IMG, 0, 0, size, size);
}

function renderExplorer(display) {
  els.explorer.innerHTML = '';
  // Group by label.
  const byClass = new Map();
  for (const s of display) {
    if (!byClass.has(s.label)) byClass.set(s.label, []);
    byClass.get(s.label).push(s);
  }
  const pending = []; // { canvas, data } — drawn once the DOM is attached
  for (let label = 0; label < 10; label++) {
    const row = document.createElement('div');
    row.className = 'digit-row';
    const cap = document.createElement('div');
    cap.className = 'digit-row-label';
    cap.textContent = String(label);
    row.appendChild(cap);
    const thumbs = document.createElement('div');
    thumbs.className = 'digit-row-thumbs';
    const arr = byClass.get(label) || [];
    for (const s of arr) {
      const c = document.createElement('canvas');
      c.className = 'thumb';
      c.title = `digit ${label}`;
      thumbs.appendChild(c);
      pending.push({ canvas: c, data: s.data });
    }
    row.appendChild(thumbs);
    els.explorer.appendChild(row);
  }
  // Second pass: every canvas is now attached + laid out, so clientWidth is
  // real (40px from CSS) instead of 0. This is what previously made all
  // thumbnails render black.
  for (const p of pending) drawThumb(p.canvas, p.data);
}

// --- message handling -------------------------------------------------------
function onMessage(ev) {
  const msg = ev.data || {};
  switch (msg.type) {
    case 'gen-progress': {
      const pct = Math.round(msg.pct * 100);
      els.genBar.style.width = pct + '%';
      els.genPct.textContent = pct + '%';
      break;
    }
    case 'dataset-ready': {
      trainState.datasetReady = true;
      if (msg.backend) setBackendState(msg.backend, msg.backendReason, msg.webgpuAvailable);
      els.genBar.style.width = '100%';
      els.genPct.textContent = '100%';
      setText(els.status, 'Dataset ready — press Start to train.');
      renderExplorer(msg.display);
      els.seedInput.value = String(msg.seed);
      updateStatsUI();
      break;
    }
    case 'backend-info': {
      setBackendState(msg.backend, msg.backendReason, msg.webgpuAvailable);
      break;
    }
    case 'started': {
      activeRunId = msg.runId;
      trainState.running = true;
      if (msg.backend) setBackendState(msg.backend, msg.backendReason, msg.webgpuAvailable);
      setText(els.status, 'Training…');
      break;
    }
    case 'paused': {
      trainState.running = false;
      setText(els.status, 'Paused.');
      break;
    }
    case 'reset-done': {
      activeRunId = msg.runId;
      trainState.running = false;
      trainState.epoch = 0;
      trainState.batch = 0;
      trainState.loss = 0;
      trainState.trainAcc = 0;
      trainState.testAcc = 0;
      trainState.samplesPerSec = 0;
      trainState.bestTestAcc = 0;
      trainState.lr = 0;
      lossEma = 0;
      bestTestAcc = 0;
      lossChart.reset();
      accChart.reset();
      lossChart.draw();
      accChart.draw();
      if (msg.backend) setBackendState(msg.backend, msg.backendReason, msg.webgpuAvailable);
      setText(els.status, 'Reset. Ready.');
      updateStatsUI();
      break;
    }
    case 'train': {
      if (msg.runId !== activeRunId) break; // stale
      trainState.running = true;
      if (msg.backend) setBackendState(msg.backend, msg.backendReason, msg.webgpuAvailable);
      trainState.epoch = msg.epoch;
      trainState.batch = msg.batch;
      trainState.loss = msg.loss;
      trainState.trainAcc = msg.trainAcc;
      trainState.testAcc = msg.testAcc;
      trainState.samplesPerSec = msg.samplesPerSec;
      trainState.lr = msg.effLr;
      if (trainState.testAcc > bestTestAcc) bestTestAcc = trainState.testAcc;
      trainState.bestTestAcc = bestTestAcc;
      // loss chart (EMA smoothed)
      const a = 0.08;
      lossEma = lossEma === 0 ? msg.loss : (1 - a) * lossEma + a * msg.loss;
      if (msg.lossBatch != null) lossChart.push('loss (smoothed)', msg.lossBatch, lossEma);
      lossChart.draw();
      // accuracy chart
      if (msg.accPoint) {
        accChart.push('train', msg.accPoint.accBatch, msg.trainAcc);
        accChart.push('test', msg.accPoint.accBatch, msg.testAcc);
        accChart.push('best', msg.accPoint.accBatch, bestTestAcc);
        accChart.draw();
      }
      updateStatsUI();
      break;
    }
    case 'gradcheck': {
      const cb = gradCheckCallbacks.get(msg.runId);
      if (cb) {
        gradCheckCallbacks.delete(msg.runId);
        cb(msg.result || { pass: false });
      }
      renderGradCheckResult(msg.result);
      break;
    }
    case 'parity': {
      const cb = parityCallbacks.get(msg.seq);
      if (cb) { parityCallbacks.delete(msg.seq); cb(msg.result || { available: false }); }
      renderParityResult(msg.result);
      break;
    }
    case 'error': {
      console.error('[worker error]', msg.where, msg.message);
      setText(els.status, 'Error: ' + msg.message);
      break;
    }
    default:
      break;
  }
}
worker.onmessage = onMessage;

// --- gradient check ---------------------------------------------------------
const gradCheckCallbacks = new Map();
let gradCheckSeq = 0;

function renderGradCheckResult(r) {
  if (!r) {
    els.gcResult.innerHTML = '<span class="gc-fail">No result.</span>';
    return;
  }
  if (r.error) {
    els.gcResult.innerHTML = `<span class="gc-fail">Error: ${r.error}</span>`;
    return;
  }
  const verdict = r.pass
    ? '<span class="gc-pass">PASS</span>'
    : '<span class="gc-fail">FAIL</span>';
  let rows = '';
  for (const d of r.details) {
    let relCell;
    if (d.kinked) relCell = '<i>kink (excluded)</i>';
    else if (d.nearZero) relCell = '<i>~0 (excluded)</i>';
    else relCell = d.relErr.toExponential(2);
    rows += `<tr><td>${d.label}</td><td>${d.analytic.toExponential(2)}</td><td>${d.numerical.toExponential(2)}</td><td>${relCell}</td></tr>`;
  }
  els.gcResult.innerHTML = `
    <div class="gc-summary">${verdict}
      max rel err = <b>${r.maxRelErr.toExponential(2)}</b> ·
      mean = <b>${r.meanRelErr.toExponential(2)}</b> ·
      checked ${r.used} · excluded ${r.kinked} kinked + ${r.skipped} near-zero · threshold 1e-2
    </div>
    <table class="gc-table"><thead><tr><th>param</th><th>analytic</th><th>numerical (f64)</th><th>rel err</th></tr></thead><tbody>${rows}</tbody></table>`;
}

async function runGradCheck() {
  els.gcBtn.disabled = true;
  setText(els.gcResult, 'Running gradient check…');
  const myId = ++gradCheckSeq;
  const p = new Promise((resolve) => gradCheckCallbacks.set(myId, resolve));
  worker.postMessage({ type: 'gradcheck', runId: myId });
  const res = await p;
  els.gcBtn.disabled = false;
  return res;
}

window.__gradCheck = async function () {
  const r = await runGradCheck();
  return {
    maxRelErr: r.maxRelErr ?? NaN,
    meanRelErr: r.meanRelErr ?? NaN,
    pass: !!r.pass,
  };
};

// --- backend parity test ----------------------------------------------------
const parityCallbacks = new Map();
let paritySeq = 0;

function renderParityResult(r) {
  if (!els.parityResult) return;
  if (!r || !r.available) {
    // Graceful, no error — including when WebGPU is simply absent.
    els.parityResult.innerHTML =
      '<span class="parity-notice">WebGPU not available in this browser — parity test needs both backends.</span>';
    return;
  }
  if (r.error) {
    els.parityResult.innerHTML = '<span class="gc-fail">Parity test error: ' + r.error + '</span>';
    return;
  }
  const verdict = r.pass
    ? '<span class="gc-pass">PASS</span>'
    : '<span class="gc-fail">FAIL</span>';
  let rows = '';
  for (const k of Object.keys(r.detail || {})) {
    rows += '<tr><td>' + k + '</td><td>' + (r.detail[k] || 0).toExponential(2) + '</td></tr>';
  }
  els.parityResult.innerHTML =
    '<div class="gc-summary">' + verdict + ' max abs diff = <b>' + (r.maxDiff || 0).toExponential(2) +
    '</b> · threshold 1e-3</div>' +
    '<table class="gc-table"><thead><tr><th>tensor</th><th>max abs diff (CPU vs WebGPU)</th></tr></thead><tbody>' +
    rows + '</tbody></table>';
}

async function runParity() {
  if (els.parityBtn) els.parityBtn.disabled = true;
  if (els.parityResult) els.parityResult.innerHTML = '<span class="muted">Running parity test…</span>';
  const seq = ++paritySeq;
  const p = new Promise((resolve) => parityCallbacks.set(seq, resolve));
  worker.postMessage({ type: 'parity', seq });
  const res = await p;
  if (els.parityBtn) els.parityBtn.disabled = false;
  return res;
}

// Honest, never-throwing programmatic entry point for automation.
window.__parityTest = async function () {
  try {
    const r = await runParity();
    if (!r || !r.available) return { available: false };
    return {
      available: true,
      pass: !!r.pass,
      maxDiff: r.maxDiff ?? NaN,
      detail: r.detail || {},
    };
  } catch (e) {
    return { available: false, error: String((e && e.message) || e) };
  }
};

// --- app API ----------------------------------------------------------------
window.__app = {
  start: () => worker.postMessage({ type: 'start' }),
  pause: () => worker.postMessage({ type: 'pause' }),
  reset: () => worker.postMessage({ type: 'reset' }),
  setHyper: (o) => worker.postMessage(Object.assign({ type: 'setHyper' }, o || {})),
  regenerate: (seed) => {
    trainState.datasetReady = false;
    els.genBar.style.width = '0%';
    els.genPct.textContent = '0%';
    setText(els.status, 'Generating dataset…');
    worker.postMessage({ type: 'generate', seed: (seed | 0) || 42 });
  },
};

// --- button wiring ----------------------------------------------------------
els.startBtn.addEventListener('click', () => worker.postMessage({ type: 'start' }));
els.pauseBtn.addEventListener('click', () => worker.postMessage({ type: 'pause' }));
els.resetBtn.addEventListener('click', () => { applyHyperFromUI(); worker.postMessage({ type: 'reset', preference: selectedBackend() }); });
els.gcBtn.addEventListener('click', runGradCheck);
if (els.parityBtn) els.parityBtn.addEventListener('click', runParity);
els.regenBtn.addEventListener('click', () => {
  const seed = parseInt(els.seedInput.value, 10) || 42;
  window.__app.regenerate(seed);
});

// Backend segmented control (Auto / CPU / WebGPU). Selection is applied on Reset.
function selectedBackend() {
  for (const r of els.backendRadios) if (r.checked) return r.value;
  return 'auto';
}
for (const r of els.backendRadios) {
  r.addEventListener('change', () => worker.postMessage({ type: 'setBackend', preference: r.value }));
}

window.addEventListener('resize', () => {
  if (lossChart) lossChart.resize();
  if (accChart) accChart.resize();
});

// --- hyperparameter panel (applied on Reset) --------------------------------
function applyHyperFromUI() {
  const lrEl = document.getElementById('hyper-lr');
  const momEl = document.getElementById('hyper-momentum');
  const bsEl = document.getElementById('hyper-batch');
  if (!lrEl) return;
  const lr = parseFloat(lrEl.value);
  const momentum = parseFloat(momEl.value);
  const batchSize = parseInt(bsEl.value, 10);
  worker.postMessage({
    type: 'setHyper',
    lr: isFinite(lr) ? lr : 0.05,
    momentum: isFinite(momentum) ? momentum : 0.9,
    batchSize: isFinite(batchSize) ? batchSize : 32,
  });
}

initCharts();
updateStatsUI();
renderBadge();
setText(els.status, 'Generating dataset…');
worker.postMessage({ type: 'getBackendInfo' });

// Inference playground + visualizations (panels 3-5).
try {
  initLab(worker, trainState);
} catch (e) {
  console.error('[NeuraLab] lab init failed:', e);
}
