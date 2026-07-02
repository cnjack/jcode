// game.js — orchestrates state, simulation, economy, phases, rendering & input

import { LOGICAL_W, LOGICAL_H, cellAt, dist2, TAU } from './utils.js';
import { PADS, getPad, resetPads, loadMap, MAPS } from './map.js';
import { Enemy, drawEnemy } from './enemies.js';
import { Tower, Projectile, TOWER_TYPES, upgradeCost, MAX_LEVEL, drawTower } from './towers.js';
import { getWave, buildQueue, hpScale, speedScale, TOTAL_WAVES, getDifficulty, DIFFICULTIES } from './waves.js';
import { AudioEngine } from './audio.js';
import {
  drawBackground, drawTrace, drawPads, drawSpawn, drawCpu, drawBeam,
  particles, burst, updateParticles, drawParticles,
  floaters, spawnFloater, updateFloaters, drawFloaters,
  rings, spawnRing, updateRings, drawRings, drawVignette,
} from './render.js';
import {
  drawHud, drawPlacementMenu, drawSelectedPanel, drawBossBar, drawBanner,
  drawMenu, drawGameOver, drawVictory, drawFooter, drawSettings, drawHelp, drawStats,
  hitTest,
} from './ui.js';

const STEP = 1 / 60;
const SELECT_RADIUS = 26;
const BEST_KEY = 'cd_bests_v1';

export class Game {
  constructor() {
    this.audio = new AudioEngine();
    this.pads = PADS;
    this.mapId = 'serpentine';
    this.difficulty = 'normal';
    this.endless = false;
    this.ff = false;            // fast-forwarding (suppress sfx)
    this.showSettings = false;
    this.showHelp = false;
    this.musicTimer = 1.5;
    this.newBest = false;
    this.reset();
  }

  get diff() { return getDifficulty(this.difficulty); }

  reset() {
    const d = this.diff;
    this.gold = d.gold;
    this.maxLives = d.lives;
    this.lives = d.lives;
    this.wave = 0;
    this.score = 0;
    this.phase = 'menu';
    this.endless = false;
    this.newBest = false;
    this.autoStart = false;
    this.countdown = 0;
    this.enemies = [];
    this.towers = [];
    this.projectiles = [];
    this.spawnQueue = [];
    this.spawnTimer = 0;
    this.spawnInterval = 1;
    this.hpMul = 1;
    this.spdMul = 1;
    this.banner = null;
    this.activePad = null;
    this.selectedTower = null;
    this.menuHoverType = null;
    this.hover = null;
    this.hoverPad = null;
    this.cpuFlash = 0;
    this.shake = 0;
    this.speed = 1;
    this.paused = false;
    this.time = 0;
    this.mouse = { x: 0, y: 0 };
    // run stats
    this.killed = 0;
    this.leaked = 0;
    this._bossAlive = false;
    resetPads();
    particles.length = 0;
    floaters.length = 0;
    rings.length = 0;
  }

  selectMap(id) {
    if (!MAPS[id]) return false;
    this.mapId = id;
    loadMap(id);
    return true;
  }
  setDifficulty(d) {
    if (!DIFFICULTIES[d]) return false;
    this.difficulty = d;
    if (this.phase === 'menu') this.reset();
    return true;
  }

  startGame() {
    if (this.phase === 'menu') {
      this.audio.ensure();
      this.audio.resume();
      this.phase = 'idle';
      this.countdown = 0;
    }
  }

  continueEndless() {
    if (this.phase !== 'victory') return;
    this.endless = true;
    this.phase = 'idle';
    this.countdown = 4;
  }

  restart() {
    this.reset();
    this.startGame();
  }

  addShake(n) { this.shake = Math.min(20, this.shake + n); }

