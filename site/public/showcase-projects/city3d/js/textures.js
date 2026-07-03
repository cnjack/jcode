// js/textures.js
// Procedural canvas textures generated at runtime so the project needs zero
// external image assets. Everything is drawn on <canvas> elements and wrapped
// in THREE.CanvasTexture.

import * as THREE from 'three';

// Helper: create a canvas of given size.
function makeCanvas(w, h) {
  const c = document.createElement('canvas');
  c.width = w;
  c.height = h;
  return c;
}

// Small inline mulberry32 so textures are deterministic per seed.
function mulberry32(seed) {
  let a = seed >>> 0;
  return function () {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// ---------------------------------------------------------------------------
// Window grid texture (albedo map + emissive map for buildings).
//
// The facade is a REGULAR grid: continuous facade-coloured gutters (columns +
// rows) with darker/bluer glass panes inset — low contrast so the facade colour
// dominates during the day. Gutters are drawn neutral/light so per-instance
// (district) tinting via instanceColor still drives the final hue.
//
// Lit windows are emissive ONLY (they live in the emissive map, not the albedo).
// During the day the building's emissiveIntensity is ~0, so the facade reads as
// clean glass; at night emissiveIntensity ramps up and the lit subset glows.
//
// `cols`/`rows` = windows across / up within ONE tile. The tile repeats (per
// instance via the aUvScale attribute) so tall towers get many rows of windows
// without stretching. `unit` = texels per cell horizontally (quality knob).
// ---------------------------------------------------------------------------
export function makeWindowTexture(opts) {
  const {
    cols = 8,
    rows = 16,
    lit = 0.4, // fraction of panes that glow at night
    litColors = ['#ffd98a', '#cfe6ff'],
    seed = 1,
    unit = 28, // texels per cell horizontally (vertical ~ unit*1.15)
  } = opts || {};

  const cellW = unit;
  const cellH = Math.round(unit * 1.15);
  // Gutter thickness within a cell (facade between windows). Kept thin so the
  // pane is ~2.5–4x the gutter, reading as a clear grid, not houndstooth.
  const gx = Math.max(3, Math.round(unit * 0.24));
  const gy = Math.max(3, Math.round(unit * 0.22));
  const paneW = cellW - gx;
  const paneH = cellH - gy;
  const offX = Math.round((cellW - paneW) / 2);
  const offY = Math.round((cellH - paneH) / 2);
  // Mullion cross thickness (subtle window-frame division inside a pane).
  const mull = Math.max(1, Math.round(unit * 0.05));

  const cw = cols * cellW;
  const ch = rows * cellH;

  const rnd = mulberry32(seed);

  // ---- albedo: gutters (facade) + darker/bluer glass panes ----
  const cc = makeCanvas(cw, ch);
  const cg = cc.getContext('2d');
  // Facade / gutter base — neutral light so instanceColor drives final hue.
  cg.fillStyle = '#c9ccd4';
  cg.fillRect(0, 0, cw, ch);
  // Glass panes — slightly darker + bluer than the facade (low contrast).
  cg.fillStyle = '#8b95a4';
  for (let y = 0; y < rows; y++) {
    for (let x = 0; x < cols; x++) {
      cg.fillRect(x * cellW + offX, y * cellH + offY, paneW, paneH);
    }
  }
  // Faint mullion cross (vertical + horizontal) inside each pane — reads as
  // window framing without raising overall contrast much.
  cg.fillStyle = 'rgba(201, 204, 212, 0.55)';
  for (let y = 0; y < rows; y++) {
    for (let x = 0; x < cols; x++) {
      const px = x * cellW + offX;
      const py = y * cellH + offY;
      const mx = px + Math.floor(paneW / 2);
      const my = py + Math.floor(paneH / 2);
      cg.fillRect(mx, py, mull, paneH); // vertical mullion
      cg.fillRect(px, my, paneW, mull); // horizontal mullion
    }
  }
  // Very subtle top→bottom shade for fake AO/weathering (kept faint).
  const grd = cg.createLinearGradient(0, 0, 0, ch);
  grd.addColorStop(0, 'rgba(255,255,255,0.045)');
  grd.addColorStop(1, 'rgba(0,0,0,0.10)');
  cg.fillStyle = grd;
  cg.fillRect(0, 0, cw, ch);

  // ---- emissive: black everywhere except lit panes (night glow) ----
  const ec = makeCanvas(cw, ch);
  const eg = ec.getContext('2d');
  eg.fillStyle = '#000';
  eg.fillRect(0, 0, cw, ch);
  for (let y = 0; y < rows; y++) {
    for (let x = 0; x < cols; x++) {
      if (rnd() >= lit) continue;
      const col = litColors[Math.floor(rnd() * litColors.length)];
      const px = x * cellW + offX;
      const py = y * cellH + offY;
      // Full pane glow...
      eg.fillStyle = col;
      eg.fillRect(px, py, paneW, paneH);
      // ...plus a brighter inner core for a "lit room" pop at night.
      const ix = Math.round(paneW * 0.18);
      const iy = Math.round(paneH * 0.18);
      eg.globalAlpha = 0.5;
      eg.fillStyle = '#ffffff';
      eg.fillRect(px + ix, py + iy, paneW - ix * 2, paneH - iy * 2);
      eg.globalAlpha = 1;
    }
  }

  const colorTex = new THREE.CanvasTexture(cc);
  colorTex.wrapS = THREE.RepeatWrapping;
  colorTex.wrapT = THREE.RepeatWrapping;
  colorTex.colorSpace = THREE.SRGBColorSpace;
  colorTex.anisotropy = 4;

  const emissiveTex = new THREE.CanvasTexture(ec);
  emissiveTex.wrapS = THREE.RepeatWrapping;
  emissiveTex.wrapT = THREE.RepeatWrapping;
  emissiveTex.colorSpace = THREE.SRGBColorSpace;
  emissiveTex.anisotropy = 4;

  return { color: colorTex, emissive: emissiveTex };
}

// ---------------------------------------------------------------------------
// Soft puffy cloud texture (RGBA) for drifting cloud billboards. Built from a
// few overlapping radial blobs so it reads as a translucent wisp.
// ---------------------------------------------------------------------------
let _cloudTex = null;
export function makeCloudTexture() {
  if (_cloudTex) return _cloudTex;
  const s = 256;
  const c = makeCanvas(s, s);
  const g = c.getContext('2d');
  g.clearRect(0, 0, s, s);
  const rnd = mulberry32(2024);
  // cluster of soft white blobs
  for (let i = 0; i < 14; i++) {
    const cx = s * (0.3 + rnd() * 0.4);
    const cy = s * (0.4 + rnd() * 0.25);
    const r = s * (0.12 + rnd() * 0.2);
    const grd = g.createRadialGradient(cx, cy, 0, cx, cy, r);
    grd.addColorStop(0.0, 'rgba(255,255,255,0.55)');
    grd.addColorStop(0.5, 'rgba(255,255,255,0.30)');
    grd.addColorStop(1.0, 'rgba(255,255,255,0.0)');
    g.fillStyle = grd;
    g.fillRect(0, 0, s, s);
  }
  const tex = new THREE.CanvasTexture(c);
  tex.colorSpace = THREE.SRGBColorSpace;
  _cloudTex = tex;
  return tex;
}

// ---------------------------------------------------------------------------
// Soft radial glow sprite texture (white). Tint per-use via material.color.
// Used for the sun/moon discs, street-lamp halos and car head/tail lights.
// ---------------------------------------------------------------------------
let _glowTex = null;
export function makeGlowTexture() {
  if (_glowTex) return _glowTex;
  const s = 128;
  const c = makeCanvas(s, s);
  const g = c.getContext('2d');
  const grd = g.createRadialGradient(s / 2, s / 2, 0, s / 2, s / 2, s / 2);
  grd.addColorStop(0.0, 'rgba(255,255,255,1.0)');
  grd.addColorStop(0.25, 'rgba(255,255,255,0.65)');
  grd.addColorStop(0.55, 'rgba(255,255,255,0.18)');
  grd.addColorStop(1.0, 'rgba(255,255,255,0.0)');
  g.fillStyle = grd;
  g.fillRect(0, 0, s, s);
  const tex = new THREE.CanvasTexture(c);
  tex.colorSpace = THREE.SRGBColorSpace;
  _glowTex = tex;
  return tex;
}

// Tiny round star point texture.
let _starTex = null;
export function makeStarTexture() {
  if (_starTex) return _starTex;
  const s = 32;
  const c = makeCanvas(s, s);
  const g = c.getContext('2d');
  const grd = g.createRadialGradient(s / 2, s / 2, 0, s / 2, s / 2, s / 2);
  grd.addColorStop(0.0, 'rgba(255,255,255,1.0)');
  grd.addColorStop(0.4, 'rgba(255,255,255,0.7)');
  grd.addColorStop(1.0, 'rgba(255,255,255,0.0)');
  g.fillStyle = grd;
  g.fillRect(0, 0, s, s);
  const tex = new THREE.CanvasTexture(c);
  tex.colorSpace = THREE.SRGBColorSpace;
  _starTex = tex;
  return tex;
}

// ---------------------------------------------------------------------------
// Crisp text label texture for district sprites (transparent background).
// Drawn at high resolution so it stays sharp when scaled in the scene.
// ---------------------------------------------------------------------------
export function makeLabelTexture(text) {
  const fs = 96;
  const pad = 28;
  // measure first to size the canvas to the text
  const probe = makeCanvas(10, 10);
  const pg = probe.getContext('2d');
  pg.font = `700 ${fs}px -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif`;
  const w = Math.ceil(pg.measureText(text).width) + pad * 2;
  const h = fs + pad * 2;
  const c = makeCanvas(w, h);
  const g = c.getContext('2d');
  g.font = `700 ${fs}px -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif`;
  g.textAlign = 'center';
  g.textBaseline = 'middle';
  // subtle glow
  g.shadowColor = 'rgba(92,224,255,0.9)';
  g.shadowBlur = 24;
  g.fillStyle = 'rgba(180,230,255,0.95)';
  g.fillText(text, w / 2, h / 2);
  // crisp core
  g.shadowBlur = 0;
  g.fillStyle = '#eaf6ff';
  g.fillText(text, w / 2, h / 2);
  const tex = new THREE.CanvasTexture(c);
  tex.colorSpace = THREE.SRGBColorSpace;
  tex.anisotropy = 8;
  return tex;
}

// ---------------------------------------------------------------------------
// Road texture: dark asphalt with a dashed yellow center line running along V.
// Tileable along its length (V). Returns a CanvasTexture.
// ---------------------------------------------------------------------------
export function makeRoadTexture() {
  const w = 128;
  const h = 256;
  const c = makeCanvas(w, h);
  const g = c.getContext('2d');

  // asphalt base
  g.fillStyle = '#1a1d22';
  g.fillRect(0, 0, w, h);

  // noise speckle
  const rnd = mulberry32(7);
  for (let i = 0; i < 1400; i++) {
    const v = Math.floor(rnd() * 40);
    g.fillStyle = `rgba(${v + 20},${v + 22},${v + 26},0.5)`;
    g.fillRect(rnd() * w, rnd() * h, 2, 2);
  }

  // dashed center line
  const dashH = 28;
  const gap = 24;
  g.fillStyle = '#e8c35a';
  for (let y = -gap; y < h + dashH; y += dashH + gap) {
    g.fillRect(w / 2 - 3, y, 6, dashH);
  }

  const tex = new THREE.CanvasTexture(c);
  tex.wrapS = THREE.ClampToEdgeWrapping;
  tex.wrapT = THREE.RepeatWrapping;
  tex.colorSpace = THREE.SRGBColorSpace;
  return tex;
}

// ---------------------------------------------------------------------------
// Sidewalk texture: light concrete tiles.
// ---------------------------------------------------------------------------
export function makeSidewalkTexture() {
  const s = 128;
  const c = makeCanvas(s, s);
  const g = c.getContext('2d');
  g.fillStyle = '#8d8f95';
  g.fillRect(0, 0, s, s);
  // tile lines
  g.strokeStyle = 'rgba(0,0,0,0.18)';
  g.lineWidth = 2;
  const step = 32;
  for (let i = 0; i <= s; i += step) {
    g.beginPath();
    g.moveTo(i, 0);
    g.lineTo(i, s);
    g.stroke();
    g.beginPath();
    g.moveTo(0, i);
    g.lineTo(s, i);
    g.stroke();
  }
  // speckle
  const rnd = mulberry32(3);
  for (let i = 0; i < 500; i++) {
    const v = Math.floor(rnd() * 30);
    g.fillStyle = `rgba(${v},${v},${v},0.25)`;
    g.fillRect(rnd() * s, rnd() * s, 2, 2);
  }
  const tex = new THREE.CanvasTexture(c);
  tex.wrapS = THREE.RepeatWrapping;
  tex.wrapT = THREE.RepeatWrapping;
  tex.colorSpace = THREE.SRGBColorSpace;
  return tex;
}

// ---------------------------------------------------------------------------
// Park grass texture.
// ---------------------------------------------------------------------------
export function makeGrassTexture() {
  const s = 128;
  const c = makeCanvas(s, s);
  const g = c.getContext('2d');
  g.fillStyle = '#3f7d3a';
  g.fillRect(0, 0, s, s);
  const rnd = mulberry32(11);
  for (let i = 0; i < 1600; i++) {
    const gr = 80 + Math.floor(rnd() * 70);
    g.fillStyle = `rgba(${Math.floor(gr * 0.5)},${gr},${Math.floor(gr * 0.4)},0.5)`;
    g.fillRect(rnd() * s, rnd() * s, 2, 2);
  }
  const tex = new THREE.CanvasTexture(c);
  tex.wrapS = THREE.RepeatWrapping;
  tex.wrapT = THREE.RepeatWrapping;
  tex.colorSpace = THREE.SRGBColorSpace;
  return tex;
}
