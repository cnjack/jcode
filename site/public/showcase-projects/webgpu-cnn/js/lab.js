// js/lab.js
// Inference playground + educational visualizations for NeuraLab.
// Owns: the drawing pad (mouse + touch), MNIST-style preprocessing, the live
// probability bars, the model-input preview, conv1 filter tiles, conv1/conv2
// activation maps, the confusion-matrix heatmap, the architecture diagram and
// the explainer wiring. Talks to the worker (which keeps a CPU mirror of the
// current weights) for inference / stamps / confusion. Exposes window.__infer,
// window.__drawPad and window.__confusion for automation.

const IMG = 16;
const IMGLEN = IMG * IMG;
const NUM_CLASSES = 10;
const DISP = 256;          // draw-pad backing-store size (px)
const BRUSH = 16;          // brush diameter at full (DISP) scale
const TARGET = 11.5;       // longest glyph side, in 16-grid units (MNIST ~20/28)

// Visualization tile sizes (CSS px). Fixed at creation so the layout is compact
// and never jumps when the first inference / training tick draws into them.
const FILTER_CELL = 26;          // conv1 3x3 filter -> 78px magnified tile
const FILTER_CSS = 3 * FILTER_CELL;   // 78
const ACT_CELL = 8;              // activation-map magnification
const CONV1_ACT_CSS = 16 * ACT_CELL;  // 128
const CONV2_ACT_CSS = 8 * ACT_CELL;   // 64

// Size a small square visualization canvas up front (devicePixelRatio-scaled)
// and clear it to black so empty tiles don't flash the canvas default 300x150.
function sizeCanvas(canvas, cssW, cssH) {
  const dpr = window.devicePixelRatio || 1;
  canvas.width = Math.round(cssW * dpr);
  canvas.height = Math.round(cssH * dpr);
  canvas.style.width = cssW + 'px';
  canvas.style.height = cssH + 'px';
  const ctx = canvas.getContext('2d');
  ctx.fillStyle = '#000';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
}

// --- colormap helpers (no library) ------------------------------------------
function clamp01(v) { return v < 0 ? 0 : v > 1 ? 1 : v; }

// Sequential black -> teal -> white for activations (value in [lo,hi]).
function seqColor(v, lo, hi) {
  let t = (v - lo) / (hi - lo + 1e-9);
  t = t < 0 ? 0 : t > 1 ? 1 : t;
  // black -> accent teal -> near-white
  const r = Math.round(60 * t * t);
  const g = Math.round(40 + 215 * t);
  const b = Math.round(60 + 160 * t);
  return [r, g, b];
}

// Diverging blue(0,120,255) -> black -> orange(255,150,40) for signed weights.
function divColor(v, lo, hi) {
  const m = Math.max(Math.abs(lo), Math.abs(hi), 1e-9);
  let t = v / m;                 // [-1, 1]
  if (t < 0) {
    const a = clamp01(-t);
    return [Math.round(20 + (0 - 20) * a), Math.round(20 + (120 - 20) * a), Math.round(20 + (255 - 20) * a)];
  }
  const a = clamp01(t);
  return [Math.round(20 + (255 - 20) * a), Math.round(20 + (150 - 20) * a), Math.round(20 + (40 - 20) * a)];
}

// Render a 1-D float array (row-major WxH) to a canvas, scaled up with nearest
// neighbor. colorfn(value, lo, hi) -> [r,g,b]. Optional per-cell normalization.
function paintGrid(canvas, data, w, h, colorfn, opts = {}) {
  if (!canvas) return;
  const px = opts.cell || 1;
  const dpr = window.devicePixelRatio || 1;
  const cssW = w * px;
  const cssH = h * px;
  if (canvas.width !== cssW * dpr) { canvas.width = cssW * dpr; canvas.height = cssH * dpr; }
  canvas.style.width = cssW + 'px';
  canvas.style.height = cssH + 'px';
  const ctx = canvas.getContext('2d');
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.imageSmoothingEnabled = false;
  // compute lo/hi
  let lo = Infinity, hi = -Infinity;
  for (let i = 0; i < data.length; i++) { const v = data[i]; if (v < lo) lo = v; if (v > hi) hi = v; }
  if (!isFinite(lo)) { lo = 0; hi = 1; }
  if (opts.symmetric) { const m = Math.max(Math.abs(lo), Math.abs(hi)); lo = -m; hi = m; }
  // draw to a tiny ImageData then upscale
  const tmp = document.createElement('canvas');
  tmp.width = w; tmp.height = h;
  const tctx = tmp.getContext('2d');
  const img = tctx.createImageData(w, h);
  for (let i = 0; i < data.length; i++) {
    const c = colorfn(data[i], lo, hi);
    img.data[i * 4] = c[0]; img.data[i * 4 + 1] = c[1]; img.data[i * 4 + 2] = c[2]; img.data[i * 4 + 3] = 255;
  }
  tctx.putImageData(img, 0, 0);
  ctx.fillStyle = '#000';
  ctx.fillRect(0, 0, cssW, cssH);
  ctx.drawImage(tmp, 0, 0, w, h, 0, 0, cssW, cssH);
}

