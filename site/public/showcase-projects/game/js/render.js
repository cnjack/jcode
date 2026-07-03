// render.js — world rendering: PCB background, trace, pads, CPU, spawn, particles, beams

import {
  LOGICAL_W, LOGICAL_H, CELL, COLS, ROWS, GRID_X, GRID_Y,
  TAU, mulberry32,
} from './utils.js';
import {
  WAYPOINTS, CPU_POINT, SPAWN_POINT, PADS,
} from './map.js';

// ---------------- PCB background (cached to offscreen) ----------------
let bgCanvas = null;

export function buildBackground() {
  if (bgCanvas) return bgCanvas;
  const off = document.createElement('canvas');
  off.width = LOGICAL_W; off.height = LOGICAL_H;
  const c = off.getContext('2d');
  const rng = mulberry32(1337);

  // base gradient
  const g = c.createLinearGradient(0, 0, 0, LOGICAL_H);
  g.addColorStop(0, '#08130f');
  g.addColorStop(1, '#040a08');
  c.fillStyle = g;
  c.fillRect(0, 0, LOGICAL_W, LOGICAL_H);

  // faint cell grid within board area
  c.strokeStyle = 'rgba(60,200,150,0.05)';
  c.lineWidth = 1;
  for (let gx = 0; gx <= COLS; gx++) {
    const x = GRID_X + gx * CELL + 0.5;
    c.beginPath(); c.moveTo(x, GRID_Y); c.lineTo(x, GRID_Y + ROWS * CELL); c.stroke();
  }
  for (let gy = 0; gy <= ROWS; gy++) {
    const y = GRID_Y + gy * CELL + 0.5;
    c.beginPath(); c.moveTo(GRID_X, y); c.lineTo(GRID_X + COLS * CELL, y); c.stroke();
  }

  // decorative silkscreen components & vias (avoid path area visually — purely cosmetic)
  drawSilkscreen(c, rng);

  // top HUD strip backdrop
  const hg = c.createLinearGradient(0, 0, 0, GRID_Y);
  hg.addColorStop(0, '#0a1815');
  hg.addColorStop(1, '#06100d');
  c.fillStyle = hg;
  c.fillRect(0, 0, LOGICAL_W, GRID_Y);
  c.strokeStyle = 'rgba(80,255,210,0.25)';
  c.beginPath(); c.moveTo(0, GRID_Y + 0.5); c.lineTo(LOGICAL_W, GRID_Y + 0.5); c.stroke();

  bgCanvas = off;
  return off;
}

function drawSilkscreen(c, rng) {
  const boardW = COLS * CELL, boardH = ROWS * CELL;
  // a few decorative trace lines + vias
  c.strokeStyle = 'rgba(90,255,200,0.06)';
  c.lineWidth = 2;
  for (let i = 0; i < 26; i++) {
    const x1 = GRID_X + Math.floor(rng() * COLS) * CELL + CELL / 2;
    const y1 = GRID_Y + Math.floor(rng() * ROWS) * CELL + CELL / 2;
    const horiz = rng() > 0.5;
    const len = (1 + Math.floor(rng() * 3)) * CELL * (rng() > 0.5 ? 1 : -1);
    c.beginPath();
    c.moveTo(x1, y1);
    if (horiz) c.lineTo(x1 + len, y1); else c.lineTo(x1, y1 + len);
    c.stroke();
    via(c, x1, y1, 2);
  }
  // a couple of decorative IC outlines
  c.strokeStyle = 'rgba(120,255,220,0.08)';
  icOutline(c, GRID_X + 1.5 * CELL, GRID_Y + 0.4 * CELL, 2 * CELL, 1.1 * CELL);
  icOutline(c, GRID_X + 16.2 * CELL, GRID_Y + 8.0 * CELL, 1.6 * CELL, 0.9 * CELL);
  // board edge border
  c.strokeStyle = 'rgba(80,255,210,0.18)';
  c.lineWidth = 2;
  c.strokeRect(GRID_X + 1, GRID_Y + 1, boardW - 2, boardH - 2);
}

function via(c, x, y, r) {
  c.fillStyle = 'rgba(200,180,90,0.5)';
  c.beginPath(); c.arc(x, y, r, 0, TAU); c.fill();
  c.fillStyle = '#040a08';
  c.beginPath(); c.arc(x, y, r * 0.5, 0, TAU); c.fill();
}

