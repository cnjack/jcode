// enemies.js — enemy types, movement & rendering (procedural sprites)

import { pointAtDistance, PATH_LENGTH } from './map.js';
import { TAU, clamp } from './utils.js';
import { spawnFloater } from './render.js';

// emit accumulated damage as a single floater, reset accumulator
function flushDamage(e) {
  spawnFloater(e.x, e.y - e.radius, Math.round(e.pendingDmg), '#ffffff');
  e.pendingDmg = 0;
  e.dmgTimer = 0;
}

export const ENEMY_TYPES = {
  bug:   { hp: 34,   speed: 68,  reward: 8,   score: 10,  radius: 15, color: '#7CFFB2', dark: '#0c2a16' },
  spark: { hp: 14,   speed: 142, reward: 5,   score: 8,   radius: 10, color: '#FFE066', dark: '#2a2305' },
  tank:  { hp: 130,  speed: 44,  reward: 20,  score: 28,  radius: 21, color: '#9bb2ff', dark: '#0a1230', armor: 4 },
  swarm: { hp: 28,   speed: 92,  reward: 6,   score: 9,   radius: 12, color: '#ff8f4d', dark: '#2a1408', splits: 2 },
  mini:  { hp: 11,   speed: 165, reward: 3,   score: 4,   radius: 7,  color: '#ffc59b', dark: '#2a1408' },
  boss:  { hp: 1100, speed: 34,  reward: 150, score: 400, radius: 30, color: '#ff5d8f', dark: '#2a0816', armor: 6, bossResist: true },
};

// display order + labels for wave preview
export const ENEMY_META = {
  bug:   { label: 'BUG',   short: 'BUG' },
  spark: { label: 'SPARK', short: 'SPK' },
  tank:  { label: 'TANK',  short: 'TNK' },
  swarm: { label: 'SWARM', short: 'SWM' },
  boss:  { label: 'BOSS',  short: 'BSS' },
};

let _id = 1;

export class Enemy {
  constructor(type, hpMul, spdMul, startDist = 0) {
    const cfg = ENEMY_TYPES[type];
    this.id = _id++;
    this.type = type;
    this.maxHp = Math.round(cfg.hp * hpMul);
    this.hp = this.maxHp;
    this.speed = cfg.speed * spdMul;
    this.reward = cfg.reward;
    this.scoreVal = cfg.score;
    this.radius = cfg.radius;
    this.color = cfg.color;
    this.dark = cfg.dark;
    this.armor = cfg.armor || 0;
    this.splits = cfg.splits || 0;
    this.bossResist = !!cfg.bossResist;

    this.dist = startDist;
    const p = pointAtDistance(this.dist);
    this.x = p.x; this.y = p.y; this.angle = p.angle;
    this.prevX = this.x; this.prevY = this.y; this.prevAngle = this.angle;

    this.alive = true;
    this.reachedCore = false;
    this.hitFlash = 0;       // seconds of white flash remaining
    this.animPhase = Math.random() * TAU;
    this.trail = [];         // recent positions (spark/mini trail)

    this.slowFactor = 1;     // 1 = normal; <1 = slowed
    this.slowTime = 0;       // seconds of slow remaining

    // floater aggregation: accumulate damage, emit one number per ~0.3s
    this.pendingDmg = 0;
    this.dmgTimer = 0;
  }

  update(dt, time) {
    this.prevX = this.x; this.prevY = this.y; this.prevAngle = this.angle;
    const fast = this.type === 'spark' || this.type === 'mini';
    this.animPhase += dt * (fast ? 26 : this.type === 'boss' ? 8 : 16);
    if (this.hitFlash > 0) this.hitFlash -= dt;
    if (this.slowTime > 0) { this.slowTime -= dt; if (this.slowTime <= 0) this.slowFactor = 1; }

    // aggregated damage floater: emit one combined number per ~0.3s
    if (this.dmgTimer > 0) {
      this.dmgTimer -= dt;
      if (this.dmgTimer <= 0 && this.pendingDmg > 0) flushDamage(this);
    }

    this.dist += this.speed * this.slowFactor * dt;
    const p = pointAtDistance(this.dist);
    this.x = p.x; this.y = p.y; this.angle = p.angle;

    if (fast) {
      this.trail.push({ x: this.x, y: this.y });
      if (this.trail.length > 8) this.trail.shift();
    }

    if (this.dist >= PATH_LENGTH) {
      this.reachedCore = true;
      this.alive = false;
    }
  }