// --- worker request/response (seq-correlated) -------------------------------
function makeRpc(worker) {
  const pending = new Map();
  let seq = 0;
  worker.addEventListener('message', (ev) => {
    const m = ev.data || {};
    if ((m.type === 'infer' || m.type === 'stamp' || m.type === 'confusion') && pending.has(m.seq)) {
      const resolve = pending.get(m.seq);
      pending.delete(m.seq);
      resolve(m.result != null ? m.result : m);
    }
  });
  return (msg) => {
    const id = ++seq;
    return new Promise((resolve) => {
      pending.set(id, resolve);
      worker.postMessage(Object.assign({ seq: id }, msg));
    });
  };
}

// --- MNIST-style preprocessing ----------------------------------------------
// Reads a DISP x DISP image (Uint8ClampedArray RGBA) and produces a centered
// 16x16 float tensor: crop to ink bounding box, scale so the longest side is
// ~TARGET grid units, then shift so the ink centroid lands at the grid center.
function preprocess(rgba, disp) {
  const out = new Float32Array(IMGLEN);
  let minx = disp, miny = disp, maxx = -1, maxy = -1;
  let sum = 0, sx = 0, sy = 0;
  for (let y = 0; y < disp; y++) {
    for (let x = 0; x < disp; x++) {
      const v = rgba[(y * disp + x) * 4] / 255; // R channel (grayscale)
      if (v > 0.1) {
        if (x < minx) minx = x; if (x > maxx) maxx = x;
        if (y < miny) miny = y; if (y > maxy) maxy = y;
        sum += v; sx += v * x; sy += v * y;
      }
    }
  }
  if (sum === 0) return out; // blank -> all zeros
  const cx = sx / sum, cy = sy / sum;
  const bw = maxx - minx + 1, bh = maxy - miny + 1;
  const side = Math.max(bw, bh, 1);
  const S = TARGET / side;            // grid units per source pixel
  const center = (IMG - 1) / 2;       // grid center (7.5)
  const sample = (gx, gy) => {
    // inverse map grid -> source pixel, then bilinear
    const px = cx + (gx - center) / S;
    const py = cy + (gy - center) / S;
    const x0 = Math.floor(px), y0 = Math.floor(py);
    const fx = px - x0, fy = py - y0;
    const get = (xx, yy) => {
      if (xx < 0 || yy < 0 || xx >= disp || yy >= disp) return 0;
      return rgba[(yy * disp + xx) * 4] / 255;
    };
    const a = get(x0, y0), b = get(x0 + 1, y0), c = get(x0, y0 + 1), d = get(x0 + 1, y0 + 1);
    return a * (1 - fx) * (1 - fy) + b * fx * (1 - fy) + c * (1 - fx) * fy + d * fx * fy;
  };
  for (let gy = 0; gy < IMG; gy++) {
    for (let gx = 0; gx < IMG; gx++) {
      let v = sample(gx + 0.5, gy + 0.5);
      if (v < 0) v = 0; else if (v > 1) v = 1;
      out[gy * IMG + gx] = v;
    }
  }
  return out;
}

