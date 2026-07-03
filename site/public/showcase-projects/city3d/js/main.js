// js/main.js
// Bootstrap: renderer, camera, controls, day/night system, city + traffic, the
// control panel wiring, the render loop + FPS meter, and the window.__city
// verification API (including a synchronous benchmark()).

import * as THREE from 'three';
import { OrbitControls } from '../vendor/OrbitControls.js';
import { buildCity } from './city.js';
import { createTraffic } from './traffic.js';
import { DayNightSystem } from './daynight.js';
import { Flythrough } from './flythrough.js';
import { makeLabelTexture } from './textures.js';
import { randomSeed } from './rng.js';

// ---------------------------------------------------------------------------
// Renderer
// ---------------------------------------------------------------------------
const container = document.getElementById('app');

function showWebGLFailed() {
  const ov = document.getElementById('loading');
  if (ov) {
    ov.classList.add('error');
    const st = ov.querySelector('.loading-status');
    if (st) st.textContent = 'WebGL is unavailable';
    const sub = ov.querySelector('.loading-sub');
    if (sub) sub.textContent = 'This browser/device could not create a WebGL context. Try a recent desktop browser with hardware acceleration enabled.';
    ov.style.opacity = '1';
    ov.style.pointerEvents = 'auto';
  }
}

let renderer;
try {
  renderer = new THREE.WebGLRenderer({ antialias: true, powerPreference: 'high-performance' });
} catch (err) {
  console.error('WebGLRenderer creation failed:', err);
  showWebGLFailed();
  throw err;
}
renderer.setSize(window.innerWidth, window.innerHeight);
renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
renderer.outputColorSpace = THREE.SRGBColorSpace;
renderer.toneMapping = THREE.ACESFilmicToneMapping;
renderer.toneMappingExposure = 1.0;
renderer.shadowMap.enabled = true;
renderer.shadowMap.type = THREE.PCFSoftShadowMap;
container.appendChild(renderer.domElement);

// Loading overlay: fade out once the first frame has rendered.
function hideLoading() {
  const ov = document.getElementById('loading');
  if (ov && !ov.classList.contains('error')) {
    ov.classList.add('hidden');
  }
}

// ---------------------------------------------------------------------------
// Scene + camera
// ---------------------------------------------------------------------------
const scene = new THREE.Scene();
const camera = new THREE.PerspectiveCamera(
  52,
  window.innerWidth / window.innerHeight,
  1,
  4000
);
camera.position.set(216, 195, 181);
camera.lookAt(0, 45, 0);

const controls = new OrbitControls(camera, renderer.domElement);
controls.enableDamping = true;
controls.dampingFactor = 0.06;
controls.minDistance = 50;
controls.maxDistance = 460;
controls.maxPolarAngle = Math.PI * 0.485;
controls.target.set(0, 45, 0);
controls.autoRotate = true;
controls.autoRotateSpeed = 0.35;

let userInteracted = false;
const stopAuto = () => {
  if (!userInteracted) {
    userInteracted = true;
    controls.autoRotate = false;
  }
};
controls.addEventListener('start', stopAuto);
renderer.domElement.addEventListener('pointerdown', stopAuto);
renderer.domElement.addEventListener('touchstart', stopAuto, { passive: true });

// ---------------------------------------------------------------------------
// Cinematic flythrough (owns its own camera path; toggled via button / C)
// ---------------------------------------------------------------------------
const flythrough = new Flythrough(camera, controls);
function toggleFlythrough(on) {
  if (on === undefined) on = !flythrough.isActive();
  if (on) {
    stopAuto();
    flythrough.start();
  } else {
    flythrough.stop();
  }
  if (els.cineBtn) els.cineBtn.classList.toggle('active', flythrough.isActive());
  if (els.cineBtn) els.cineBtn.setAttribute('aria-pressed', String(flythrough.isActive()));
}

// ---------------------------------------------------------------------------
// Day/night system (owns lights, sky dome, sun/moon, stars, fog)
// ---------------------------------------------------------------------------
const daynight = new DayNightSystem(scene, renderer);

// ---------------------------------------------------------------------------
// City + traffic state
// ---------------------------------------------------------------------------
let cityState = null;
let trafficState = null;
let currentSeed = randomSeed();
let density = 1.0;
let trafficDensity = 0.6;
let quality = 'high';

