// towers.js — tower types, targeting modes, projectiles & rendering

import { cellCenter } from './utils.js';
import { TAU, clamp, dist2, angleDelta, rotateToward } from './utils.js';

export const MAX_LEVEL = 3;
export const TARGET_MODES = ['first', 'last', 'strongest', 'closest'];

export const TOWER_TYPES = {
  pulse: {
    label: 'PULSE', color: '#5fe3ff', accent: '#0a2a33', kind: 'projectile', projSpeed: 560, cost: 70,
    blurb: 'fast single-target',
    levels: [
      { range: 138, damage: 9,  fireInterval: 0.38 },
      { range: 156, damage: 16, fireInterval: 0.31 },
      { range: 178, damage: 27, fireInterval: 0.25 },
    ],
  },
  beam: {
    label: 'BEAM', color: '#ff5db0', accent: '#33091f', kind: 'beam', cost: 120,
    blurb: 'continuous laser',
    levels: [
      { range: 156, dps: 24 },
      { range: 176, dps: 42 },
      { range: 200, dps: 68 },
    ],
  },
  splash: {
    label: 'SPLASH', color: '#ff9b3d', accent: '#331e08', kind: 'splash', projSpeed: 300, cost: 95,
    blurb: 'lobbed AoE + knockback',
    levels: [
      { range: 150, damage: 18, fireInterval: 1.15, splashRadius: 52, knockback: 26 },
      { range: 165, damage: 30, fireInterval: 1.00, splashRadius: 62, knockback: 34 },
      { range: 182, damage: 48, fireInterval: 0.85, splashRadius: 74, knockback: 44 },
    ],
  },
  slow: {
    label: 'SLOW', color: '#b06bff', accent: '#1e0d33', kind: 'aura', cost: 85,
    blurb: 'cryo field, no dmg',
    levels: [
      { range: 120, slow: 0.60 },
      { range: 138, slow: 0.55 },
      { range: 158, slow: 0.50 },
    ],
  },
};

// rising upgrade cost curve
export function upgradeCost(cfg, currentLevel) {
  return Math.round(cfg.cost * (currentLevel === 1 ? 0.85 : 1.5));
}

export class Tower {
  constructor(type, gx, gy) {
    const cfg = TOWER_TYPES[type];
    this.type = type;
    this.cfg = cfg;
    this.gx = gx; this.gy = gy;
    const c = cellCenter(gx, gy);
    this.x = c.x; this.y = c.y;
    this.level = 1;
    this.invested = cfg.cost;
    this.kills = 0;
    this.damageDealt = 0;
    this.shotsFired = 0;
    this.shotsHit = 0;
    this.mode = 'first';
    this.angle = -Math.PI / 2;
    this.prevAngle = this.angle;
    this.cooldown = 0;
    this.target = null;
    this.firing = 0;          // beam/aura visual strength 0..1
    this.idle = Math.random() * TAU;
    this.muzzleFlash = 0;
    this.pulse = Math.random() * TAU; // aura rotation phase
  }

  get stats() { return this.cfg.levels[this.level - 1]; }
  get maxed() { return this.level >= MAX_LEVEL; }

  acquireTarget(enemies) {
    const r2 = this.stats.range * this.stats.range;
    let best = null;
    for (const e of enemies) {
      if (!e.alive) continue;
      if (dist2(this.x, this.y, e.x, e.y) > r2) continue;
      if (!best) { best = e; continue; }
      switch (this.mode) {
        case 'first': if (e.dist > best.dist) best = e; break;
        case 'last': if (e.dist < best.dist) best = e; break;
        case 'strongest': if (e.hp > best.hp) best = e; break;
        case 'closest':
          if (dist2(this.x, this.y, e.x, e.y) < dist2(this.x, this.y, best.x, best.y)) best = e;
          break;
      }
    }
    return best;
  }

  update(dt, time, enemies, spawnProjectile, applyDamage, applySlow) {
    this.prevAngle = this.angle;
    this.idle += dt;
    this.pulse += dt;
    if (this.muzzleFlash > 0) this.muzzleFlash -= dt;

    const stats = this.stats;

    if (this.cfg.kind === 'aura') {
      // cryo field: continuously slow all enemies in range (no damage)
      const r2 = stats.range * stats.range;
      let any = false;
      for (const e of enemies) {
        if (!e.alive) continue;
        if (dist2(this.x, this.y, e.x, e.y) <= r2) { applySlow(e, stats.slow, 0.4); any = true; }
      }
      this.firing = any ? clamp(this.firing + dt * 4, 0, 1) : clamp(this.firing - dt * 3, 0, 1);
      return;
    }

    this.target = this.acquireTarget(enemies);
    if (this.target) {
      const desired = Math.atan2(this.target.y - this.y, this.target.x - this.x);
      this.angle = rotateToward(this.angle, desired, 10 * dt);
      if (this.cfg.kind === 'beam') {
        applyDamage(this, this.target, stats.dps * dt);
        this.firing = clamp(this.firing + dt * 6, 0, 1);
      } else { // projectile / splash
        this.cooldown -= dt;
        if (this.cooldown <= 0 && Math.abs(angleDelta(this.angle, desired)) < 0.4) {
          spawnProjectile(this, this.target);
          this.cooldown = stats.fireInterval;
          this.muzzleFlash = 0.08;
        }
        this.firing = clamp(this.firing - dt * 6, 0, 1);
      }
    } else {
      this.firing = clamp(this.firing - dt * 6, 0, 1);
      this.angle += Math.sin(this.idle * 0.6) * 0.15 * dt * 6; // idle sweep
    }
  }