  // Apply raw damage (armor reduces per hit, min 1). Returns actual damage dealt.
  damage(n) {
    const real = Math.max(1, n - this.armor);
    this.hp -= real;
    this.hitFlash = 0.12;
    if (this.hp <= 0) {
      this.hp = 0;
      this.alive = false;
    }
    return real;
  }

  // Apply a slow aura: factor<1 slows; does not stack multiplicatively (strongest wins).
  applySlow(factor, dur) {
    if (factor < this.slowFactor) this.slowFactor = factor;
    if (dur > this.slowTime) this.slowTime = dur;
  }

  // Push backward along the path (knockback). Bosses resist strongly.
  knockback(amount) {
    const eff = this.bossResist ? amount * 0.12 : amount;
    this.dist = Math.max(0, this.dist - eff);
  }

  // interpolated render position
  renderPos(alpha) {
    return {
      x: this.prevX + (this.x - this.prevX) * alpha,
      y: this.prevY + (this.y - this.prevY) * alpha,
    };
  }
}

export function drawEnemy(ctx, e, alpha, time) {
  const pos = e.renderPos(alpha);
  ctx.save();
  ctx.translate(pos.x, pos.y);
  if (e.type === 'bug') drawBug(ctx, e, time);
  else if (e.type === 'spark') drawSpark(ctx, e, time);
  else if (e.type === 'tank') drawTank(ctx, e, time);
  else if (e.type === 'swarm') drawSwarm(ctx, e, time);
  else if (e.type === 'mini') drawMini(ctx, e, time);
  else if (e.type === 'boss') drawBoss(ctx, e, time);
  ctx.restore();

  if (e.hp < e.maxHp) drawHpBar(ctx, pos.x, pos.y - e.radius - 9, e.hp / e.maxHp, e.radius);
}

// ---- existing bug (beetle) ----
function drawBug(ctx, e, time) {
  const r = e.radius;
  ctx.fillStyle = 'rgba(0,0,0,0.35)';
  ctx.beginPath(); ctx.ellipse(0, r * 0.55, r * 0.9, r * 0.4, 0, 0, TAU); ctx.fill();
  const swing = Math.sin(e.animPhase) * 0.5;
  ctx.strokeStyle = e.color; ctx.lineWidth = 1.6; ctx.lineCap = 'round';
  for (let i = 0; i < 3; i++) {
    const side = i % 2 === 0 ? 1 : -1;
    const yy = (i - 1) * r * 0.42;
    const lift = (i % 2 ? swing : -swing) * r * 0.35;
    ctx.beginPath(); ctx.moveTo(side * r * 0.5, yy);
    ctx.quadraticCurveTo(side * r * 0.9, yy + lift, side * r * 1.15, yy + lift * 0.4); ctx.stroke();
  }
  ctx.fillStyle = e.dark; ctx.strokeStyle = e.color; ctx.lineWidth = 1.5;
  roundEllipse(ctx, 0, 0, r * 1.05, r * 0.8); ctx.fill(); ctx.stroke();
  ctx.strokeStyle = e.color; ctx.globalAlpha = 0.6;
  ctx.beginPath(); ctx.moveTo(0, -r * 0.7); ctx.lineTo(0, r * 0.7); ctx.stroke();
  ctx.globalAlpha = 1;
  ctx.fillStyle = e.color;
  ctx.beginPath(); ctx.ellipse(0, -r * 0.78, r * 0.42, r * 0.4, 0, 0, TAU); ctx.fill();
  drawEyes(ctx, e, r);
  hitFlashEllipse(ctx, e, r * 1.05, r * 0.8, true);
}

function drawEyes(ctx, e, r) {
  ctx.fillStyle = '#fff'; ctx.shadowColor = e.color; ctx.shadowBlur = 6;
  ctx.beginPath();
  ctx.arc(-r * 0.18, -r * 0.82, 1.5, 0, TAU);
  ctx.arc(r * 0.18, -r * 0.82, 1.5, 0, TAU);
  ctx.fill(); ctx.shadowBlur = 0;
}

function hitFlashEllipse(ctx, e, rx, ry, useEllipse) {
  if (e.hitFlash <= 0) return;
  ctx.globalAlpha = clamp(e.hitFlash / 0.12, 0, 1) * 0.8;
  ctx.fillStyle = '#ffffff';
  if (useEllipse) roundEllipse(ctx, 0, 0, rx, ry); else ctx.beginPath();
  ctx.fill();
  ctx.globalAlpha = 1;
}