// --- draw pad ---------------------------------------------------------------
class DrawPad {
  constructor(canvas, preview, onChange) {
    this.canvas = canvas;
    this.preview = preview;
    this.onChange = onChange;
    this.drawing = false;
    this.last = null;
    canvas.width = DISP; canvas.height = DISP;
    const ctx = canvas.getContext('2d', { willReadFrequently: true });
    this.ctx = ctx;
    this.clear();
    // unified pointer events cover mouse + touch
    const pos = (e) => {
      const r = canvas.getBoundingClientRect();
      const sx = DISP / r.width, sy = DISP / r.height;
      return { x: (e.clientX - r.left) * sx, y: (e.clientY - r.top) * sy };
    };
    const start = (e) => {
      e.preventDefault();
      this.drawing = true;
      this.last = pos(e);
      this.dot(this.last.x, this.last.y);
      this.changed();
    };
    const move = (e) => {
      if (!this.drawing) return;
      e.preventDefault();
      const p = pos(e);
      this.line(this.last.x, this.last.y, p.x, p.y);
      this.last = p;
      this.changed();
    };
    const end = (e) => { if (this.drawing) { this.drawing = false; this.last = null; } };
    canvas.addEventListener('pointerdown', start);
    canvas.addEventListener('pointermove', move);
    window.addEventListener('pointerup', end);
    canvas.addEventListener('pointercancel', end);
    canvas.style.touchAction = 'none';
  }
  clear() {
    const { ctx } = this;
    ctx.fillStyle = '#000';
    ctx.fillRect(0, 0, DISP, DISP);
    this.changed();
  }
  dot(x, y) {
    const { ctx } = this;
    ctx.fillStyle = '#fff';
    ctx.beginPath();
    ctx.arc(x, y, BRUSH / 2, 0, Math.PI * 2);
    ctx.fill();
  }
  line(x0, y0, x1, y1) {
    const { ctx } = this;
    ctx.strokeStyle = '#fff';
    ctx.lineWidth = BRUSH;
    ctx.lineCap = 'round';
    ctx.lineJoin = 'round';
    ctx.beginPath();
    ctx.moveTo(x0, y0);
    ctx.lineTo(x1, y1);
    ctx.stroke();
  }
  // Stamp a 16x16 float tensor onto the pad, upscaled to fill the canvas.
  stamp(tensor16) {
    const { ctx } = this;
    // build a 16x16 ImageData then nearest-neighbor upscale to DISP
    const tmp = document.createElement('canvas');
    tmp.width = IMG; tmp.height = IMG;
    const tctx = tmp.getContext('2d');
    const img = tctx.createImageData(IMG, IMG);
    for (let i = 0; i < IMGLEN; i++) {
      const v = Math.max(0, Math.min(255, (tensor16[i] * 255) | 0));
      img.data[i * 4] = v; img.data[i * 4 + 1] = v; img.data[i * 4 + 2] = v; img.data[i * 4 + 3] = 255;
    }
    tctx.putImageData(img, 0, 0);
    ctx.fillStyle = '#000';
    ctx.fillRect(0, 0, DISP, DISP);
    ctx.imageSmoothingEnabled = false;
    ctx.drawImage(tmp, 0, 0, IMG, IMG, 0, 0, DISP, DISP);
    this.changed();
  }
  tensor() {
    const img = this.ctx.getImageData(0, 0, DISP, DISP).data;
    return preprocess(img, DISP);
  }
  changed() {
    const t = this.tensor();
    this.paintPreview(t);
    if (this.onChange) this.onChange(t);
  }
  paintPreview(t) {
    paintGrid(this.preview, t, IMG, IMG, seqColor, { cell: (this.preview.dataset.cell | 0) || 9 });
  }
}