function icOutline(c, x, y, w, h) {
  c.save();
  c.translate(x, y);
  c.strokeRect(-w / 2, -h / 2, w, h);
  for (let i = 0; i < 5; i++) {
    const px = -w / 2 + (i + 0.5) * (w / 5);
    c.beginPath(); c.arc(px, -h / 2, 1.5, 0, TAU); c.stroke();
    c.beginPath(); c.arc(px, h / 2, 1.5, 0, TAU); c.stroke();
  }
  c.restore();
}

export function drawBackground(ctx) {
  const bg = buildBackground();
  ctx.drawImage(bg, 0, 0);
}

// ---------------- enemy trace (animated) ----------------
export function drawTrace(ctx, time) {
  // outer glow
  ctx.save();
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';

  ctx.strokeStyle = 'rgba(255,180,70,0.10)';
  ctx.lineWidth = 30;
  tracePath(ctx);
  ctx.stroke();

  // copper body
  ctx.strokeStyle = '#3a2a14';
  ctx.lineWidth = 20;
  tracePath(ctx);
  ctx.stroke();

  // bright core
  ctx.strokeStyle = '#ffae3d';
  ctx.lineWidth = 8;
  tracePath(ctx);
  ctx.stroke();

  // energised pulse travelling along
  ctx.strokeStyle = '#fff2c0';
  ctx.lineWidth = 4;
  ctx.setLineDash([26, 600]);
  ctx.lineDashOffset = -(time * 220) % 2000;
  ctx.shadowColor = '#ffd27a';
  ctx.shadowBlur = 10;
  tracePath(ctx);
  ctx.stroke();
  ctx.setLineDash([]);
  ctx.shadowBlur = 0;

  // vias at each waypoint corner
  for (const w of WAYPOINTS) {
    if (w.x < 0) continue;
    ctx.fillStyle = '#1a1206';
    ctx.strokeStyle = '#ffae3d';
    ctx.lineWidth = 2;
    ctx.beginPath(); ctx.arc(w.x, w.y, 6, 0, TAU); ctx.fill(); ctx.stroke();
  }
  ctx.restore();
}

function tracePath(ctx) {
  ctx.beginPath();
  ctx.moveTo(WAYPOINTS[0].x, WAYPOINTS[0].y);
  for (let i = 1; i < WAYPOINTS.length; i++) ctx.lineTo(WAYPOINTS[i].x, WAYPOINTS[i].y);
}

// ---------------- build pads ----------------
export function drawPads(ctx, pads, hoverPad, time) {
  for (const p of pads) {
    const pulse = 0.5 + 0.5 * Math.sin(time * 2 + p.gx + p.gy);
    ctx.save();
    ctx.translate(p.x, p.y);
    // pad ring
    ctx.fillStyle = p.occupied ? 'rgba(20,30,28,0.9)' : 'rgba(15,40,33,0.65)';
    ctx.strokeStyle = p.occupied ? 'rgba(120,255,210,0.15)' :
      (p === hoverPad ? `rgba(120,255,210,${0.7 + pulse * 0.3})` : 'rgba(120,255,210,0.30)');
    ctx.lineWidth = p === hoverPad ? 2 : 1;
    ctx.beginPath(); ctx.arc(0, 0, 20, 0, TAU); ctx.fill(); ctx.stroke();
    // inner crosshair for empty pads
    if (!p.occupied) {
      ctx.strokeStyle = `rgba(120,255,210,${0.2 + (p === hoverPad ? 0.5 : 0)})`;
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(-6, 0); ctx.lineTo(6, 0);
      ctx.moveTo(0, -6); ctx.lineTo(0, 6);
      ctx.stroke();
    }
    ctx.restore();
  }
}

// ---------------- spawn connector ----------------
export function drawSpawn(ctx, time) {
  const s = SPAWN_POINT;
  ctx.save();
  // gold edge-finger connector just off the left edge at row 1
  const y = s.y;
  ctx.fillStyle = '#caa23a';
  for (let i = -2; i <= 2; i++) {
    ctx.fillRect(-12, y + i * 12 - 4, 18, 8);
  }
  ctx.fillStyle = 'rgba(255,220,120,0.25)';
  ctx.fillRect(-14, y - 34, 4, 68);
  // pulsing arrow at entry
  const a = 0.5 + 0.5 * Math.sin(time * 4);
  ctx.fillStyle = `rgba(255,174,61,${0.4 + a * 0.5})`;
  ctx.beginPath();
  ctx.moveTo(4, y - 8); ctx.lineTo(18, y); ctx.lineTo(4, y + 8); ctx.closePath();
  ctx.fill();
  ctx.restore();
}