function drawSpark(ctx, e, time) {
  const r = e.radius;
  for (let i = 0; i < e.trail.length; i++) {
    const t = e.trail[i]; const f = i / e.trail.length;
    ctx.globalAlpha = f * 0.4; ctx.fillStyle = e.color;
    ctx.beginPath(); ctx.arc(t.x - e.x, t.y - e.y, r * (0.3 + f * 0.7), 0, TAU); ctx.fill();
  }
  ctx.globalAlpha = 1;
  const jitter = Math.sin(e.animPhase) * 1.2;
  ctx.shadowColor = e.color; ctx.shadowBlur = 16; ctx.fillStyle = e.color;
  ctx.beginPath(); ctx.arc(jitter, jitter * 0.5, r * 0.7, 0, TAU); ctx.fill();
  ctx.fillStyle = '#fffceb'; ctx.beginPath(); ctx.arc(0, 0, r * 0.38, 0, TAU); ctx.fill();
  ctx.shadowBlur = 0;
  ctx.strokeStyle = e.color; ctx.globalAlpha = 0.8; ctx.lineWidth = 1.2;
  for (let i = 0; i < 3; i++) {
    const a = (e.animPhase * 0.3 + i * 2.094);
    ctx.beginPath(); ctx.moveTo(0, 0); ctx.lineTo(Math.cos(a) * r * 1.1, Math.sin(a) * r * 1.1); ctx.stroke();
  }
  ctx.globalAlpha = 1;
  hitFlashCircle(ctx, e, r);
}

// ---- tank: chunky armored hex ----
function drawTank(ctx, e, time) {
  const r = e.radius;
  ctx.fillStyle = 'rgba(0,0,0,0.4)';
  ctx.beginPath(); ctx.ellipse(0, r * 0.6, r, r * 0.4, 0, 0, TAU); ctx.fill();
  // armor plates (hex)
  ctx.fillStyle = e.dark; ctx.strokeStyle = e.color; ctx.lineWidth = 2;
  hexPath(ctx, 0, 0, r); ctx.fill(); ctx.stroke();
  // inner core plate
  ctx.fillStyle = '#161c33'; hexPath(ctx, 0, 0, r * 0.62); ctx.fill(); ctx.stroke();
  // rivets
  ctx.fillStyle = e.color;
  for (let i = 0; i < 6; i++) {
    const a = i * TAU / 6;
    ctx.beginPath(); ctx.arc(Math.cos(a) * r * 0.78, Math.sin(a) * r * 0.78, 1.6, 0, TAU); ctx.fill();
  }
  // armor pips indicating armor value
  ctx.fillStyle = '#cfe0ff'; ctx.font = `bold 9px monospace`;
  ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
  ctx.fillText('▣', 0, 1);
  if (e.hitFlash > 0) {
    ctx.globalAlpha = clamp(e.hitFlash / 0.12, 0, 1) * 0.7; ctx.fillStyle = '#fff';
    hexPath(ctx, 0, 0, r); ctx.fill(); ctx.globalAlpha = 1;
  }
}

// ---- swarm: buzzing cluster ----
function drawSwarm(ctx, e, time) {
  const r = e.radius;
  ctx.fillStyle = 'rgba(0,0,0,0.3)';
  ctx.beginPath(); ctx.ellipse(0, r * 0.5, r * 0.9, r * 0.35, 0, 0, TAU); ctx.fill();
  // three orbiting motes
  ctx.shadowColor = e.color; ctx.shadowBlur = 10;
  for (let i = 0; i < 3; i++) {
    const a = e.animPhase + i * 2.094;
    const ox = Math.cos(a) * r * 0.55, oy = Math.sin(a) * r * 0.45;
    ctx.fillStyle = e.color;
    ctx.beginPath(); ctx.arc(ox, oy, r * 0.42, 0, TAU); ctx.fill();
  }
  ctx.shadowBlur = 0;
  ctx.fillStyle = '#fff'; ctx.globalAlpha = 0.8;
  ctx.beginPath(); ctx.arc(0, 0, r * 0.25, 0, TAU); ctx.fill();
  ctx.globalAlpha = 1;
  hitFlashCircle(ctx, e, r);
}

// ---- mini swarmling: tiny fast mote ----
function drawMini(ctx, e, time) {
  const r = e.radius;
  for (let i = 0; i < e.trail.length; i++) {
    const t = e.trail[i]; const f = i / e.trail.length;
    ctx.globalAlpha = f * 0.35; ctx.fillStyle = e.color;
    ctx.beginPath(); ctx.arc(t.x - e.x, t.y - e.y, r * (0.3 + f), 0, TAU); ctx.fill();
  }
  ctx.globalAlpha = 1;
  ctx.shadowColor = e.color; ctx.shadowBlur = 8; ctx.fillStyle = e.color;
  ctx.beginPath(); ctx.arc(0, 0, r * 0.8, 0, TAU); ctx.fill();
  ctx.fillStyle = '#fff'; ctx.beginPath(); ctx.arc(0, 0, r * 0.35, 0, TAU); ctx.fill();
  ctx.shadowBlur = 0;
  hitFlashCircle(ctx, e, r);
}

