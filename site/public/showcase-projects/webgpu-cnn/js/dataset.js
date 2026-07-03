// js/dataset.js
// Procedural 16x16 grayscale digit dataset. Digits are rendered to a canvas
// (OffscreenCanvas inside the worker, with a main-thread canvas fallback),
// then read back as pixels and augmented. All randomness is seeded.

import { RNG } from './rng.js';

export const IMG = 16; // image dimension
export const IMGLEN = IMG * IMG; // 256 floats per image
export const NUM_CLASSES = 10;

// At least 4 distinct font stacks are cycled through for variety.
const FONT_STACKS = [
  'Arial, Helvetica, sans-serif',
  'Georgia, "Times New Roman", serif',
  '"Courier New", Courier, monospace',
  'Verdana, Geneva, sans-serif',
  'Helvetica, Arial, sans-serif',
];

// Canvas factory that works in a worker (OffscreenCanvas) and degrades to a
// hidden document canvas on the main thread when OffscreenCanvas is absent.
function defaultMakeCanvas() {
  if (typeof OffscreenCanvas !== 'undefined') {
    const c = new OffscreenCanvas(IMG, IMG);
    const ctx = c.getContext('2d', { willReadFrequently: true });
    return { ctx, read: () => ctx.getImageData(0, 0, IMG, IMG).data };
  }
  // Main-thread fallback (only used if OffscreenCanvas is unavailable).
  const c = document.createElement('canvas');
  c.width = IMG;
  c.height = IMG;
  const ctx = c.getContext('2d', { willReadFrequently: true });
  return { ctx, read: () => ctx.getImageData(0, 0, IMG, IMG).data };
}

// Render a single digit onto the provided 2d context with random augmentation.
function renderOne(ctx, rng, digit) {
  const font = rng.pick(FONT_STACKS);
  const weight = rng.bool(0.5) ? 'bold' : 'normal';
  const size = rng.range(11, 16); // scale jitter (±~20% around a base of 13)
  const rot = rng.range(-15, 15) * (Math.PI / 180);
  const dx = rng.range(-2, 2);
  const dy = rng.range(-2, 2);

  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.fillStyle = '#000';
  ctx.fillRect(0, 0, IMG, IMG);
  ctx.fillStyle = '#fff';
  ctx.font = `${weight} ${size.toFixed(2)}px ${font}`;
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  ctx.translate(IMG / 2 + dx, IMG / 2 + dy);
  ctx.rotate(rot);
  ctx.fillText(String(digit), 0, 0);
}

// Read raw pixels -> normalized grayscale float32 in [0, 1].
function pixelsToFloat(data, rng) {
  const out = new Float32Array(IMGLEN);
  for (let i = 0; i < IMGLEN; i++) out[i] = data[i * 4] / 255; // R channel (grayscale source)
  // Slight blur variation: 3x3 box blur ~half the time.
  let buf = out;
  if (rng.bool(0.5)) buf = blur3x3(buf);
  // Additive Gaussian pixel noise.
  for (let i = 0; i < IMGLEN; i++) buf[i] += rng.gauss(0, 0.05);
  // Clamp to [0, 1].
  for (let i = 0; i < IMGLEN; i++) {
    const v = buf[i];
    buf[i] = v < 0 ? 0 : v > 1 ? 1 : v;
  }
  return buf;
}

function blur3x3(src) {
  const dst = new Float32Array(IMGLEN);
  for (let y = 0; y < IMG; y++) {
    for (let x = 0; x < IMG; x++) {
      let s = 0;
      let cnt = 0;
      for (let dy = -1; dy <= 1; dy++) {
        for (let dx = -1; dx <= 1; dx++) {
          const ny = y + dy;
          const nx = x + dx;
          if (ny < 0 || ny >= IMG || nx < 0 || nx >= IMG) continue;
          s += src[ny * IMG + nx];
          cnt++;
        }
      }
      dst[y * IMG + x] = s / cnt;
    }
  }
  return dst;
}

// Deterministic in-place shuffle that permutes rows of `x` in lockstep with `y`.
function shuffleRows(x, y, rng) {
  const n = y.length;
  const order = new Array(n);
  for (let i = 0; i < n; i++) order[i] = i;
  rng.shuffle(order);
  const x2 = new Float32Array(x.length);
  const y2 = new Int32Array(n);
  for (let i = 0; i < n; i++) {
    const src = order[i];
    y2[i] = y[src];
    x2.set(x.subarray(src * IMGLEN, (src + 1) * IMGLEN), i * IMGLEN);
  }
  x.set(x2);
  y.set(y2);
}

// Create a balanced dataset. Async so it can yield to keep the worker
// (and thus the UI) responsive while reporting progress.
export async function createDataset(opts = {}) {
  const {
    seed = 42,
    trainSize = 3000,
    testSize = 600,
    numClasses = NUM_CLASSES,
    makeCanvas = defaultMakeCanvas,
    onProgress = () => {},
    yieldFn = () => new Promise((r) => setTimeout(r, 0)),
  } = opts;

  const rng = new RNG(seed);
  const { ctx, read } = makeCanvas();

  const trainX = new Float32Array(trainSize * IMGLEN);
  const trainY = new Int32Array(trainSize);
  const testX = new Float32Array(testSize * IMGLEN);
  const testY = new Int32Array(testSize);

  const sets = [
    { x: trainX, y: trainY, count: trainSize },
    { x: testX, y: testY, count: testSize },
  ];
  const total = trainSize + testSize;
  let done = 0;

  for (const set of sets) {
    for (let i = 0; i < set.count; i++) {
      const label = i % numClasses; // balanced across classes
      renderOne(ctx, rng, label);
      const f = pixelsToFloat(read(), rng);
      set.x.set(f, i * IMGLEN);
      set.y[i] = label;
      done++;
      if ((done & 31) === 0) {
        onProgress(done / total);
        await yieldFn();
      }
    }
  }
  onProgress(1);

  // Shuffle deterministically (separate sub-stream) so minibatches are diverse.
  shuffleRows(trainX, trainY, new RNG((seed ^ 0xabcdef) >>> 0));
  shuffleRows(testX, testY, new RNG((seed ^ 0x123456) >>> 0));

  return { trainX, trainY, testX, testY, numClasses, imgSize: IMG, imgLen: IMGLEN };
}

// Build a compact set of display samples: `per` images per class.
export function displaySamples(dataset, per = 8) {
  const { trainX, trainY, numClasses } = dataset;
  const seen = new Int32Array(numClasses);
  const out = [];
  for (let i = 0; i < trainY.length && out.length < numClasses * per; i++) {
    const label = trainY[i];
    if (seen[label] < per) {
      out.push({
        label,
        data: trainX.slice(i * IMGLEN, (i + 1) * IMGLEN),
      });
      seen[label]++;
    }
  }
  return out;
}