// ---------------- CPU core ----------------
export function drawCpu(ctx, lives, maxLives, time, flash) {
  const c = CPU_POINT;
  const frac = Math.max(0, lives / maxLives);
  ctx.save();
  ctx.translate(c.x, c.y);

  // life ring
  ctx.strokeStyle = 'rgba(120,255,210,0.18)';
  ctx.lineWidth = 4;
  ctx.beginPath(); ctx.arc(0, 0, 34, 0, TAU); ctx.stroke();
  const col = frac > 0.5 ? '#5dff9b' : frac > 0.25 ? '#ffd84d' : '#ff5d5d';
  ctx.strokeStyle = col;
  ctx.lineWidth = 4;
  ctx.lineCap = 'round';
  ctx.beginPath(); ctx.arc(0, 0, 34, -Math.PI / 2, -Math.PI / 2 + frac * TAU); ctx.stroke();

  // chip body
  ctx.fillStyle = flash > 0 ? '#3a1414' : '#0c1a16';
  ctx.strokeStyle = flash > 0 ? '#ff5d5d' : '#46e0b0';
  ctx.lineWidth = 2;
  roundRect(ctx, -22, -22, 44, 44, 5); ctx.fill(); ctx.stroke();
  // pins
  ctx.fillStyle = 'rgba(200,230,220,0.5)';
  for (let i = -1; i <= 1; i++) {
    ctx.fillRect(-26, i * 10 - 2, 4, 4); ctx.fillRect(22, i * 10 - 2, 4, 4);
    ctx.fillRect(i * 10 - 2, -26, 4, 4); ctx.fillRect(i * 10 - 2, 22, 4, 4);
  }
  // glowing core
  const pulse = 0.6 + 0.4 * Math.sin(time * 3);
  ctx.fillStyle = col;
  ctx.shadowColor = col; ctx.shadowBlur = 14 * pulse;
  ctx.beginPath(); ctx.arc(0, 0, 8 + pulse * 2, 0, TAU); ctx.fill();
  ctx.shadowBlur = 0;
  ctx.fillStyle = '#eafff5';
  ctx.font = "bold 9px 'SF Mono',monospace";
  ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
  ctx.fillText('CPU', 0, 0);
  ctx.restore();
}

// ---------------- beams ----------------
export function drawBeam(ctx, tower, target, alpha) {
  if (!target) return;
  const tx = tower.x + Math.cos(tower.renderAngle(alpha)) * 13;
  const ty = tower.y + Math.sin(tower.renderAngle(alpha)) * 13;
  ctx.save();
  ctx.lineCap = 'round';
  ctx.strokeStyle = 'rgba(255,93,176,0.25)';
  ctx.lineWidth = 8;
  ctx.beginPath(); ctx.moveTo(tx, ty); ctx.lineTo(target.x, target.y); ctx.stroke();
  ctx.strokeStyle = tower.cfg.color;
  ctx.lineWidth = 3;
  ctx.shadowColor = tower.cfg.color; ctx.shadowBlur = 12;
  ctx.beginPath(); ctx.moveTo(tx, ty); ctx.lineTo(target.x, target.y); ctx.stroke();
  ctx.strokeStyle = '#fff';
  ctx.lineWidth = 1.2;
  ctx.beginPath(); ctx.moveTo(tx, ty); ctx.lineTo(target.x, target.y); ctx.stroke();
  ctx.restore();
}