function applyQuality(q) {
  quality = q;
  renderer.shadowMap.enabled = q === 'high';
  daynight.sun.castShadow = q === 'high';
  renderer.setPixelRatio(q === 'high' ? Math.min(window.devicePixelRatio, 2) : 1);
}

function rebuild(seed, opts = {}) {
  const useSeed = seed === undefined ? randomSeed() : seed;
  currentSeed = useSeed >>> 0;
  const useDensity = opts.density ?? density;
  const useQuality = opts.quality ?? quality;
  density = useDensity;

  if (trafficState) {
    scene.remove(trafficState.group);
    trafficState.dispose();
    trafficState = null;
  }
  if (cityState) {
    scene.remove(cityState.group);
    cityState.dispose();
    cityState = null;
  }

  cityState = buildCity(currentSeed, { density: useDensity, quality: useQuality });
  scene.add(cityState.group);
  daynight.setCity(cityState);

  trafficState = createTraffic(currentSeed, cityState.roads, trafficDensity);
  scene.add(trafficState.group);
  daynight.setTraffic(trafficState);
  // rebuild the flythrough path for the new city layout
  flythrough.build(cityState);
  // re-apply current night factor for the new traffic
  daynight.apply();

  updateHud();
  return cityState;
}

function regenerate(seed) {
  return rebuild(seed);
}

// ---------------------------------------------------------------------------
// Control panel wiring
// ---------------------------------------------------------------------------
const $ = (id) => document.getElementById(id);
const els = {
  panel: $('panel'),
  collapseBtn: $('collapseBtn'),
  body: $('panelBody'),
  timeSlider: $('timeSlider'),
  timeVal: $('timeVal'),
  autoCycle: $('autoCycle'),
  cycleSpeed: $('cycleSpeed'),
  densitySlider: $('densitySlider'),
  trafficSlider: $('trafficSlider'),
  seedInput: $('seedInput'),
  regen: $('regen'),
  seedVal: $('seedVal'),
  count: $('count'),
  carVal: $('carVal'),
  fps: $('fps'),
  qualityToggle: $('qualityToggle'),
  cineBtn: $('cineBtn'),
};

function fmtTime(h) {
  const hh = Math.floor(h);
  const mm = Math.floor((h - hh) * 60);
  return `${String(hh).padStart(2, '0')}:${String(mm).padStart(2, '0')}`;
}

function updateHud() {
  if (els.seedVal) els.seedVal.textContent = String(currentSeed);
  if (els.count) els.count.textContent = cityState ? String(cityState.buildingCount) : '—';
  if (els.carVal) els.carVal.textContent = trafficState ? String(trafficState.carCount) : '—';
  if (els.seedInput && document.activeElement !== els.seedInput) {
    els.seedInput.value = String(currentSeed);
  }
}

function syncTimeUI() {
  const h = daynight.getTimeOfDay();
  if (els.timeSlider) els.timeSlider.value = String(h);
  if (els.timeVal) els.timeVal.textContent = fmtTime(h);
}

// Collapse toggle (keyboard accessible)
if (els.collapseBtn) {
  els.collapseBtn.addEventListener('click', () => {
    const collapsed = els.panel.classList.toggle('collapsed');
    els.collapseBtn.setAttribute('aria-expanded', String(!collapsed));
  });
}

// Time slider: manual takes over (disables auto-cycle)
if (els.timeSlider) {
  els.timeSlider.addEventListener('input', () => {
    const h = parseFloat(els.timeSlider.value);
    daynight.setTimeOfDay(h);
    if (els.timeVal) els.timeVal.textContent = fmtTime(h);
    // manual control takes over -> stop auto-cycle
    daynight.setAutoCycle(false);
    if (els.autoCycle) els.autoCycle.checked = false;
  });
}
if (els.autoCycle) {
  els.autoCycle.addEventListener('change', () => {
    daynight.setAutoCycle(els.autoCycle.checked);
  });
}
if (els.cycleSpeed) {
  els.cycleSpeed.addEventListener('input', () => {
    daynight.setCycleSpeed(parseFloat(els.cycleSpeed.value));
  });
}

// Building density (apply on release -> full rebuild)
if (els.densitySlider) {
  els.densitySlider.addEventListener('change', () => {
    density = parseFloat(els.densitySlider.value);
    rebuild(currentSeed);
  });
}
// Traffic density (live, no full rebuild)
if (els.trafficSlider) {
  const applyTraffic = () => {
    trafficDensity = parseFloat(els.trafficSlider.value);
    if (trafficState) trafficState.setDensity(trafficDensity);
    if (els.carVal) els.carVal.textContent = String(trafficState ? trafficState.carCount : 0);
  };
  els.trafficSlider.addEventListener('input', applyTraffic);
}