  // ---------- persistence ----------
  bests() {
    try { return JSON.parse(localStorage.getItem(BEST_KEY) || '{}'); }
    catch (e) { return {}; }
  }
  bestKey() { return this.mapId + ':' + this.difficulty; }
  recordBest() {
    const all = this.bests();
    const k = this.bestKey();
    const prev = all[k] || { wave: 0, score: 0 };
    this.newBest = this.wave > prev.wave || (this.wave === prev.wave && this.score > prev.score);
    if (this.wave > prev.wave || this.score > prev.score) {
      all[k] = { wave: Math.max(prev.wave, this.wave), score: Math.max(prev.score, this.score) };
      try { localStorage.setItem(BEST_KEY, JSON.stringify(all)); } catch (e) { /* ignore */ }
    }
    return this.newBest;
  }
  currentBest() { return this.bests()[this.bestKey()] || { wave: 0, score: 0 }; }

  // ---------- run stats (for end screens) ----------
  runStats() {
    const byType = {};
    let shotsFired = 0, shotsHit = 0;
    for (const id of ['pulse', 'beam', 'splash', 'slow']) byType[id] = { dmg: 0, kills: 0, count: 0 };
    for (const t of this.towers) {
      const s = byType[t.type];
      s.dmg += t.damageDealt; s.kills += t.kills; s.count += 1;
      if (t.cfg.kind === 'projectile' || t.cfg.kind === 'splash') {
        shotsFired += t.shotsFired; shotsHit += t.shotsHit;
      }
    }
    return {
      byType,
      killed: this.killed,
      leaked: this.leaked,
      waves: this.wave,
      score: this.score,
      accuracy: shotsFired > 0 ? shotsHit / shotsFired : 0,
      endless: this.endless,
    };
  }

  // ---------- economy / actions ----------
  addGold(n) { this.gold += n; }

  place(type, gx, gy) {
    const pad = getPad(gx, gy);
    if (!pad || pad.occupied) return false;
    const cfg = TOWER_TYPES[type];
    if (!cfg) return false;
    if (this.gold < cfg.cost) return false;
    this.gold -= cfg.cost;
    const tw = new Tower(type, gx, gy);
    this.towers.push(tw);
    pad.occupied = true;
    burst(pad.x, pad.y, cfg.color, 8, 90);
    if (!this.ff) this.audio.shoot(type);
    return true;
  }

  towerAt(gx, gy) { return this.towers.find((t) => t.gx === gx && t.gy === gy) || null; }

  upgrade(gx, gy) {
    const t = this.towerAt(gx, gy);
    if (!t || t.maxed) return false;
    const cost = upgradeCost(t.cfg, t.level);
    if (this.gold < cost) return false;
    this.gold -= cost;
    t.invested += cost;
    t.level += 1;
    burst(t.x, t.y, t.cfg.color, 14, 120);
    spawnRing(t.x, t.y, 40, t.cfg.color);
    if (!this.ff) this.audio.upgrade();
    return true;
  }

  sell(gx, gy) {
    const t = this.towerAt(gx, gy);
    if (!t) return false;
    const refund = Math.floor(0.7 * t.invested);
    this.gold += refund;
    getPad(t.gx, t.gy).occupied = false;
    burst(t.x, t.y, '#ffd86b', 12, 120);
    if (this.selectedTower === t) this.selectedTower = null;
    this.towers = this.towers.filter((x) => x !== t);
    if (!this.ff) this.audio.sell();
    return true;
  }

  setMode(gx, gy, mode) {
    const t = this.towerAt(gx, gy);
    if (!t) return false;
    if (!['first', 'last', 'strongest', 'closest'].includes(mode)) return false;
    t.mode = mode;
    return true;
  }

  setSpeed(n) { if ([1, 2, 3].includes(n)) this.speed = n; }
  pause(b) { this.paused = !!b; }

