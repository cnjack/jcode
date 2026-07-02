# NeuraLab — Train a CNN in your browser

An interactive ML lab that **procedurally generates a digit dataset**, trains a
small convolutional neural network **live in the browser**, and plots
loss/accuracy in real time. Training runs on either a **hand-written CPU
backend** (pure JavaScript on `Float32Array`, no ML libraries) or an optional
**WebGPU compute backend** that implements the identical math on the GPU, with
automatic, silent fallback to CPU when WebGPU is unavailable.

## Run it

Purely static — no build step:

```bash
python3 -m http.server
# open http://localhost:8000/
```

Open `index.html` through the server (ES modules require `http://`, not `file://`).

## What it does

1. **Dataset** — Renders digits 0–9 as 16×16 grayscale images on an
   `OffscreenCanvas` (inside a Web Worker) with per-sample augmentation:
   4+ font stacks, ±15° rotation, scale/shift jitter, bold/normal stroke,
   additive Gaussian noise (σ≈0.05), and optional blur. 3000 train / 600 test,
   balanced. All randomness flows from a single seeded PRNG (mulberry32),
   default seed 42 — fully reproducible.
2. **CNN** — `1×16×16 → conv3×3(8) → ReLU → maxpool2 → conv3×3(16) → ReLU →
   maxpool2 → flatten(256) → dense(10) → softmax+CE`. He init, SGD+momentum
   (lr 0.05, momentum 0.9, batch 32), shuffled each epoch with the seeded PRNG.
   Every conv/pool/dense forward **and** backward pass is written by hand.
3. **Gradient check** — Central-difference (eps 1e-3) vs analytic gradients for
   ~20 parameters spread across conv1/conv2/dense weights & biases on a fixed
   batch of 8. Reports max/mean relative error with a PASS (<1e-2) / FAIL verdict.
4. **Live charts** — Two hand-drawn retina (`devicePixelRatio`) canvas line
   charts: smoothed training loss per batch, and train vs test accuracy.
5. **Inference playground** — A 256×256 drawing canvas (mouse + touch, soft
   white-on-black strokes) feeds a **live, MNIST-style preprocessor**: the ink
   is cropped to its bounding box, scaled so the longest side is ~11.5 grid
   cells, then shifted so the ink **centroid** lands at the grid center — the
   same centering the dataset generator applies, which is what lets an
   off-center scribble still classify well. A crisp 16×16 "model input" preview
   shows exactly what the network sees, and a 10-class probability bar chart
   updates at ~10 Hz as you draw. **Surprise me** stamps a random *test-set*
   sample onto the pad.
6. **Visualizations** — The 8 conv1 3×3 first-layer filters as magnified
   diverging (blue↔orange) heatmap tiles, refreshed every few seconds during
   training; conv1 (8×16×16) and conv2 (16×8×8) post-ReLU activation maps for
   the *current* drawing; and a **confusion matrix** button that evaluates the
   whole test set (chunked, in the worker) into a 10×10 heatmap with hover cell
   counts and per-class accuracy.

## Architecture

```
index.html
css/style.css
js/
  rng.js          seeded PRNG (mulberry32) + helpers
  dataset.js      procedural digit generation (OffscreenCanvas, seeded)
  charts.js       retina canvas line charts (no libraries)
  main.js         UI wiring, app state, exposes window.__app/__trainState/__gradCheck
  lab.js          inference playground + visualizations (draw pad, preprocessing,
                  prob bars, conv filters/activations, confusion matrix, arch
                  diagram, explainers) — exposes window.__infer/__drawPad/__confusion
  worker.js       Web Worker: dataset generation + training loop (time-sliced) +
                  inference/stamp/confusion handlers (CPU mirror model)
  nn/
    cpu.js        CPU backend: Conv2D / ReLU / MaxPool2D / Flatten / Dense / SoftmaxCE
    gpu.js        WebGPU backend: same layer factory surface, async forward/backward, GPU-resident weights
    wgsl/
      kernels.js  WGSL compute kernels (JS template-literal generators, specialized per layer)
    model.js      backend-agnostic model definition, optimizer, metrics
    gradcheck.js  numerical gradient check
README.md
```

### Backend abstraction

Each layer exposes `forward(x)`, `backward(dy)`, and `getParams()`. The model is
built via a `backend` factory (`cpuBackend` in `nn/cpu.js`, `makeGpuBackend` in
`nn/gpu.js`), so `buildModel()` is identical for both backends. CPU layers are
synchronous; GPU layers are **async** (`forward`/`backward` return
`Promise<tensor>`). The worker drives either via `modelForwardAsync` /
`modelBackwardAsync`, where `await` transparently handles the synchronous CPU
layers too — so the CPU fast path is byte-for-byte the proven round-2 loop.

### WebGPU backend design

- **Detection & fallback.** On startup the worker calls `navigator.gpu
  .requestAdapter()` → `requestDevice()`. If any step fails (no `navigator.gpu`,
  null adapter, device rejection, or `device.lost` later) it logs a single
  `console.info` and continues on CPU. `window.__trainState.backend` reports
  `"webgpu"` or `"cpu"` truthfully, and `window.__trainState.backendReason`
  explains *why* CPU was chosen (e.g. `"navigator.gpu undefined"`).
- **UI honesty.** A header badge reads **“WebGPU active”** (green) when the GPU
  is running, **“CPU (selected)”** (neutral) when the user picked the CPU backend
  while WebGPU is available, or **“CPU fallback — WebGPU unavailable”** (neutral)
  only when WebGPU is genuinely absent. A segmented control (Auto / CPU / WebGPU)
  sets a preference applied on **Reset**; the WebGPU option is disabled with a
  tooltip when no adapter is present.