// Seed input + regenerate
if (els.regen) {
  els.regen.addEventListener('click', () => {
    const raw = els.seedInput ? els.seedInput.value.trim() : '';
    const parsed = raw === '' ? NaN : Number(raw);
    rebuild(Number.isFinite(parsed) ? (parsed >>> 0) : undefined);
  });
}

// Quality preset
if (els.qualityToggle) {
  els.qualityToggle.addEventListener('change', () => {
    setQuality(els.qualityToggle.value === 'low' ? 'low' : 'high');
  });
}

function setQuality(q) {
  applyQuality(q);
  if (els.qualityToggle) els.qualityToggle.value = q;
  rebuild(currentSeed);
}

// Cinematic toggle button + keyboard shortcuts (C toggle, ESC exit)
if (els.cineBtn) {
  els.cineBtn.addEventListener('click', () => toggleFlythrough());
}
window.addEventListener('keydown', (e) => {
  // ignore when typing in an input
  const tag = (e.target && e.target.tagName) || '';
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
  if (e.key === 'c' || e.key === 'C') {
    toggleFlythrough();
  } else if (e.key === 'Escape' && flythrough.isActive()) {
    flythrough.stop();
    if (els.cineBtn) {
      els.cineBtn.classList.remove('active');
      els.cineBtn.setAttribute('aria-pressed', 'false');
    }
  } else if (e.key === '?' || (e.key === '/' && e.shiftKey)) {
    const h = document.getElementById('helpPanel');
    if (h) h.classList.toggle('open');
  }
});

// ---------------------------------------------------------------------------
// window.__city API (for automated verification)
// ---------------------------------------------------------------------------
window.__city = {
  get seed() {
    return currentSeed;
  },
  get buildingCount() {
    return cityState ? cityState.buildingCount : 0;
  },
  get carCount() {
    return trafficState ? trafficState.carCount : 0;
  },
  regenerate,
  getTimeOfDay() {
    return daynight.getTimeOfDay();
  },
  setTimeOfDay(hours) {
    daynight.setTimeOfDay(hours);
    daynight.setAutoCycle(false);
    if (els.autoCycle) els.autoCycle.checked = false;
    syncTimeUI();
  },
  setAutoCycle(on) {
    daynight.setAutoCycle(!!on);
    if (els.autoCycle) els.autoCycle.checked = !!on;
  },
  setQuality(q) {
    setQuality(q === 'low' ? 'low' : 'high');
  },
  // --- cinematic flythrough (works in a hidden tab, no rAF) ---
  startFlythrough() {
    toggleFlythrough(true);
    return flythrough.isActive();
  },
  stopFlythrough() {
    if (flythrough.isActive()) {
      flythrough.stop();
      if (els.cineBtn) {
        els.cineBtn.classList.remove('active');
        els.cineBtn.setAttribute('aria-pressed', 'false');
      }
    }
  },
  isFlythrough() {
    return flythrough.isActive();
  },
  // Place the camera exactly at fraction t (0..1) along the flythrough path and
  // render a single frame synchronously. Works whether or not flythrough is
  // active; designed for hidden-tab verification screenshots.
  seekFlythrough(t) {
    if (!flythrough.curve) return;
    daynight.apply();
    if (trafficState) trafficState.update(0, camera);
    flythrough.seek(t, () => renderer.render(scene, camera));
  },
  // Synchronously render N frames in a tight loop (direct renderer.render, no
  // rAF) so it works in a hidden/background tab. Returns {avgMs, fps}.
  benchmark(frames = 60) {
    const n = Math.max(1, Math.floor(frames));
    // make sure the scene is fully up to date before measuring
    controls.update();
    daynight.apply();
    const gl = renderer.getContext();
    const start = performance.now();
    for (let i = 0; i < n; i++) {
      renderer.render(scene, camera);
    }
    gl.finish(); // flush the GL pipeline so timing reflects real GPU work
    const elapsed = performance.now() - start;
    const avgMs = elapsed / n;
    return { avgMs, fps: 1000 / avgMs };
  },
};

// ---------------------------------------------------------------------------
// Resize
// ---------------------------------------------------------------------------
function onResize() {
  const w = window.innerWidth;
  const h = window.innerHeight;
  camera.aspect = w / h;
  camera.updateProjectionMatrix();
  renderer.setSize(w, h);
}
window.addEventListener('resize', onResize);

