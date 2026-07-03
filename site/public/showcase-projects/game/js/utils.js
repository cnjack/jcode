// utils.js — shared constants & math helpers (pure, no side effects)

export const LOGICAL_W = 1280;
export const LOGICAL_H = 720;

export const CELL = 64;
export const COLS = 20;          // 20 * 64 = 1280 (exact width)
export const ROWS = 10;          // 10 * 64 = 640
export const GRID_X = 0;
export const GRID_Y = 64;        // top 64px reserved for HUD strip; bottom margin = 16

export const TAU = Math.PI * 2;

export const clamp = (v, a, b) => (v < a ? a : v > b ? b : v);
export const lerp = (a, b, t) => a + (b - a) * t;
export const smooth = (t) => t * t * (3 - 2 * t); // smoothstep

export const dist = (x1, y1, x2, y2) => Math.hypot(x2 - x1, y2 - y1);
export const dist2 = (x1, y1, x2, y2) => {
  const dx = x2 - x1, dy = y2 - y1;
  return dx * dx + dy * dy;
};

// shortest absolute angular delta from a to b (radians)
export function angleDelta(a, b) {
  let d = (b - a) % TAU;
  if (d < -Math.PI) d += TAU;
  if (d > Math.PI) d -= TAU;
  return d;
}

// rotate a toward b by at most maxStep radians
export function rotateToward(a, b, maxStep) {
  const d = angleDelta(a, b);
  if (Math.abs(d) <= maxStep) return b;
  return a + Math.sign(d) * maxStep;
}

// grid cell center -> logical pixel
export function cellCenter(gx, gy) {
  return { x: GRID_X + gx * CELL + CELL / 2, y: GRID_Y + gy * CELL + CELL / 2 };
}

// logical pixel -> grid cell (may be out of bounds)
export function cellAt(px, py) {
  return {
    gx: Math.floor((px - GRID_X) / CELL),
    gy: Math.floor((py - GRID_Y) / CELL),
  };
}

export function inBounds(gx, gy) {
  return gx >= 0 && gy >= 0 && gx < COLS && gy < ROWS;
}

// roundrect polyfill helper (some Safari versions lack ctx.roundRect)
export function roundRect(ctx, x, y, w, h, r) {
  const rr = Math.min(r, w / 2, h / 2);
  ctx.beginPath();
  ctx.moveTo(x + rr, y);
  ctx.arcTo(x + w, y, x + w, y + h, rr);
  ctx.arcTo(x + w, y + h, x, y + h, rr);
  ctx.arcTo(x, y + h, x, y, rr);
  ctx.arcTo(x, y, x + w, y, rr);
  ctx.closePath();
}

// tiny seeded PRNG for deterministic decorations
export function mulberry32(seed) {
  let a = seed >>> 0;
  return function () {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export const FONT_MONO = "'SF Mono','JetBrains Mono','Consolas','Menlo','Courier New',monospace";
export const FONT_SANS = "-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif";