- **GPU-resident parameters.** Weights, velocities and gradient slots live in
  persistent GPU storage buffers, uploaded once at construction. The
  `GpuMomentumSGD` optimizer (global-norm gradient clipping + PyTorch-style
  momentum) updates them **on the GPU** via a `reduce_sum_squares` + per-tensor
  `sgd_update` pair of kernels, so weights never round-trip to the CPU during
  training. Only the scalar loss (every batch) and accuracy (eval) are read back.
- **Kernels** (`nn/wgsl/kernels.js`): conv2d forward / weight-grad / bias-grad /
  input-grad, maxpool forward (records argmax) / backward (scatter via argmax),
  dense forward + dW/db/dx, ReLU forward/backward, softmax+CE forward (per-row,
  loss summed on the CPU) / backward, plus the optimizer kernels. Each kernel is
  a JS template-literal generator that **bakes the layer’s static dimensions** as
  module-scope `const` declarations (not `let` — WGSL removed module-scope `let`,
  and current Chrome rejects shaders that still use it) and shares a single
  `{N}` uniform across a batch — eliminating uniform-buffer reuse hazards and
  keeping buffer layouts identical to the CPU NCHW convention.

### Backend parity testing

A **Backend parity test** button runs one forward+backward on a fixed seeded
batch of 16 on **both** backends and reports the per-tensor max abs diff
(logits, loss, conv1/conv2/dense weight grads) with PASS < 1e-3. When WebGPU is
absent it does **not** error — it shows a styled inline notice. The programmatic
hook `window.__parityTest()` resolves to `{available, pass?, maxDiff?, detail?}`
(`available:false` when there is no GPU).

**Verified on real WebGPU** (headless Chrome, Apple Metal adapter, driven via
CDP): `window.__parityTest()` → `{available:true, pass:true, maxDiff≈5e-7}` —
every reported tensor (logits, loss, conv1.dW, conv2.dW, dense.dW) agrees to
float32 rounding noise. GPU training reaches >90% test accuracy in a few seconds
(~5.5k train samples/sec vs ~2k on CPU), with a truthful, monotonically
decreasing loss; the CPU path and the numerical gradient check (max rel err
≈1e-5) are unchanged.

### Why a worker?

Generation and training run inside a Web Worker so the page never freezes. The
training loop is cooperative: it runs batches within an ~8 ms time budget then
yields (`setTimeout`) so it can react to pause/reset. A `runId` is attached to
every progress message so stale updates never reach the UI after a reset.

### Inference & preprocessing

Live inference uses a **second, CPU-only mirror model** kept inside the worker
(`inferModel`). It never touches the training model's forward/backward state, so
drawing classifies correctly whether training is running, paused, or finished.
On every inference the mirror's weights are synced from the active training
model (a few array copies on CPU; a small GPU readback when WebGPU is active).
Activations are captured mid-forward at conv1/conv2 (post-ReLU) for the
visualizations; the conv1 kernels feed the filter tiles.

**Preprocessing math** (`preprocess` in `lab.js`): given the 256×256 ink, find
the ink bounding box and the intensity-weighted centroid `(cx, cy)`. Let `side`
be the longest bbox dimension and `S = 11.5 / side` (grid units per source
pixel). Each output cell center `(gx+0.5, gy+0.5)` is inverse-mapped to source
coordinates `cx + (gx − 7.5)/S`, `cy + (gy − 7.5)/S` and bilinearly sampled.
This scales the glyph to ~11.5/16 of the grid (matching the dataset's
font-size-11–16 centered glyphs) and pins the centroid to the grid center — the
same invariance MNIST's centering gives, so an off-center scribble still
classifies well.

## Verification hooks (for automation)

- `window.__trainState` — `{ running, epoch, batch, loss, trainAcc, testAcc,
  samplesPerSec, datasetReady, backend, backendReason, webgpuAvailable }`, kept
  live. `backend` is `"cpu"` or `"webgpu"` truthfully; `backendReason` explains
  why CPU was chosen (empty string when WebGPU is active). The samples/sec meter
  is **train-only** — evaluation time is paused out of its window.
- `window.__app` — `start()`, `pause()`, `reset()`, `regenerate(seed)`.
- `window.__gradCheck()` — async, resolves `{ maxRelErr, meanRelErr, pass }`.
- `window.__parityTest()` — async, resolves `{ available, pass?, maxDiff?,
  detail? }`; `available:false` (never throws) when WebGPU is absent.
- `window.__infer(pixels)` — async, resolves to the 10 probabilities (Array of
  0..1) for a 16×16 input. Accepts a `Float32Array`/`Array` of length 256 in
  0..1 (any array-like is coerced and length-padded/truncated).
- `window.__drawPad = { clear(), stamp(classIndex) }` — `stamp` draws a random
  test-set sample of the given class onto the pad, runs the normal inference
  path, and resolves to the resulting 10 probabilities.
- `window.__confusion()` — async, resolves to the 10×10 test-set confusion
  matrix (Array of 10 rows of 10 counts; rows = true class, cols = prediction),
  evaluated in the worker on the CPU mirror model, chunked so the UI stays live.

## Controls

- **Start / Pause / Reset** — drive training.
- **seed + Regenerate** — rebuild the dataset with a new seed.
- **Hyperparameters** (learning rate, momentum, batch size 8/16/32/64) —
  editable; applied on **Reset**. Learning rate decays ×0.5 every 1200 batches
  (floor ×0.1), shown live in the stats.
- **Run gradient check** — validate the backward pass.
- **Try it** — draw a digit (or **Surprise me**), watch the bars, filters and
  activations update live.
- **Compute confusion matrix** — evaluate the test set.

Built autonomously by jcode.