// ---------------- particles ----------------
export const particles = [];
export function burst(x, y, color, n = 10, speed = 140) {
  for (let i = 0; i < n; i++) {
    if (particles.length > 360) break;
    const a = Math.random() * TAU;
    const sp = speed * (0.3 + Math.random() * 0.7);
    particles.push({
      x, y,
      vx: Math.cos(a) * sp, vy: Math.sin(a) * sp,
      life: 0.4 + Math.random() * 0.4, max: 0.8,
      color, r: 1.5 + Math.random() * 2,
    });
  }
}
export function updateParticles(dt) {
  for (let i = particles.length - 1; i >= 0; i--) {
    const p = particles[i];
    p.life -= dt;
    if (p.life <= 0) { particles.splice(i, 1); continue; }
    p.x += p.vx * dt; p.y += p.vy * dt;
    p.vx *= 0.92; p.vy *= 0.92;
  }
}
export function drawParticles(ctx) {
  for (const p of particles) {
    ctx.globalAlpha = Math.max(0, p.life / p.max);
    ctx.fillStyle = p.color;
    ctx.beginPath(); ctx.arc(p.x, p.y, p.r, 0, TAU); ctx.fill();
  }
  ctx.globalAlpha = 1;
}

// ---------------- floating damage numbers (additive, capped) ----------------
export const floaters = [];
const MAX_FLOATERS = 180;
export function spawnFloater(x, y, val, color) {
  if (floaters.length >= MAX_FLOATERS) floaters.shift();
  floaters.push({ x, y: y - 4, val, color, life: 0.7, max: 0.7, vy: -34, vx: (Math.random() - 0.5) * 14 });
}
export function updateFloaters(dt) {
  for (let i = floaters.length - 1; i >= 0; i--) {
    const f = floaters[i];
    f.life -= dt;
    if (f.life <= 0) { floaters.splice(i, 1); continue; }
    f.y += f.vy * dt; f.x += f.vx * dt; f.vy *= 0.94;
  }
}
export function drawFloaters(ctx) {
  ctx.save();
  ctx.globalCompositeOperation = 'lighter';
  ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
  ctx.font = `bold 13px 'SF Mono','JetBrains Mono','Consolas','Menlo',monospace`;
  for (const f of floaters) {
    const a = Math.max(0, f.life / f.max);
    ctx.globalAlpha = a;
    ctx.fillStyle = '#000';
    ctx.fillText(String(f.val), f.x + 1, f.y + 1);
    ctx.fillStyle = f.color;
    ctx.fillText(String(f.val), f.x, f.y);
  }
  ctx.globalAlpha = 1;
  ctx.restore();
}

// ---------------- expanding blast rings (splash impact) ----------------
export const rings = [];
export function spawnRing(x, y, maxR, color) {
  if (rings.length > 40) rings.shift();
  rings.push({ x, y, r: 4, maxR, life: 0.4, max: 0.4, color });
}
export function updateRings(dt) {
  for (let i = rings.length - 1; i >= 0; i--) {
    const rg = rings[i];
    rg.life -= dt;
    if (rg.life <= 0) { rings.splice(i, 1); continue; }
    rg.r += (rg.maxR - 4) * dt / rg.max;
  }
}
export function drawRings(ctx) {
  for (const rg of rings) {
    const a = Math.max(0, rg.life / rg.max);
    ctx.globalAlpha = a * 0.8;
    ctx.strokeStyle = rg.color;
    ctx.lineWidth = 3 * a + 1;
    ctx.beginPath(); ctx.arc(rg.x, rg.y, rg.r, 0, TAU); ctx.stroke();
  }
  ctx.globalAlpha = 1;
}

// ---------------- CPU damage red vignette ----------------
export function drawVignette(ctx, strength) {
  if (strength <= 0) return;
  const a = Math.min(1, strength) * 0.5;
  const g = ctx.createRadialGradient(LOGICAL_W / 2, LOGICAL_H / 2, LOGICAL_H * 0.35, LOGICAL_W / 2, LOGICAL_H / 2, LOGICAL_H * 0.8);
  g.addColorStop(0, 'rgba(255,40,40,0)');
  g.addColorStop(1, `rgba(255,30,30,${a})`);
  ctx.fillStyle = g;
  ctx.fillRect(0, 0, LOGICAL_W, LOGICAL_H);
}

// local roundRect (avoid pulling another import cycle)
function roundRect(ctx, x, y, w, h, r) {
  const rr = Math.min(r, w / 2, h / 2);
  ctx.beginPath();
  ctx.moveTo(x + rr, y);
  ctx.arcTo(x + w, y, x + w, y + h, rr);
  ctx.arcTo(x + w, y + h, x, y + h, rr);
  ctx.arcTo(x, y + h, x, y, rr);
  ctx.arcTo(x, y, x + w, y, rr);
  ctx.closePath();
}

export { PADS };