  renderAngle(alpha) {
    return this.prevAngle + (this.angle - this.prevAngle) * alpha;
  }
}

export class Projectile {
  constructor(tower, target) {
    const stats = tower.stats;
    const cfg = tower.cfg;
    this.owner = tower;
    this.kind = cfg.kind; // 'projectile' or 'splash'
    this.x = tower.x + Math.cos(tower.angle) * 18;
    this.y = tower.y + Math.sin(tower.angle) * 18;
    this.prevX = this.x; this.prevY = this.y;
    this.target = target;
    this.tx = target.x; this.ty = target.y;
    this.speed = cfg.projSpeed;
    this.damage = stats.damage;
    this.color = cfg.color;
    this.life = 2.2;
    this.dead = false;
    this.trail = [];
    if (this.kind === 'splash') {
      this.splash = stats.splashRadius;
      this.knockback = stats.knockback;
      this.startDist = Math.max(1, Math.hypot(this.tx - this.x, this.ty - this.y));
      this.traveled = 0;
    }
  }

  update(dt, onHit) {
    this.prevX = this.x; this.prevY = this.y;
    this.life -= dt;
    if (this.kind !== 'splash' && this.target && this.target.alive) {
      this.tx = this.target.x; this.ty = this.target.y;
    }
    const dx = this.tx - this.x, dy = this.ty - this.y;
    const d = Math.hypot(dx, dy) || 1;
    const step = this.speed * dt;
    this.trail.push({ x: this.x, y: this.y });
    if (this.trail.length > 6) this.trail.shift();

    const hitR = this.kind === 'splash' ? step : (this.target ? this.target.radius : 4);
    if (d <= step + hitR) {
      this.x = this.tx; this.y = this.ty;
      onHit(this, this.x, this.y);
      this.dead = true;
      return;
    }
    this.x += (dx / d) * step;
    this.y += (dy / d) * step;
    if (this.kind === 'splash') this.traveled += step;
    if (this.life <= 0) {
      if (this.kind === 'splash') onHit(this, this.x, this.y); // explode on expiry too
      this.dead = true;
    }
  }

  // visual arc height for lobbed splash shots
  arcHeight() {
    if (this.kind !== 'splash') return 0;
    const p = clamp(this.traveled / this.startDist, 0, 1);
    return Math.sin(p * Math.PI) * 46;
  }

  renderPos(alpha) {
    return { x: this.prevX + (this.x - this.prevX) * alpha, y: this.prevY + (this.y - this.prevY) * alpha };
  }
}

export function drawTower(ctx, t, alpha, time) {
  const ang = t.renderAngle(alpha);
  const lv = t.level;
  ctx.save();
  ctx.translate(t.x, t.y);

  // level aura glow (cooler each level)
  if (lv >= 2) {
    ctx.fillStyle = t.cfg.color;
    ctx.globalAlpha = 0.10 + lv * 0.04;
    ctx.beginPath(); ctx.arc(0, 0, 24 + lv * 2, 0, TAU); ctx.fill();
    ctx.globalAlpha = 1;
  }

  // base solder pad
  ctx.fillStyle = '#0b1410';
  ctx.strokeStyle = 'rgba(120,255,210,0.25)';
  ctx.lineWidth = 1;
  pad(ctx, 22); ctx.fill(); ctx.stroke();

  // chip body (grows with level)
  const cfg = t.cfg;
  const bodyR = 13 + (lv - 1) * 2.5;
  ctx.save();
  ctx.fillStyle = '#101a17';
  ctx.strokeStyle = cfg.color;
  ctx.lineWidth = 1.5 + (lv - 1) * 0.5;
  roundRectPath(ctx, -bodyR, -bodyR, bodyR * 2, bodyR * 2, 4);
  ctx.fill(); ctx.stroke();
  // chip pins — more pins at higher level
  ctx.fillStyle = 'rgba(200,230,220,0.5)';
  const pinRows = 1 + (lv - 1); // 1..3
  for (let i = 0; i < pinRows * 2 + 1; i++) {
    const oy = (i - pinRows) * 6;
    ctx.fillRect(-bodyR - 3, oy - 1.5, 3, 3);
    ctx.fillRect(bodyR, oy - 1.5, 3, 3);
  }
  ctx.restore();

  // level pips (top-left of chip)
  ctx.save();
  for (let i = 0; i < MAX_LEVEL; i++) {
    ctx.fillStyle = i < lv ? cfg.color : 'rgba(120,130,135,0.3)';
    ctx.beginPath(); ctx.arc(-bodyR + 3 + i * 4, -bodyR + 3, 1.5, 0, TAU); ctx.fill();
  }
  ctx.restore();

  // turret / emitter / aura
  if (cfg.kind === 'aura') {
    drawSlowEmitter(ctx, t, time);
  } else {
    ctx.rotate(ang);
    if (cfg.kind === 'projectile') drawPulseEmitter(ctx, t);
    else if (cfg.kind === 'beam') drawBeamEmitter(ctx, t);
    else if (cfg.kind === 'splash') drawSplashEmitter(ctx, t);
  }
  ctx.restore();
}