  spawnWave() {
    if (this.phase === 'gameover' || this.phase === 'victory') return;
    if (this.wave >= TOTAL_WAVES && !this.endless) return;
    this.wave += 1;
    const w = getWave(this.wave);
    const d = this.diff;
    this.spawnQueue = buildQueue(this.wave, d.countMul);
    this.spawnInterval = w.interval;
    this.hpMul = hpScale(this.wave) * d.hpMul;
    this.spdMul = speedScale(this.wave);
    this.spawnTimer = 0.4;
    this.phase = 'combat';
    this.activePad = null;
    const sub = this.endless ? `ENDLESS WAVE ${this.wave}`
      : this.wave >= 20 ? 'FINAL ASSAULT'
      : (w.boss > 0) ? '⚠ boss incoming'
      : this.wave % 5 === 0 ? 'pressure rising'
      : 'incoming bugs';
    this.banner = { text: 'WAVE ' + this.wave, sub, t: 0, dur: 2.2 };
    if (!this.ff) this.audio.waveStart();
  }

  fastForward(seconds) {
    this.ff = true;
    this.audio.suppress = true;
    const steps = Math.max(0, Math.round(seconds / STEP));
    for (let i = 0; i < steps; i++) this.update(STEP);
    this.ff = false;
    this.audio.suppress = false;
  }

  // ---------- simulation ----------
  update(dt) {
    this.time += dt;
    if (this.banner) { this.banner.t += dt; if (this.banner.t >= this.banner.dur) this.banner = null; }
    if (this.cpuFlash > 0) this.cpuFlash -= dt;
    if (this.shake > 0) this.shake = Math.max(0, this.shake - dt * 30);

    // ambient music scheduler (skipped during fast-forward)
    this.musicTimer -= dt;
    if (this.musicTimer <= 0 && !this.ff) {
      this.audio.musicTick();
      this.musicTimer = 2.4 + Math.random() * 2.2;
    }

    const spawnProj = (tw, tg) => {
      this.projectiles.push(new Projectile(tw, tg));
      if (tw) tw.shotsFired += 1;
      if (!this.ff) this.audio.shoot(tw.type);
    };

    // applyDamage: track damage/kills; AGGREGATE floaters (no per-tick number)
    const applyDamage = (tw, tg, raw) => {
      if (!tg || !tg.alive) return;
      const real = tg.damage(raw);
      if (tw) { tw.damageDealt += real; if (!tg.alive) tw.kills += 1; }
      tg.pendingDmg += real;
      if (tg.dmgTimer <= 0) tg.dmgTimer = 0.3;
    };
    const applySlow = (e, factor, dur) => {
      e.applySlow(factor, dur);
      if (!this.ff && factor < 1) this.audio.shoot('slow');
    };

    if (this.phase === 'combat') {
      // spawning
      if (this.spawnQueue.length) {
        this.spawnTimer -= dt;
        if (this.spawnTimer <= 0) {
          const type = this.spawnQueue.shift();
          this.enemies.push(new Enemy(type, this.hpMul, this.spdMul));
          this.spawnTimer = this.spawnInterval;
          if (type === 'boss' && !this.ff) this.audio.bossSpawn();
        }
      }
      // entities
      for (const e of this.enemies) e.update(dt, this.time);
      for (const t of this.towers) t.update(dt, this.time, this.enemies, spawnProj, applyDamage, applySlow);
      for (const p of this.projectiles) p.update(dt, (proj, hx, hy) => this.onProjectileHit(proj, hx, hy, applyDamage));
      this.projectiles = this.projectiles.filter((p) => !p.dead);

      // boss drone manage
      let bossAlive = false;
      for (const e of this.enemies) if (e.type === 'boss' && e.alive) { bossAlive = true; break; }
      if (bossAlive && !this._bossAlive) this.audio.startDrone();
      else if (!bossAlive && this._bossAlive) this.audio.stopDrone();
      this._bossAlive = bossAlive;

      // resolve dead enemies
      const newSpawns = [];
      for (let i = this.enemies.length - 1; i >= 0; i--) {
        const e = this.enemies[i];
        if (!e.alive) {
          if (e.reachedCore) {
            this.lives -= 1; this.leaked += 1;
            this.cpuFlash = 0.45;
            this.addShake(e.type === 'boss' ? 14 : 5);
            burst(e.x, e.y, '#ff6b6b', 12, 120);
            if (!this.ff) this.audio.leak();
            if (this.lives <= 0) {
              this.lives = 0; this.phase = 'gameover'; this.banner = null;
              this.audio.stopDrone();
              this.recordBest();
              if (!this.ff) this.audio.gameOver();
            }
          } else {
            this.killed += 1;
            this.gold += e.reward;
            this.score += e.scoreVal;
            if (e.type === 'boss') {
              this.addShake(14);
              burst(e.x, e.y, e.color, 40, 220);
              spawnRing(e.x, e.y, 120, e.color);
              this.banner = { text: 'BOSS DOWN', sub: `+${e.reward}g`, t: 0, dur: 1.8 };
              if (!this.ff) this.audio.bossDown();
            } else {
              burst(e.x, e.y, e.color, e.type === 'tank' ? 16 : 12, 150);
              if (!this.ff) this.audio.death();
            }
            if (e.splits > 0) {
              for (let k = 0; k < e.splits; k++) {
                const off = (k - (e.splits - 1) / 2) * 10;
                newSpawns.push(new Enemy('mini', this.hpMul, this.spdMul, Math.max(0, e.dist + off)));
              }
            }
          }
          this.enemies.splice(i, 1);
        }
      }
      for (const s of newSpawns) this.enemies.push(s);

      // wave clear
      if (this.phase === 'combat' && this.spawnQueue.length === 0 && this.enemies.length === 0) {
        this.score += this.wave * 25;
        this.audio.stopDrone();
        this._bossAlive = false;
        if (this.wave >= TOTAL_WAVES && !this.endless) {
          this.phase = 'victory';
          this.recordBest();
          if (!this.ff) this.audio.victory();
        } else {
          this.phase = 'idle';
          this.countdown = this.endless ? 6 : 10;
        }
      }
    } else {
      for (const t of this.towers) t.update(dt, this.time, this.enemies, spawnProj, applyDamage, applySlow);
      if (this.phase === 'idle' && this.autoStart) {
        this.countdown -= dt;
        if (this.countdown <= 0) this.spawnWave();
      }
    }

    updateParticles(dt);
    updateFloaters(dt);
    updateRings(dt);
  }