// --- public init ------------------------------------------------------------
export function initLab(worker, trainState) {
  const rpc = makeRpc(worker);
  const els = {
    pad: document.getElementById('draw-pad'),
    preview: document.getElementById('model-input'),
    bars: document.getElementById('prob-bars'),
    pred: document.getElementById('infer-pred'),
    clear: document.getElementById('btn-clear'),
    surprise: document.getElementById('btn-surprise'),
    // activations + filters
    filters: document.getElementById('filter-tiles'),
    conv1acts: document.getElementById('conv1-acts'),
    conv2acts: document.getElementById('conv2-acts'),
    // confusion
    confBtn: document.getElementById('btn-confusion'),
    confCanvas: document.getElementById('confusion-canvas'),
    confStatus: document.getElementById('confusion-status'),
    // arch backend tag
    archTag: document.getElementById('arch-backend-tag'),
  };

  // --- probability bars (DOM) ---
  const bars = [];
  if (els.bars) {
    for (let k = 0; k < NUM_CLASSES; k++) {
      const row = document.createElement('div');
      row.className = 'pbar-row';
      const lab = document.createElement('span');
      lab.className = 'pbar-label';
      lab.textContent = String(k);
      const track = document.createElement('div');
      track.className = 'pbar-track';
      const fill = document.createElement('div');
      fill.className = 'pbar-fill';
      track.appendChild(fill);
      const val = document.createElement('span');
      val.className = 'pbar-val';
      val.textContent = '—';
      row.appendChild(lab); row.appendChild(track); row.appendChild(val);
      els.bars.appendChild(row);
      bars.push({ fill, val, row });
    }
  }

  function renderBars(probs) {
    if (!probs) return;
    let best = 0;
    for (let k = 1; k < NUM_CLASSES; k++) if (probs[k] > probs[best]) best = k;
    for (let k = 0; k < NUM_CLASSES; k++) {
      const p = probs[k];
      bars[k].fill.style.width = (p * 100).toFixed(1) + '%';
      bars[k].val.textContent = (p * 100).toFixed(1) + '%';
      bars[k].row.classList.toggle('best', k === best);
    }
    if (els.pred) els.pred.textContent = 'prediction: ' + best + ' (' + (probs[best] * 100).toFixed(1) + '%)';
  }

  // --- filter tiles (8 conv1 3x3 kernels) ---
  const filterCanvases = [];
  if (els.filters) {
    for (let f = 0; f < 8; f++) {
      const wrap = document.createElement('div');
      wrap.className = 'tile-wrap';
      const c = document.createElement('canvas');
      // Size now (not only at draw time) so the section is compact from load.
      sizeCanvas(c, FILTER_CSS, FILTER_CSS);
      wrap.appendChild(c);
      const cap = document.createElement('span');
      cap.className = 'tile-cap';
      cap.textContent = 'f' + f;
      wrap.appendChild(cap);
      els.filters.appendChild(wrap);
      filterCanvases.push(c);
    }
  }
  function renderFilters(conv1W) {
    if (!conv1W) return;
    // conv1.W layout [F, C=1, 3, 3]; filter f occupies [f*9 .. f*9+9)
    for (let f = 0; f < 8; f++) {
      const data = conv1W.slice(f * 9, f * 9 + 9);
      paintGrid(filterCanvases[f], data, 3, 3, divColor, { cell: FILTER_CELL, symmetric: true });
    }
  }

  // --- activation maps ---
  const conv1Canvases = [], conv2Canvases = [];
  if (els.conv1acts) {
    for (let i = 0; i < 8; i++) {
      const c = document.createElement('canvas');
      sizeCanvas(c, CONV1_ACT_CSS, CONV1_ACT_CSS);
      els.conv1acts.appendChild(c);
      conv1Canvases.push(c);
    }
  }
  if (els.conv2acts) {
    for (let i = 0; i < 16; i++) {
      const c = document.createElement('canvas');
      sizeCanvas(c, CONV2_ACT_CSS, CONV2_ACT_CSS);
      els.conv2acts.appendChild(c);
      conv2Canvases.push(c);
    }
  }
  function renderActs(conv1, conv2) {
    if (conv1) {
      for (let f = 0; f < 8; f++) {
        paintGrid(conv1Canvases[f], conv1.slice(f * 256, f * 256 + 256), 16, 16, seqColor, { cell: ACT_CELL });
      }
    }
    if (conv2) {
      for (let f = 0; f < 16; f++) {
        paintGrid(conv2Canvases[f], conv2.slice(f * 64, f * 64 + 64), 8, 8, seqColor, { cell: ACT_CELL });
      }
    }
  }

  // --- throttled live inference (~10 Hz, ordered + trailing-edge) -------------
  // Every live request is tagged with a monotonically increasing id. A response
  // is applied to the UI ONLY if its id >= the latest applied id, so an older,
  // out-of-order (or pre-clear) response can never overwrite a newer visual
  // state. Trailing edge: if the pad changes while an inference is in flight,
  // `dirty` stays set and the FINAL pad state is inferred once the in-flight one
  // completes, so the last change always ends up displayed.
  let dirty = false;
  let lastRun = 0;
  let inFlight = false;
  let lastTensor = new Float32Array(IMGLEN);
  let inferSeq = 0;      // monotonic id for live-inference requests
  let lastApplied = 0;   // highest id applied to the UI
  function scheduleInfer() { dirty = true; }

  function isEmpty(t) { for (let i = 0; i < t.length; i++) if (t[i] !== 0) return false; return true; }

  // Reset the probability UI to a neutral state (empty pad = no prediction).
  function renderNeutral() {
    for (let k = 0; k < NUM_CLASSES; k++) {
      bars[k].fill.style.width = '0%';
      bars[k].val.textContent = '—';
      bars[k].row.classList.remove('best');
    }
    if (els.pred) els.pred.textContent = 'prediction: —';
  }

  // Atomic UI update: bars, numeric percentages, activation maps and the
  // prediction line are ALL derived from the SAME response here — never from
  // different inference results.
  function applyInference(r) {
    if (!r || r.error || !r.probs) { renderNeutral(); return; }
    renderBars(r.probs);
    renderActs(r.conv1, r.conv2);
    renderFilters(r.filters ? r.filters.conv1 : null);
  }

  setInterval(() => {
    const now = performance.now();
    if (!dirty || inFlight || now - lastRun < 95) return;
    dirty = false;
    inFlight = true;
    lastRun = now;
    const id = ++inferSeq;      // tag this request
    const t = lastTensor;
    const empty = isEmpty(t);
    rpc({ type: 'infer', pixels: t }).then((r) => {
      inFlight = false;
      // Drop stale / out-of-order responses (e.g. one sent before a clear).
      if (id < lastApplied) return;
      lastApplied = id;
      if (empty) renderNeutral(); else applyInference(r);
    }).catch(() => { inFlight = false; });
  }, 100);

  // --- draw pad ---
  const pad = new DrawPad(els.pad, els.preview, (t) => {
    lastTensor = t;
    if (isEmpty(t)) {
      // clear/empty: show neutral immediately and invalidate any in-flight
      // result so it cannot overwrite the neutral state when it lands.
      lastApplied = ++inferSeq;
      renderNeutral();
    }
    scheduleInfer();
  });

  // --- buttons ---
  if (els.clear) els.clear.addEventListener('click', () => pad.clear());
  async function surprise() {
    const cls = Math.floor(Math.random() * NUM_CLASSES);
    await stampClass(cls);
  }
  async function stampClass(cls) {
    const r = await rpc({ type: 'stamp', classIndex: cls });
    if (r && r.pixels) pad.stamp(r.pixels);
  }
  if (els.surprise) els.surprise.addEventListener('click', surprise);

  // --- confusion matrix ---
  function renderConfusion(matrix) {
    if (!els.confCanvas || !matrix) return;
    const N = NUM_CLASSES;
    const cell = 34, padL = 34, padT = 22;
    const dpr = window.devicePixelRatio || 1;
    const w = padL + cell * N + 6;
    const h = padT + cell * N + 6;
    els.confCanvas.width = w * dpr; els.confCanvas.height = h * dpr;
    els.confCanvas.style.width = w + 'px'; els.confCanvas.style.height = h + 'px';
    const ctx = els.confCanvas.getContext('2d');
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.fillStyle = '#0e1521'; ctx.fillRect(0, 0, w, h);
    // row totals -> max for color scale
    const rowTot = matrix.map((row) => row.reduce((a, b) => a + b, 0) || 1);
    const maxAcc = Math.max(...matrix.map((row, i) => row[i] / rowTot[i]));
    ctx.font = '11px system-ui, sans-serif';
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
    for (let i = 0; i < N; i++) {
      ctx.fillStyle = '#8b95a7';
      ctx.fillText(String(i), padL + i * cell + cell / 2, padT - 10);     // x axis (predicted)
      ctx.fillText(String(i), padL - 12, padT + i * cell + cell / 2);     // y axis (true)
      const acc = matrix[i][i] / rowTot[i];
      for (let j = 0; j < N; j++) {
        const frac = matrix[i][j] / rowTot[i];
        const t = frac / (maxAcc || 1);
        // diagonal green-ish, off-diagonal accent
        const r = j === i ? Math.round(20 + 90 * t) : Math.round(20 + 70 * t);
        const g = j === i ? Math.round(40 + 200 * t) : Math.round(40 + 150 * t);
        const b = j === i ? Math.round(40 + 80 * t) : Math.round(60 + 160 * t);
        ctx.fillStyle = `rgb(${r},${g},${b})`;
        ctx.fillRect(padL + j * cell + 1, padT + i * cell + 1, cell - 2, cell - 2);
        ctx.fillStyle = frac > 0.45 ? '#06222c' : '#e6ecf5';
        if (matrix[i][j] > 0) ctx.fillText(String(matrix[i][j]), padL + j * cell + cell / 2, padT + i * cell + cell / 2);
      }
      // per-class accuracy at row end
      ctx.fillStyle = acc >= 0.9 ? '#7cffb2' : acc >= 0.5 ? '#ffb454' : '#ff6b6b';
      ctx.textAlign = 'left';
      ctx.fillText((acc * 100).toFixed(0) + '%', padL + N * cell + 8, padT + i * cell + cell / 2);
      ctx.textAlign = 'center';
    }
    // hover: update canvas title with the hovered cell
    els.confCanvas.onmousemove = (e) => {
      const rect = els.confCanvas.getBoundingClientRect();
      const x = (e.clientX - rect.left) - padL;
      const y = (e.clientY - rect.top) - padT;
      const j = Math.floor(x / cell), i = Math.floor(y / cell);
      if (i >= 0 && i < N && j >= 0 && j < N) {
        const acc = matrix[i][i] / rowTot[i];
        els.confCanvas.title = `true ${i} -> pred ${j}: ${matrix[i][j]}  (class ${i} acc ${(acc * 100).toFixed(1)}%)`;
      } else {
        els.confCanvas.title = '';
      }
    };
    els.confCanvas.title = 'rows = true class, columns = prediction';
  }
  if (els.confBtn) els.confBtn.addEventListener('click', async () => {
    els.confBtn.disabled = true;
    if (els.confStatus) els.confStatus.textContent = 'Evaluating test set…';
    const r = await rpc({ type: 'confusion' });
    els.confBtn.disabled = false;
    if (r && r.matrix) { renderConfusion(r.matrix); if (els.confStatus) els.confStatus.textContent = 'Done — rows: true class, cols: prediction, right margin: per-class accuracy.'; }
    else if (els.confStatus) els.confStatus.textContent = r && r.error ? 'Error: ' + r.error : 'Not ready yet.';
  });

  // --- architecture backend highlight (polls trainState) ---
  function updateArch() {
    const gpu = trainState.backend === 'webgpu';
    document.documentElement.style.setProperty('--arch-tint', gpu ? 'rgba(124,255,178,0.5)' : 'rgba(90,209,255,0.5)');
    document.body.classList.toggle('arch-gpu', gpu);
    if (els.archTag) els.archTag.textContent = gpu ? 'executing on WebGPU' : 'executing on CPU';
  }
  setInterval(updateArch, 600);
  updateArch();

  // --- keep filter tiles fresh during training (even when not drawing) ------
  setInterval(() => {
    if (trainState.running) {
      // a lightweight infer on the current pad content refreshes filters too
      scheduleInfer();
    }
  }, 3000);

  // --- verification hooks ---
  window.__infer = function (pixels) {
    let t;
    if (pixels && pixels.length === IMGLEN) t = Float32Array.from(pixels);
    else { t = Float32Array.from(pixels || []); if (t.length !== IMGLEN) { const p = new Float32Array(IMGLEN); p.set(t.subarray(0, Math.min(t.length, IMGLEN))); t = p; } }
    return rpc({ type: 'infer', pixels: t }).then((r) => (r && r.probs) ? r.probs.slice() : new Array(NUM_CLASSES).fill(1 / NUM_CLASSES));
  };
  window.__drawPad = {
    clear: () => pad.clear(),
    stamp: async (classIndex) => {
      await stampClass((classIndex | 0) % NUM_CLASSES);
      // pad.stamp already fired changed() -> inference path
      // wait one inference cycle so callers see updated probabilities
      return window.__infer(lastTensor);
    },
  };
  window.__confusion = function () {
    return rpc({ type: 'confusion' }).then((r) => (r && r.matrix) ? r.matrix : null);
  };

  return { pad, rpc, renderBars, renderFilters, renderActs };
}