function drawPulseEmitter(ctx, t) {
  const cfg = t.cfg;
  const len = 16 + (t.level - 1) * 3;
  ctx.fillStyle = cfg.accent;
  ctx.strokeStyle = cfg.color;
  ctx.lineWidth = 2;
  roundRectPath(ctx, -3, -4, len, 8, 3);
  ctx.fill(); ctx.stroke();
  if (t.muzzleFlash > 0) {
    ctx.fillStyle = cfg.color;
    ctx.shadowColor = cfg.color; ctx.shadowBlur = 12;
    ctx.beginPath(); ctx.arc(len - 2, 0, 4 + t.muzzleFlash * 30, 0, TAU); ctx.fill();
    ctx.shadowBlur = 0;
  }
}

function drawBeamEmitter(ctx, t) {
  const cfg = t.cfg;
  const len = 14 + (t.level - 1) * 2;
  ctx.fillStyle = cfg.accent;
  ctx.strokeStyle = cfg.color;
  ctx.lineWidth = 2;
  ctx.beginPath(); ctx.moveTo(2, -7); ctx.lineTo(len, 0); ctx.lineTo(2, 7); ctx.closePath();
  ctx.fill(); ctx.stroke();
  ctx.fillStyle = cfg.color;
  ctx.shadowColor = cfg.color; ctx.shadowBlur = t.firing * 16;
  ctx.beginPath(); ctx.arc(len - 1, 0, 2.5 + t.firing * 2 + (t.level - 1), 0, TAU); ctx.fill();
  ctx.shadowBlur = 0;
}

function drawSplashEmitter(ctx, t) {
  const cfg = t.cfg;
  const len = 15 + (t.level - 1) * 2;
  // mortar tube
  ctx.fillStyle = cfg.accent;
  ctx.strokeStyle = cfg.color;
  ctx.lineWidth = 2;
  roundRectPath(ctx, -4, -5, len, 10, 3);
  ctx.fill(); ctx.stroke();
  ctx.fillStyle = cfg.color;
  ctx.beginPath(); ctx.arc(len - 2, 0, 3 + (t.level - 1), 0, TAU); ctx.fill();
  if (t.muzzleFlash > 0) {
    ctx.fillStyle = '#fff';
    ctx.globalAlpha = clamp(t.muzzleFlash / 0.08, 0, 1);
    ctx.beginPath(); ctx.arc(len - 2, 0, 6, 0, TAU); ctx.fill();
    ctx.globalAlpha = 1;
  }
}

function drawSlowEmitter(ctx, t, time) {
  const cfg = t.cfg;
  const r = t.stats.range;
  // frosty aura field
  ctx.save();
  ctx.globalAlpha = 0.10 + t.firing * 0.10;
  const g = ctx.createRadialGradient(0, 0, 4, 0, 0, r);
  g.addColorStop(0, cfg.color);
  g.addColorStop(1, 'rgba(176,107,255,0)');
  ctx.fillStyle = g;
  ctx.beginPath(); ctx.arc(0, 0, r, 0, TAU); ctx.fill();
  ctx.restore();
  // rotating crystal emitter
  ctx.save();
  ctx.rotate(t.pulse * 0.8);
  ctx.strokeStyle = cfg.color;
  ctx.fillStyle = cfg.accent;
  ctx.lineWidth = 2;
  ctx.shadowColor = cfg.color; ctx.shadowBlur = 8 + t.firing * 8;
  for (let i = 0; i < 3; i++) {
    ctx.rotate(TAU / 3);
    ctx.beginPath();
    ctx.moveTo(0, -6); ctx.lineTo(11 + (t.level - 1) * 2, 0); ctx.lineTo(0, 6); ctx.closePath();
    ctx.fill(); ctx.stroke();
  }
  ctx.shadowBlur = 0;
  ctx.restore();
  // central frost core
  ctx.fillStyle = '#e6d6ff';
  ctx.beginPath(); ctx.arc(0, 0, 3 + t.level, 0, TAU); ctx.fill();
}

// ---- small shape helpers ----
function pad(ctx, r) { ctx.beginPath(); ctx.arc(0, 0, r, 0, TAU); }
function roundRectPath(ctx, x, y, w, h, r) {
  const rr = Math.min(r, w / 2, h / 2);
  ctx.beginPath();
  ctx.moveTo(x + rr, y);
  ctx.arcTo(x + w, y, x + w, y + h, rr);
  ctx.arcTo(x + w, y + h, x, y + h, rr);
  ctx.arcTo(x, y + h, x, y, rr);
  ctx.arcTo(x, y, x + w, y, rr);
  ctx.closePath();
}