  onProjectileHit(proj, hx, hy, applyDamage) {
    if (proj.kind === 'splash') {
      const r2 = proj.splash * proj.splash;
      let hits = 0;
      for (const e of this.enemies) {
        if (!e.alive) continue;
        if (dist2(hx, hy, e.x, e.y) <= r2) {
          applyDamage(proj.owner, e, proj.damage);
          e.knockback(proj.knockback);
          hits++;
        }
      }
      if (proj.owner) proj.owner.shotsHit += hits > 0 ? 1 : 0;
      spawnRing(hx, hy, proj.splash, proj.color);
      burst(hx, hy, proj.color, 16, 170);
      this.addShake(3);
      if (!this.ff) this.audio.impact();
    } else {
      if (proj.target && proj.target.alive) {
        applyDamage(proj.owner, proj.target, proj.damage);
        if (proj.owner) proj.owner.shotsHit += 1;
        burst(hx, hy, proj.color, 6, 120);
      }
    }
  }

  // ---------- rendering ----------
  render(ctx, alpha) {
    let ox = 0, oy = 0;
    if (this.shake > 0.2) { ox = (Math.random() - 0.5) * this.shake; oy = (Math.random() - 0.5) * this.shake; }
    ctx.save();
    ctx.translate(ox, oy);

    drawBackground(ctx);
    drawTrace(ctx, this.time);
    drawPads(ctx, PADS, this.hoverPad, this.time);
    drawSpawn(ctx, this.time);
    drawCpu(ctx, this.lives, this.maxLives, this.time, this.cpuFlash);

    if (this.selectedTower) {
      const t = this.selectedTower;
      ctx.save();
      ctx.strokeStyle = 'rgba(125,240,192,0.6)';
      ctx.fillStyle = 'rgba(125,240,192,0.05)';
      ctx.lineWidth = 1.5; ctx.setLineDash([6, 6]);
      ctx.beginPath(); ctx.arc(t.x, t.y, t.stats.range, 0, TAU); ctx.fill(); ctx.stroke();
      ctx.setLineDash([]); ctx.restore();
    }

    for (const t of this.towers) drawTower(ctx, t, alpha, this.time);
    for (const t of this.towers) {
      if (t.cfg.kind === 'beam' && t.target && t.firing > 0.1) drawBeam(ctx, t, t.target, alpha);
    }
    drawRings(ctx);
    for (const e of this.enemies) drawEnemy(ctx, e, alpha, this.time);
    for (const p of this.projectiles) drawProjectile(ctx, p, alpha);
    drawParticles(ctx);
    drawFloaters(ctx);

    ctx.restore();

    drawVignette(ctx, this.cpuFlash);

    drawHud(ctx, this, this.time, this.mouse);
    if (this.activePad) drawPlacementMenu(ctx, this, this.time);
    if (this.selectedTower) drawSelectedPanel(ctx, this, this.time);
    drawBossBar(ctx, this);
    drawBanner(ctx, this, this.time);

    if (this.phase === 'menu') drawMenu(ctx, this, this.time);
    else if (this.phase === 'gameover') { drawGameOver(ctx, this, this.time); drawStats(ctx, this); }
    else if (this.phase === 'victory') { drawVictory(ctx, this, this.time); drawStats(ctx, this); }

    if (this.showHelp) drawHelp(ctx, this, this.time);
    if (this.showSettings) drawSettings(ctx, this, this.time);

    if (this.paused && this.phase !== 'menu' && this.phase !== 'gameover' && this.phase !== 'victory') {
      ctx.save();
      ctx.fillStyle = 'rgba(3,8,7,0.5)';
      ctx.fillRect(0, 0, LOGICAL_W, LOGICAL_H);
      ctx.fillStyle = '#9fffe0';
      ctx.font = `bold 48px -apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif`;
      ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
      ctx.fillText('❚❚ PAUSED', LOGICAL_W / 2, LOGICAL_H / 2);
      ctx.restore();
    }

    drawFooter(ctx);
  }