// ---------------------------------------------------------------------------
// FPS meter
// ---------------------------------------------------------------------------
let frameCount = 0;
let fpsLast = performance.now();
function updateFps(now) {
  frameCount++;
  const elapsed = now - fpsLast;
  if (elapsed >= 500) {
    const fps = (frameCount * 1000) / elapsed;
    if (els.fps) {
      els.fps.textContent = fps.toFixed(0);
      els.fps.classList.remove('good', 'ok', 'bad');
      els.fps.classList.add(fps >= 50 ? 'good' : fps >= 30 ? 'ok' : 'bad');
    }
    frameCount = 0;
    fpsLast = now;
  }
}

// ---------------------------------------------------------------------------
// District labels (fade in when camera is high/far, out when low/close)
// ---------------------------------------------------------------------------
let labelGroup = null;
function buildLabels(city) {
  if (labelGroup) {
    scene.remove(labelGroup);
    labelGroup.traverse((o) => {
      if (o.material) {
        if (o.material.map) o.material.map.dispose();
        o.material.dispose();
      }
    });
  }
  labelGroup = new THREE.Group();
  const span = city.citySpan;
  const pitch = city.pitch;
  const bc = (i, j) => -span / 2 + pitch / 2 + i * pitch;
  const defs = [
    { text: 'DOWNTOWN', i: 7, j: 7, y: 150, scale: 70 },
    { text: 'MIDRISE', i: 10, j: 7, y: 55, scale: 55 },
    { text: 'RESIDENTIAL', i: 12, j: 1, y: 22, scale: 50 },
    { text: 'THE PARK', i: 3, j: 10, y: 28, scale: 48 },
  ];
  for (const d of defs) {
    const tex = makeLabelTexture(d.text);
    const mat = new THREE.SpriteMaterial({
      map: tex,
      transparent: true,
      opacity: 0,
      depthWrite: false,
      depthTest: true,
      fog: true,
    });
    const sp = new THREE.Sprite(mat);
    sp.position.set(bc(d.i, d.j), d.y, bc(d.j, d.i));
    sp.scale.set(d.scale, d.scale * (tex.image.height / tex.image.width), 1);
    sp.userData.baseScale = d.scale;
    labelGroup.add(sp);
  }
  scene.add(labelGroup);
}

const _labelTmp = new THREE.Vector3();
function updateLabels() {
  if (!labelGroup) return;
  const camY = camera.position.y;
  const camDist = camera.position.length();
  for (const sp of labelGroup.children) {
    _labelTmp.copy(sp.position);
    // fade IN with distance and altitude; fade OUT when close/low
    const distFade = THREE.MathUtils.clamp((camDist - 200) / 160, 0, 1);
    const altFade = THREE.MathUtils.clamp((camY - 70) / 110, 0, 1);
    let op = Math.min(distFade, altFade);
    if (flythrough.isActive()) op = Math.max(op, 0.6);
    sp.material.opacity = op * 0.85;
    // face-on scale stays constant (sprites always face camera)
  }
}

// ---------------------------------------------------------------------------
// Boot + render loop
// ---------------------------------------------------------------------------
rebuild(currentSeed);
buildLabels(cityState);
daynight.setTimeOfDay(13); // first paint: daytime
syncTimeUI();
applyQuality(quality);

// Collapse the panel by default on small screens so it never overflows.
if (window.matchMedia && window.matchMedia('(max-width: 640px)').matches) {
  if (els.panel) {
    els.panel.classList.add('collapsed');
    if (els.collapseBtn) els.collapseBtn.setAttribute('aria-expanded', 'false');
  }
}

let lastT = performance.now();
let firstFrameRendered = false;
function animate() {
  requestAnimationFrame(animate);
  const now = performance.now();
  const dt = now - lastT;
  lastT = now;

  if (flythrough.isActive()) {
    flythrough.update(dt);
  } else {
    controls.update();
  }
  daynight.update(dt);
  if (trafficState) trafficState.update(dt, camera);
  updateLabels();
  if (els.timeVal && daynight.autoCycle) els.timeVal.textContent = fmtTime(daynight.getTimeOfDay());
  renderer.render(scene, camera);
  updateFps(now);
  if (!firstFrameRendered) {
    firstFrameRendered = true;
    hideLoading();
  }
}
animate();