// ---- boss: huge armored core ----
function drawBoss(ctx, e, time) {
  const r = e.radius;
  ctx.fillStyle = 'rgba(0,0,0,0.5)';
  ctx.beginPath(); ctx.ellipse(0, r * 0.6, r * 1.1, r * 0.45, 0, 0, TAU); ctx.fill();
  // rotating outer ring of spikes
  ctx.save();
  ctx.rotate(e.animPhase * 0.4);
  ctx.fillStyle = e.color; ctx.globalAlpha = 0.9;
  for (let i = 0; i < 8; i++) {
    const a = i * TAU / 8;
    ctx.beginPath();
    ctx.moveTo(Math.cos(a) * r * 0.95, Math.sin(a) * r * 0.95);
    ctx.lineTo(Math.cos(a) * r * 1.3, Math.sin(a) * r * 1.3);
    ctx.lineTo(Math.cos(a + 0.18) * r * 0.95, Math.sin(a + 0.18) * r * 0.95);
    ctx.fill();
  }
  ctx.globalAlpha = 1;
  ctx.restore();
  // body
  ctx.fillStyle = e.dark; ctx.strokeStyle = e.color; ctx.lineWidth = 2.5;
  hexPath(ctx, 0, 0, r * 0.95); ctx.fill(); ctx.stroke();
  // glowing reactor core
  const pulse = 0.6 + 0.4 * Math.sin(time * 5);
  ctx.fillStyle = e.color; ctx.shadowColor = e.color; ctx.shadowBlur = 22 * pulse;
  ctx.beginPath(); ctx.arc(0, 0, r * 0.42 * (0.9 + pulse * 0.2), 0, TAU); ctx.fill();
  ctx.fillStyle = '#fff'; ctx.beginPath(); ctx.arc(0, 0, r * 0.2, 0, TAU); ctx.fill();
  ctx.shadowBlur = 0;
  if (e.hitFlash > 0) {
    ctx.globalAlpha = clamp(e.hitFlash / 0.12, 0, 1) * 0.5; ctx.fillStyle = '#fff';
    hexPath(ctx, 0, 0, r * 0.95); ctx.fill(); ctx.globalAlpha = 1;
  }
}

function hitFlashCircle(ctx, e, r) {
  if (e.hitFlash <= 0) return;
  ctx.globalAlpha = clamp(e.hitFlash / 0.12, 0, 1);
  ctx.fillStyle = '#fff';
  ctx.beginPath(); ctx.arc(0, 0, r, 0, TAU); ctx.fill();
  ctx.globalAlpha = 1;
}

function drawHpBar(ctx, cx, cy, frac, radius) {
  const w = Math.max(24, radius * 2.2), h = 4;
  ctx.fillStyle = 'rgba(0,0,0,0.6)';
  roundRectFill(ctx, cx - w / 2 - 1, cy - 1, w + 2, h + 2, 2);
  ctx.fillStyle = '#2a1414';
  roundRectFill(ctx, cx - w / 2, cy, w, h, 2);
  const col = frac > 0.5 ? '#5dff9b' : frac > 0.25 ? '#ffd84d' : '#ff5d5d';
  ctx.fillStyle = col;
  roundRectFill(ctx, cx - w / 2, cy, w * clamp(frac, 0, 1), h, 2);
}

function hexPath(ctx, x, y, r) {
  ctx.beginPath();
  for (let i = 0; i < 6; i++) {
    const a = i * TAU / 6 + Math.PI / 6;
    const px = x + Math.cos(a) * r, py = y + Math.sin(a) * r;
    if (i === 0) ctx.moveTo(px, py); else ctx.lineTo(px, py);
  }
  ctx.closePath();
}

// local tiny helpers
function roundEllipse(ctx, x, y, rx, ry) {
  ctx.beginPath();
  ctx.ellipse(x, y, rx, ry, 0, 0, TAU);
}
function roundRectFill(ctx, x, y, w, h, r) {
  const rr = Math.min(r, w / 2, h / 2);
  ctx.beginPath();
  ctx.moveTo(x + rr, y);
  ctx.arcTo(x + w, y, x + w, y + h, rr);
  ctx.arcTo(x + w, y + h, x, y + h, rr);
  ctx.arcTo(x, y + h, x, y, rr);
  ctx.arcTo(x, y, x + w, y, rr);
  ctx.closePath();
  ctx.fill();
}