  // ---------- input ----------
  handleMove(x, y) {
    this.mouse = { x, y };
    this.hoverPad = null;
    const padR = this.touch ? 30 : 22;
    for (const p of PADS) {
      if (!p.occupied && Math.hypot(x - p.x, y - p.y) <= padR) { this.hoverPad = p; break; }
    }
    const h = hitTest(x, y);
    this.hover = h;
    this.menuHoverType = (h && h.startsWith('place_')) ? h.slice(6) : null;
  }

  handleClick(x, y) {
    const h = hitTest(x, y);
    // global toggles available in most phases
    if (h === 'gear') { this.showSettings = !this.showSettings; return true; }
    if (h === 'helpToggle') { this.showHelp = !this.showHelp; return true; }
    if (this.showSettings) {
      if (h === 'mute') { this.audio.setMuted(!this.audio.muted); return true; }
      if (h === 'music') { this.audio.setMusic(!this.audio.musicOn); return true; }
      if (h === 'volBar') {
        // volume set by click x handled in click handler via stored rect
        const r = window.__volRect;
        if (r) { this.audio.setVolume(Math.max(0, Math.min(1, (x - r.x) / r.w))); }
        return true;
      }
      if (h === 'settingsPanel') return true;
      if (h !== 'gear') { this.showSettings = false; }
    }
    if (this.showHelp) { if (h !== 'helpToggle') { this.showHelp = false; return true; } }

    if (this.phase === 'menu') {
      if (h && h.startsWith('map_')) { this.selectMap(h.slice(4)); return true; }
      if (h && h.startsWith('diff_')) { this.setDifficulty(h.slice(5)); return true; }
      if (h === 'startGame') { this.startGame(); return true; }
      if (h === 'helpBtn') { this.showHelp = !this.showHelp; return true; }
      return true;
    }
    if (this.phase === 'gameover' || this.phase === 'victory') {
      if (h === 'restart') { this.restart(); return true; }
      if (h === 'continue') { this.continueEndless(); return true; }
      if (h === 'menu') { this.reset(); return true; }
      return true;
    }
    // placement menu
    if (this.activePad) {
      if (h && h.startsWith('place_')) { this.place(h.slice(6), this.activePad.gx, this.activePad.gy); this.activePad = null; return true; }
      if (h === 'placePanel') return true;
      this.activePad = null; return true;
    }
    if (this.selectedTower) {
      if (h === 'upgrade') { this.upgrade(this.selectedTower.gx, this.selectedTower.gy); return true; }
      if (h === 'sell') { this.sell(this.selectedTower.gx, this.selectedTower.gy); return true; }
      if (h && h.startsWith('mode_')) { this.setMode(this.selectedTower.gx, this.selectedTower.gy, h.slice(5)); return true; }
      if (h === 'selPanel') return true;
    }
    if (h === 'startWave') { if (this.phase === 'idle') this.spawnWave(); return true; }
    if (h === 'autoToggle') { this.autoStart = !this.autoStart; return true; }
    if (h === 'speed1' || h === 'speed2' || h === 'speed3') { this.speed = Number(h.slice(5)); return true; }
    if (h === 'pause') { this.paused = !this.paused; return true; }

    const cell = cellAt(x, y);
    const pad = getPad(cell.gx, cell.gy);
    if (pad && !pad.occupied) { this.activePad = pad; this.selectedTower = null; return true; }
    let clickedTower = null;
    const selR = this.touch ? 34 : SELECT_RADIUS;
    for (const t of this.towers) {
      if (dist2(x, y, t.x, t.y) <= selR * selR) { clickedTower = t; break; }
    }
    if (clickedTower) { this.selectedTower = clickedTower; return true; }
    this.selectedTower = null;
    return false;
  }
}

function drawProjectile(ctx, p, alpha) {
  const pos = p.renderPos(alpha);
  if (p.trail.length > 1) {
    ctx.save();
    ctx.globalCompositeOperation = 'lighter';
    ctx.strokeStyle = p.color; ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(p.trail[0].x, p.trail[0].y);
    for (let i = 1; i < p.trail.length; i++) ctx.lineTo(p.trail[i].x, p.trail[i].y);
    ctx.lineTo(pos.x, pos.y);
    ctx.globalAlpha = 0.4; ctx.stroke();
    ctx.restore();
  }
  if (p.kind === 'splash') {
    const ah = p.arcHeight();
    ctx.save();
    ctx.globalAlpha = 0.3; ctx.fillStyle = '#000';
    ctx.beginPath(); ctx.ellipse(pos.x, pos.y, 4, 1.6, 0, 0, TAU); ctx.fill();
    ctx.restore();
    ctx.save();
    ctx.shadowColor = p.color; ctx.shadowBlur = 12; ctx.fillStyle = p.color;
    ctx.beginPath(); ctx.arc(pos.x, pos.y - ah, 4.5, 0, TAU); ctx.fill();
    ctx.fillStyle = '#fff'; ctx.beginPath(); ctx.arc(pos.x, pos.y - ah, 2, 0, TAU); ctx.fill();
    ctx.restore();
    return;
  }
  ctx.save();
  ctx.shadowColor = p.color; ctx.shadowBlur = 12; ctx.fillStyle = p.color;
  ctx.beginPath(); ctx.arc(pos.x, pos.y, 3.5, 0, TAU); ctx.fill();
  ctx.fillStyle = '#ffffff'; ctx.beginPath(); ctx.arc(pos.x, pos.y, 1.6, 0, TAU); ctx.fill();
  ctx.restore();
}
