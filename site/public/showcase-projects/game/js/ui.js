// ui.js — HUD, placement menu, selected-tower panel, banners, overlays & hit-testing

import { LOGICAL_W, LOGICAL_H, FONT_MONO, FONT_SANS, clamp } from './utils.js';
import { TOWER_TYPES, MAX_LEVEL, TARGET_MODES, upgradeCost } from './towers.js';
import { TOTAL_WAVES, getWave, DIFFICULTIES, DIFFICULTY_ORDER } from './waves.js';
import { ENEMY_TYPES, ENEMY_META } from './enemies.js';
import { MAPS, MAP_ORDER } from './map.js';

// Registry of interactive rectangles drawn this frame: name -> {x,y,w,h,enabled,priority,seq}
const hit = new Map();
let _seq = 0;
export function resetHits() { hit.clear(); _seq = 0; }

// BUG FIX (critical): priority-based hit test. Buttons (priority 1) beat full-screen
// backdrops like 'cancel' (priority 0); ties broken by last drawn (overlays beat HUD).
export function hitTest(x, y) {
  let best = null;
  for (const [name, r] of hit) {
    if (x >= r.x && x <= r.x + r.w && y >= r.y && y <= r.y + r.h) {
      if (!best || r.priority > best.priority || (r.priority === best.priority && r.seq > best.seq)) {
        best = { name, priority: r.priority, seq: r.seq };
      }
    }
  }
  return best ? best.name : null;
}

function reg(ctx, name, x, y, w, h, label, opts = {}) {
  const enabled = opts.enabled !== false;
  const hover = opts.hover;
  const priority = opts.priority !== undefined ? opts.priority : 1;
  hit.set(name, { x, y, w, h, enabled, priority, seq: _seq++ });
  if (label === null) return; // region-only (no drawn button)
  ctx.save();
  ctx.fillStyle = enabled
    ? (hover ? 'rgba(70,224,176,0.28)' : 'rgba(20,60,48,0.7)')
    : 'rgba(40,40,44,0.6)';
  ctx.strokeStyle = enabled
    ? (hover ? '#7df0c0' : 'rgba(120,255,210,0.55)')
    : 'rgba(120,130,135,0.3)';
  ctx.lineWidth = hover ? 2 : 1;
  roundRect(ctx, x, y, w, h, 6); ctx.fill(); ctx.stroke();
  ctx.fillStyle = enabled ? '#eafff5' : 'rgba(180,190,195,0.5)';
  ctx.font = `bold ${opts.fs || 14}px ${FONT_MONO}`;
  ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
  ctx.fillText(label, x + w / 2, y + h / 2 + 1);
  ctx.restore();
}

// ---------------- HUD ----------------
export function drawHud(ctx, game, time, mouse) {
  resetHits();
  const y = 12;
  stat(ctx, 24, y, 'GOLD', String(Math.floor(game.gold)), '#ffd86b');
  stat(ctx, 160, y, 'CORE', `${game.lives}/${game.maxLives}`, game.lives > 5 ? '#7df0c0' : '#ff6b6b');
  stat(ctx, 300, y, 'WAVE', `${game.wave}${game.endless ? '' : '/' + TOTAL_WAVES}`, '#6fd0ff');
  stat(ctx, 430, y, 'SCORE', String(game.score), '#eafff5');

  // speed / pause cluster
  for (const s of [1, 2, 3]) {
    const on = game.speed === s;
    reg(ctx, 'speed' + s, 560 + (s - 1) * 46, 14, 42, 36, s + 'x',
      { hover: game.hover === 'speed' + s, fs: 14 });
    if (on) { ctx.save(); ctx.strokeStyle = '#7df0c0'; ctx.lineWidth = 2; roundRect(ctx, 560 + (s - 1) * 46, 14, 42, 36, 6); ctx.stroke(); ctx.restore(); }
  }
  reg(ctx, 'pause', 560 + 3 * 46 + 6, 14, 56, 36, game.paused ? '▶ PLAY' : '❚❚ PAUSE',
    { hover: game.hover === 'pause', fs: 11 });

  // wave preview (idle/countdown)
  if (game.phase === 'idle') drawWavePreview(ctx, game);

  // right-side controls
  const canStart = game.phase === 'idle';
  reg(ctx, 'startWave', LOGICAL_W - 290, 14, 150, 36,
    game.phase === 'combat' ? 'IN COMBAT' : `START WAVE ▸`,
    { enabled: canStart, hover: game.hover === 'startWave', fs: 13 });
  reg(ctx, 'autoToggle', LOGICAL_W - 130, 14, 60, 36,
    game.autoStart ? 'AUTO●' : 'AUTO○',
    { hover: game.hover === 'autoToggle', fs: 12 });

  // settings gear + help — far right, always available
  reg(ctx, 'helpToggle', LOGICAL_W - 64, 14, 26, 36, '?',
    { hover: game.hover === 'helpToggle', fs: 16, priority: 3 });
  reg(ctx, 'gear', LOGICAL_W - 34, 14, 26, 36, '⚙',
    { hover: game.hover === 'gear', fs: 16, priority: 3 });

  if (game.phase === 'idle') {
    ctx.fillStyle = 'rgba(180,220,210,0.7)';
    ctx.font = `11px ${FONT_MONO}`;
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
    const txt = game.autoStart ? `auto-start ${Math.ceil(game.countdown)}s` : `next wave ready`;
    ctx.fillText(txt, LOGICAL_W - 205, 60);
  }
}

function stat(ctx, x, y, label, value, color) {
  ctx.fillStyle = 'rgba(140,200,190,0.6)';
  ctx.font = `11px ${FONT_MONO}`;
  ctx.textAlign = 'left'; ctx.textBaseline = 'top';
  ctx.fillText(label, x, y);
  ctx.fillStyle = color;
  ctx.font = `bold 22px ${FONT_MONO}`;
  ctx.fillText(value, x, y + 14);
}

// small strip showing the NEXT wave's composition
function drawWavePreview(ctx, game) {
  const next = Math.min(game.wave + 1, TOTAL_WAVES);
  const w = getWave(next);
  const entries = [
    ['bug', w.bugs], ['spark', w.sparks], ['tank', w.tanks], ['swarm', w.swarms], ['boss', w.boss],
  ].filter((e) => e[1] > 0);
  let cx = 560;
  const cy = 58;
  ctx.fillStyle = 'rgba(180,220,210,0.55)';
  ctx.font = `10px ${FONT_MONO}`;
  ctx.textAlign = 'left'; ctx.textBaseline = 'middle';
  ctx.fillText('NEXT', cx - 34, cy + 1);
  for (const [type, n] of entries) {
    const col = ENEMY_TYPES[type].color;
    ctx.fillStyle = col;
    ctx.beginPath(); ctx.arc(cx, cy, 5, 0, Math.PI * 2); ctx.fill();
    ctx.fillStyle = '#cfeae2';
    ctx.font = `bold 11px ${FONT_MONO}`;
    ctx.fillText('×' + n, cx + 8, cy + 1);
    cx += 8 + ctx.measureText('×' + n).width + 14;
  }
}

// ---------------- placement menu ----------------
const PLACE_TYPES = ['pulse', 'beam', 'splash', 'slow'];

export function drawPlacementMenu(ctx, game, time) {
  const pad = game.activePad;
  if (!pad) return;
  const prevType = game.menuHoverType || PLACE_TYPES[0];
  const cfg = TOWER_TYPES[prevType];
  // range preview
  ctx.save();
  ctx.strokeStyle = 'rgba(120,255,210,0.35)';
  ctx.fillStyle = 'rgba(120,255,210,0.06)';
  ctx.lineWidth = 1.5; ctx.setLineDash([6, 6]);
  ctx.beginPath(); ctx.arc(pad.x, pad.y, cfg.levels[0].range, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
  ctx.setLineDash([]); ctx.restore();

  const pw = 176, ph = 28 + PLACE_TYPES.length * 28 + 10;
  let px = pad.x - pw / 2;
  let py = pad.y - 50 - ph;
  px = clamp(px, 8, LOGICAL_W - pw - 8);
  if (py < 64) py = pad.y + 50;

  ctx.save();
  ctx.fillStyle = 'rgba(8,18,16,0.95)';
  ctx.strokeStyle = 'rgba(120,255,210,0.6)';
  ctx.lineWidth = 1.5;
  roundRect(ctx, px, py, pw, ph, 8); ctx.fill(); ctx.stroke();
  ctx.fillStyle = '#9fffe0';
  ctx.font = `bold 10px ${FONT_MONO}`;
  ctx.textAlign = 'left'; ctx.textBaseline = 'top';
  ctx.fillText('SOLDER CHIP', px + 10, py + 9);
  ctx.restore();

  PLACE_TYPES.forEach((t, i) => {
    const c = TOWER_TYPES[t];
    const afford = game.gold >= c.cost;
    const bx = px + 10, by = py + 26 + i * 28, bw = pw - 20, bh = 24;
    reg(ctx, 'place_' + t, bx, by, bw, bh, `${c.label} ${c.cost}g`,
      { enabled: afford, hover: game.menuHoverType === t, fs: 12, priority: 2 });
  });
  reg(ctx, 'placePanel', px, py, pw, ph, null, { priority: 1 });
  reg(ctx, 'cancel', 0, 0, LOGICAL_W, LOGICAL_H, null, { priority: 0 });
}

// ---------------- selected tower panel ----------------
export function drawSelectedPanel(ctx, game, time) {
  const t = game.selectedTower;
  if (!t) return;
  const cfg = t.cfg;
  const st = t.stats;
  const pw = 252, ph = 178;
  const px = 14, py = LOGICAL_H - ph - 22;

  ctx.save();
  ctx.fillStyle = 'rgba(8,18,16,0.95)';
  ctx.strokeStyle = cfg.color;
  ctx.lineWidth = 1.5;
  roundRect(ctx, px, py, pw, ph, 8); ctx.fill(); ctx.stroke();
  ctx.restore();

  ctx.fillStyle = cfg.color;
  ctx.font = `bold 13px ${FONT_MONO}`;
  ctx.textAlign = 'left'; ctx.textBaseline = 'top';
  ctx.fillText(`${cfg.label} CHIP`, px + 12, py + 10);
  ctx.fillStyle = '#cfeae2';
  ctx.font = `bold 11px ${FONT_MONO}`;
  ctx.fillText(`Lv ${t.level}/${MAX_LEVEL}`, px + pw - 60, py + 12);

  ctx.fillStyle = 'rgba(200,240,230,0.85)';
  ctx.font = `11px ${FONT_MONO}`;
  let statTxt;
  if (t.type === 'beam') statTxt = `DPS ${st.dps}   RNG ${Math.round(st.range)}`;
  else if (t.type === 'slow') statTxt = `SLOW ${Math.round((1 - st.slow) * 100)}%   RNG ${Math.round(st.range)}`;
  else if (t.type === 'splash') statTxt = `DMG ${st.damage}   SPLASH ${Math.round(st.splashRadius)}   RNG ${Math.round(st.range)}`;
  else statTxt = `DMG ${st.damage}   RATE ${(1 / st.fireInterval).toFixed(1)}/s   RNG ${Math.round(st.range)}`;
  ctx.fillText(statTxt, px + 12, py + 32);

  ctx.fillStyle = 'rgba(180,220,210,0.7)';
  ctx.fillText(`KILLS ${t.kills}   DMG DEALT ${Math.round(t.damageDealt)}`, px + 12, py + 50);
  ctx.fillText(`MODE ${t.mode.toUpperCase()}`, px + 12, py + 66);

  const maxed = t.maxed;
  const upCost = maxed ? 0 : upgradeCost(cfg, t.level);
  const affordUp = !maxed && game.gold >= upCost;
  const refund = Math.floor(0.7 * t.invested);
  reg(ctx, 'upgrade', px + 12, py + 86, 110, 30,
    maxed ? 'MAX LEVEL' : `UPGRADE ${upCost}g`,
    { enabled: affordUp, hover: game.hover === 'upgrade', fs: 11, priority: 2 });
  reg(ctx, 'sell', px + 130, py + 86, 110, 30, `SELL ${refund}g`,
    { hover: game.hover === 'sell', fs: 11, priority: 2 });

  ctx.fillStyle = 'rgba(180,220,210,0.6)';
  ctx.font = `9px ${FONT_MONO}`;
  ctx.fillText('TARGET', px + 12, py + 122);
  const mw = (pw - 24 - 9) / 4;
  const labels = { first: 'FIRST', last: 'LAST', strongest: 'STRG', closest: 'CLOSE' };
  TARGET_MODES.forEach((m, i) => {
    const bx = px + 12 + i * (mw + 3), by = py + 134, bh = 28;
    const on = t.mode === m;
    ctx.save();
    ctx.fillStyle = on ? 'rgba(70,224,176,0.35)' : 'rgba(20,60,48,0.6)';
    ctx.strokeStyle = on ? '#7df0c0' : 'rgba(120,255,210,0.4)';
    ctx.lineWidth = on ? 2 : 1;
    roundRect(ctx, bx, by, mw, bh, 5); ctx.fill(); ctx.stroke();
    ctx.fillStyle = '#eafff5';
    ctx.font = `bold 10px ${FONT_MONO}`;
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
    ctx.fillText(labels[m], bx + mw / 2, by + bh / 2 + 1);
    ctx.restore();
    reg(ctx, 'mode_' + m, bx, by, mw, bh, null, { priority: 2 });
  });
  reg(ctx, 'selPanel', px, py, pw, ph, null, { priority: 1 });
}

// ---------------- boss HP bar ----------------
export function drawBossBar(ctx, game) {
  let boss = null;
  for (const e of game.enemies) { if (e.type === 'boss' && e.alive) { boss = e; break; } }
  if (!boss) return;
  const bw = 520, bh = 16;
  const bx = (LOGICAL_W - bw) / 2, by = 64;
  ctx.save();
  ctx.fillStyle = 'rgba(0,0,0,0.6)';
  roundRect(ctx, bx - 2, by - 2, bw + 4, bh + 4, 4); ctx.fill();
  ctx.fillStyle = '#2a0816';
  roundRect(ctx, bx, by, bw, bh, 3); ctx.fill();
  const frac = clamp(boss.hp / boss.maxHp, 0, 1);
  ctx.fillStyle = '#ff5d8f';
  ctx.shadowColor = '#ff5d8f'; ctx.shadowBlur = 12;
  roundRect(ctx, bx, by, bw * frac, bh, 3); ctx.fill();
  ctx.shadowBlur = 0;
  ctx.fillStyle = '#fff';
  ctx.font = `bold 11px ${FONT_MONO}`;
  ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
  ctx.fillText(`BOSS  ${Math.ceil(boss.hp)}/${boss.maxHp}`, LOGICAL_W / 2, by + bh / 2 + 1);
  ctx.restore();
}

// ---------------- banner ----------------
export function drawBanner(ctx, game, time) {
  const b = game.banner;
  if (!b) return;
  const t = b.t;
  let alpha;
  if (t < 0.35) alpha = clamp(t / 0.35, 0, 1);
  else if (t > b.dur - 0.5) alpha = clamp((b.dur - t) / 0.5, 0, 1);
  else alpha = 1;
  const slide = (1 - alpha) * 30;
  ctx.save();
  ctx.globalAlpha = alpha;
  ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
  ctx.fillStyle = '#6fd0ff';
  ctx.shadowColor = '#6fd0ff'; ctx.shadowBlur = 20;
  ctx.font = `bold 64px ${FONT_SANS}`;
  ctx.fillText(b.text, LOGICAL_W / 2, 230 - slide);
  ctx.shadowBlur = 0;
  if (b.sub) {
    ctx.fillStyle = 'rgba(220,240,255,0.8)';
    ctx.font = `16px ${FONT_MONO}`;
    ctx.fillText(b.sub, LOGICAL_W / 2, 278 - slide);
  }
  ctx.restore();
}

// ---------------- helpers: map thumbnail ----------------
// Draw a small normalized preview of a map's path into box (bx,by,bw,bh).
function drawMapThumb(ctx, def, bx, by, bw, bh, time, sel) {
  const cells = def.cells;
  let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
  for (const [c, r] of cells) { minX = Math.min(minX, c); maxX = Math.max(maxX, c); minY = Math.min(minY, r); maxY = Math.max(maxY, r); }
  const pad = 10;
  const spanX = Math.max(1, maxX - minX), spanY = Math.max(1, maxY - minY);
  const sx = (bw - pad * 2) / spanX, sy = (bh - pad * 2) / spanY;
  const s = Math.min(sx, sy);
  const ox = bx + (bw - spanX * s) / 2 - minX * s;
  const oy = by + (bh - spanY * s) / 2 - minY * s;
  const P = (c, r) => ({ x: ox + c * s, y: oy + r * s });

  // board grid backdrop
  ctx.save();
  ctx.globalAlpha = 0.5;
  ctx.fillStyle = '#0a1612';
  roundRect(ctx, bx, by, bw, bh, 6); ctx.fill();
  ctx.restore();

  // trace
  ctx.save();
  ctx.strokeStyle = sel ? '#46e0b0' : 'rgba(120,200,180,0.6)';
  ctx.lineWidth = sel ? 2.5 : 2;
  ctx.lineJoin = 'round'; ctx.lineCap = 'round';
  ctx.shadowColor = '#46e0b0'; ctx.shadowBlur = sel ? 8 : 0;
  ctx.beginPath();
  cells.forEach(([c, r], i) => { const p = P(c, r); if (i === 0) ctx.moveTo(p.x, p.y); else ctx.lineTo(p.x, p.y); });
  ctx.stroke();
  ctx.restore();

  // spawn + cpu nodes
  const sp = P(cells[0][0], cells[0][1]);
  const cp = P(def.cpu[0], def.cpu[1]);
  const pulse = 0.5 + 0.5 * Math.sin(time * 4);
  ctx.fillStyle = '#ff6b6b';
  ctx.beginPath(); ctx.arc(sp.x, sp.y, 4, 0, Math.PI * 2); ctx.fill();
  ctx.fillStyle = '#7df0c0';
  ctx.shadowColor = '#7df0c0'; ctx.shadowBlur = 6 + pulse * 6;
  ctx.beginPath(); ctx.arc(cp.x, cp.y, 5, 0, Math.PI * 2); ctx.fill();
  ctx.shadowBlur = 0;

  // animated pulse traveling the path
  const segs = [];
  let total = 0;
  for (let i = 0; i < cells.length - 1; i++) {
    const a = P(cells[i][0], cells[i][1]), b = P(cells[i + 1][0], cells[i + 1][1]);
    const len = Math.hypot(b.x - a.x, b.y - a.y);
    segs.push({ a, b, len, start: total }); total += len;
  }
  const d = ((time * 0.5) % 1) * total;
  for (const sg of segs) {
    if (d >= sg.start && d <= sg.start + sg.len) {
      const t = (d - sg.start) / sg.len;
      const px = sg.a.x + (sg.b.x - sg.a.x) * t;
      const py = sg.a.y + (sg.b.y - sg.a.y) * t;
      ctx.fillStyle = '#6fd0ff';
      ctx.shadowColor = '#6fd0ff'; ctx.shadowBlur = 8;
      ctx.beginPath(); ctx.arc(px, py, 3, 0, Math.PI * 2); ctx.fill();
      ctx.shadowBlur = 0;
      break;
    }
  }
}

// ---------------- START SCREEN ----------------
export function drawMenu(ctx, game, time) {
  // backdrop
  ctx.fillStyle = 'rgba(3,8,7,0.9)';
  ctx.fillRect(0, 0, LOGICAL_W, LOGICAL_H);

  // ambient circuit traces behind the title
  drawCircuitMotif(ctx, time);

  // title
  ctx.save();
  ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
  const glow = 0.6 + 0.4 * Math.sin(time * 2);
  ctx.fillStyle = '#46e0b0';
  ctx.shadowColor = '#46e0b0'; ctx.shadowBlur = 30 * glow;
  ctx.font = `bold 72px ${FONT_SANS}`;
  ctx.fillText('CIRCUIT DEFENSE', LOGICAL_W / 2, 78);
  ctx.shadowBlur = 0;
  ctx.fillStyle = 'rgba(159,255,224,0.7)';
  ctx.font = `15px ${FONT_MONO}`;
  ctx.fillText('Solder defense chips on the pads. Stop the bugs from reaching the CPU.', LOGICAL_W / 2, 122);
  ctx.restore();

  // ---- map select ----
  ctx.fillStyle = '#9fffe0';
  ctx.font = `bold 12px ${FONT_MONO}`;
  ctx.textAlign = 'left'; ctx.textBaseline = 'top';
  ctx.fillText('SELECT MAP', 150, 158);

  const cardW = 320, cardH = 150, gap = 20;
  const totalW = cardW * 3 + gap * 2;
  const startX = (LOGICAL_W - totalW) / 2;
  MAP_ORDER.forEach((id, i) => {
    const def = MAPS[id];
    const sel = game.mapId === id;
    const cx = startX + i * (cardW + gap), cy = 178;
    ctx.save();
    ctx.fillStyle = sel ? 'rgba(20,60,48,0.85)' : 'rgba(10,22,18,0.8)';
    ctx.strokeStyle = sel ? '#7df0c0' : 'rgba(120,255,210,0.3)';
    ctx.lineWidth = sel ? 2.5 : 1.5;
    roundRect(ctx, cx, cy, cardW, cardH, 10); ctx.fill(); ctx.stroke();
    ctx.restore();
    // name
    ctx.fillStyle = sel ? '#eafff5' : '#cfeae2';
    ctx.font = `bold 16px ${FONT_MONO}`;
    ctx.textAlign = 'left'; ctx.textBaseline = 'top';
    ctx.fillText(def.name.toUpperCase(), cx + 14, cy + 10);
    ctx.fillStyle = 'rgba(180,220,210,0.6)';
    ctx.font = `10px ${FONT_MONO}`;
    // wrap blurb
    ctx.fillText(def.blurb, cx + 14, cy + 32, cardW - 28);
    // thumbnail
    drawMapThumb(ctx, def, cx + 10, cy + 50, cardW - 20, cardH - 60, time, sel);
    // best on this map+difficulty
    const best = (game.bests()[id + ':' + game.difficulty]) || { wave: 0, score: 0 };
    ctx.fillStyle = 'rgba(255,216,107,0.8)';
    ctx.font = `bold 10px ${FONT_MONO}`;
    ctx.textAlign = 'right';
    ctx.fillText(`BEST W${best.wave}  ${best.score}`, cx + cardW - 12, cy + 12);
    // register the whole card as a hit region
    reg(ctx, 'map_' + id, cx, cy, cardW, cardH, null, { priority: 2 });
  });

  // ---- difficulty select ----
  ctx.fillStyle = '#9fffe0';
  ctx.font = `bold 12px ${FONT_MONO}`;
  ctx.textAlign = 'left'; ctx.textBaseline = 'top';
  ctx.fillText('DIFFICULTY', 150, 348);

  const dbW = 250, dbH = 56, dgap = 18;
  const dtotal = dbW * 3 + dgap * 2;
  const dstart = (LOGICAL_W - dtotal) / 2;
  DIFFICULTY_ORDER.forEach((id, i) => {
    const d = DIFFICULTIES[id];
    const sel = game.difficulty === id;
    const dx = dstart + i * (dbW + dgap), dy = 368;
    ctx.save();
    ctx.fillStyle = sel ? 'rgba(20,60,48,0.85)' : 'rgba(10,22,18,0.8)';
    ctx.strokeStyle = sel ? '#7df0c0' : 'rgba(120,255,210,0.3)';
    ctx.lineWidth = sel ? 2.5 : 1.5;
    roundRect(ctx, dx, dy, dbW, dbH, 8); ctx.fill(); ctx.stroke();
    ctx.restore();
    ctx.fillStyle = sel ? '#eafff5' : '#cfeae2';
    ctx.font = `bold 15px ${FONT_MONO}`;
    ctx.textAlign = 'center'; ctx.textBaseline = 'top';
    ctx.fillText(d.name.toUpperCase(), dx + dbW / 2, dy + 8);
    ctx.fillStyle = 'rgba(180,220,210,0.65)';
    ctx.font = `9px ${FONT_MONO}`;
    ctx.fillText(`${d.gold}g start · ${d.lives} lives`, dx + dbW / 2, dy + 28);
    ctx.fillText(d.blurb, dx + dbW / 2, dy + 41, dbW - 16);
    reg(ctx, 'diff_' + id, dx, dy, dbW, dbH, null, { priority: 2 });
  });

  // ---- how to play + start ----
  reg(ctx, 'helpBtn', LOGICAL_W / 2 - 250, 452, 150, 46, 'HOW TO PLAY',
    { hover: game.hover === 'helpBtn', fs: 12, priority: 2 });
  reg(ctx, 'startGame', LOGICAL_W / 2 - 90, 452, 180, 46, 'START ▸',
    { hover: game.hover === 'startGame', fs: 18, priority: 2 });

  // current selection summary + controls hint
  ctx.fillStyle = 'rgba(180,220,210,0.6)';
  ctx.font = `11px ${FONT_MONO}`;
  ctx.textAlign = 'center'; ctx.textBaseline = 'top';
  ctx.fillText(`${MAPS[game.mapId].name} · ${DIFFICULTIES[game.difficulty].name}   —   Space = start   P = pause   1/2/3 = speed`, LOGICAL_W / 2, 512);

  // audio state hint
  ctx.fillStyle = 'rgba(140,180,175,0.45)';
  ctx.font = `10px ${FONT_MONO}`;
  ctx.fillText(game.audio.muted ? '🔇 muted — ⚙ to enable sound' : '⚙ settings · ? how to play', LOGICAL_W / 2, 534);

  // full-screen capture so stray clicks during menu don't fall through
  reg(ctx, 'menuBackdrop', 0, 0, LOGICAL_W, LOGICAL_H, null, { priority: 0 });
}

// decorative circuit traces with traveling pulses
function drawCircuitMotif(ctx, time) {
  ctx.save();
  ctx.globalAlpha = 0.18;
  ctx.strokeStyle = '#46e0b0';
  ctx.lineWidth = 1.5;
  ctx.lineJoin = 'round';
  // a few orthogonal traces across the top band
  const traces = [
    [[40, 40], [40, 130], [220, 130], [220, 40]],
    [[LOGICAL_W - 40, 40], [LOGICAL_W - 40, 150], [LOGICAL_W - 260, 150], [LOGICAL_W - 260, 40]],
    [[80, LOGICAL_H - 40], [80, LOGICAL_H - 120], [300, LOGICAL_H - 120]],
    [[LOGICAL_W - 80, LOGICAL_H - 40], [LOGICAL_W - 80, LOGICAL_H - 110], [LOGICAL_W - 320, LOGICAL_H - 110]],
  ];
  for (const tr of traces) {
    ctx.beginPath();
    tr.forEach((p, i) => { if (i === 0) ctx.moveTo(p[0], p[1]); else ctx.lineTo(p[0], p[1]); });
    ctx.stroke();
    // node dots at corners
    ctx.fillStyle = '#46e0b0';
    for (const p of tr) { ctx.beginPath(); ctx.arc(p[0], p[1], 3, 0, Math.PI * 2); ctx.fill(); }
  }
  // chips
  ctx.fillStyle = '#0a2a25';
  ctx.strokeStyle = '#46e0b0';
  ctx.lineWidth = 1;
  drawChip(ctx, 150, 60, 26, 18, time);
  drawChip(ctx, LOGICAL_W - 176, 60, 26, 18, time + 1);
  ctx.restore();
}
function drawChip(ctx, x, y, w, h, time) {
  ctx.fillRect(x, y, w, h); ctx.strokeRect(x, y, w, h);
  // pins
  for (let i = 0; i < 3; i++) { ctx.fillRect(x - 3, y + 4 + i * 5, 3, 2); ctx.fillRect(x + w, y + 4 + i * 5, 3, 2); }
  const pulse = 0.5 + 0.5 * Math.sin(time * 3);
  ctx.fillStyle = `rgba(125,240,192,${0.3 + pulse * 0.5})`;
  ctx.beginPath(); ctx.arc(x + w / 2, y + h / 2, 2.5, 0, Math.PI * 2); ctx.fill();
}

// ---------------- STATS SCREEN (game-over & victory) ----------------
export function drawStats(ctx, game) {
  const s = game.runStats();
  const pw = 560, ph = 196;
  const px = (LOGICAL_W - pw) / 2, py = LOGICAL_H / 2 + 70;
  ctx.save();
  ctx.fillStyle = 'rgba(6,14,12,0.92)';
  ctx.strokeStyle = 'rgba(120,255,210,0.4)';
  ctx.lineWidth = 1.5;
  roundRect(ctx, px, py, pw, ph, 10); ctx.fill(); ctx.stroke();
  ctx.restore();

  ctx.fillStyle = '#9fffe0';
  ctx.font = `bold 12px ${FONT_MONO}`;
  ctx.textAlign = 'left'; ctx.textBaseline = 'top';
  ctx.fillText('RUN STATISTICS', px + 16, py + 12);

  // per-tower table
  const cols = ['CHIP', 'BUILT', 'KILLS', 'DMG'];
  let cx = px + 16;
  const rowY0 = py + 38;
  ctx.fillStyle = 'rgba(180,220,210,0.5)';
  ctx.font = `bold 9px ${FONT_MONO}`;
  const colX = [px + 16, px + 150, px + 260, px + 380];
  cols.forEach((c, i) => ctx.fillText(c, colX[i], rowY0));
  PLACE_TYPES.forEach((t, i) => {
    const st = s.byType[t];
    const cfg = TOWER_TYPES[t];
    const ry = rowY0 + 16 + i * 18;
    ctx.fillStyle = cfg.color;
    ctx.fillRect(colX[0], ry + 2, 8, 8);
    ctx.fillStyle = '#eafff5';
    ctx.font = `bold 11px ${FONT_MONO}`;
    ctx.fillText(cfg.label, colX[0] + 14, ry);
    ctx.fillStyle = '#cfeae2';
    ctx.font = `11px ${FONT_MONO}`;
    ctx.fillText(String(st.count), colX[1], ry);
    ctx.fillText(String(st.kills), colX[2], ry);
    ctx.fillText(String(Math.round(st.dmg)), colX[3], ry);
  });

  // summary row
  const sy = rowY0 + 16 + 4 * 18 + 8;
  ctx.fillStyle = 'rgba(200,240,230,0.85)';
  ctx.font = `bold 11px ${FONT_MONO}`;
  ctx.fillText(`KILLED ${s.killed}   LEAKED ${s.leaked}   WAVES ${s.waves}`, colX[0], sy);
  ctx.fillText(`ACCURACY ${Math.round(s.accuracy * 100)}%`, colX[0], sy + 16);
  const best = game.currentBest();
  ctx.fillStyle = 'rgba(255,216,107,0.8)';
  ctx.fillText(`BEST  W${best.wave}  ${best.score}`, colX[2], sy + 16);
}

// ---------------- end-screen shared overlay ----------------
function endOverlay(ctx, game, title, sub, titleColor) {
  ctx.save();
  ctx.fillStyle = 'rgba(3,8,7,0.82)';
  ctx.fillRect(0, 0, LOGICAL_W, LOGICAL_H);
  ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
  ctx.fillStyle = titleColor;
  ctx.shadowColor = titleColor; ctx.shadowBlur = 30;
  ctx.font = `bold 64px ${FONT_SANS}`;
  ctx.fillText(title, LOGICAL_W / 2, LOGICAL_H / 2 - 130);
  ctx.shadowBlur = 0;
  ctx.fillStyle = '#cfeae2';
  ctx.font = `18px ${FONT_MONO}`;
  ctx.fillText(sub, LOGICAL_W / 2, LOGICAL_H / 2 - 78);
  // NEW BEST callout
  if (game.newBest) {
    const wob = Math.sin(game.time * 6) * 4;
    ctx.fillStyle = '#ffd86b';
    ctx.shadowColor = '#ffd86b'; ctx.shadowBlur = 18;
    ctx.font = `bold 30px ${FONT_SANS}`;
    ctx.fillText('★ NEW BEST! ★', LOGICAL_W / 2, LOGICAL_H / 2 - 44 + wob);
    ctx.shadowBlur = 0;
  }
  ctx.fillStyle = '#ffd86b';
  ctx.font = `bold 26px ${FONT_MONO}`;
  ctx.fillText(`SCORE  ${game.score}`, LOGICAL_W / 2, LOGICAL_H / 2 - (game.newBest ? 14 : 10));
  ctx.restore();
}

export function drawGameOver(ctx, game, time) {
  const endlessTxt = game.endless ? ` (endless · wave ${game.wave})` : '';
  endOverlay(ctx, game, 'CORE BREACHED', `You held through ${game.wave} wave${game.wave === 1 ? '' : 's'}${endlessTxt}.`, '#ff6b6b');
  // buttons sit above the stats panel; place them at top of the lower band
  const by = LOGICAL_H - 70;
  reg(ctx, 'restart', LOGICAL_W / 2 - 250, by, 150, 44, 'RESTART ↻',
    { hover: game.hover === 'restart', fs: 14, priority: 2 });
  reg(ctx, 'menu', LOGICAL_W / 2 + 100, by, 150, 44, 'MENU ◀',
    { hover: game.hover === 'menu', fs: 14, priority: 2 });
}

export function drawVictory(ctx, game, time) {
  const title = game.endless ? 'ENDLESS RUN' : 'SYSTEM DEFENDED';
  const sub = game.endless ? `Pushed to wave ${game.wave} before falling.` : 'All 20 waves repelled. The grid is safe.';
  endOverlay(ctx, game, title, sub, '#ffd86b');
  const by = LOGICAL_H - 70;
  if (!game.endless) {
    reg(ctx, 'continue', LOGICAL_W / 2 - 330, by, 200, 44, 'CONTINUE — ENDLESS',
      { hover: game.hover === 'continue', fs: 12, priority: 2 });
  }
  reg(ctx, 'restart', LOGICAL_W / 2 - 100, by, 150, 44, 'PLAY AGAIN ↻',
    { hover: game.hover === 'restart', fs: 13, priority: 2 });
  reg(ctx, 'menu', LOGICAL_W / 2 + 80, by, 150, 44, 'MENU ◀',
    { hover: game.hover === 'menu', fs: 14, priority: 2 });
}

// ---------------- SETTINGS POPOVER ----------------
export function drawSettings(ctx, game, time) {
  const pw = 280, ph = 214;
  const px = LOGICAL_W - pw - 12, py = 58;
  ctx.save();
  ctx.fillStyle = 'rgba(6,14,12,0.96)';
  ctx.strokeStyle = '#7df0c0';
  ctx.lineWidth = 1.5;
  roundRect(ctx, px, py, pw, ph, 10); ctx.fill(); ctx.stroke();
  ctx.fillStyle = '#9fffe0';
  ctx.font = `bold 12px ${FONT_MONO}`;
  ctx.textAlign = 'left'; ctx.textBaseline = 'top';
  ctx.fillText('SETTINGS', px + 14, py + 12);
  ctx.restore();

  const a = game.audio;
  // mute toggle
  reg(ctx, 'mute', px + 14, py + 38, pw - 28, 34, a.muted ? '🔇  SOUND OFF' : '🔊  SOUND ON',
    { hover: game.hover === 'mute', fs: 12, priority: 4 });
  // music toggle
  reg(ctx, 'music', px + 14, py + 80, pw - 28, 34, a.musicOn ? '🎵  MUSIC ON' : '🎵  MUSIC OFF',
    { hover: game.hover === 'music', fs: 12, priority: 4 });

  // volume slider
  ctx.fillStyle = 'rgba(200,240,230,0.8)';
  ctx.font = `bold 11px ${FONT_MONO}`;
  ctx.textAlign = 'left'; ctx.textBaseline = 'middle';
  ctx.fillText('VOLUME', px + 14, py + 134);
  const vx = px + 14, vy = py + 152, vw = pw - 28;
  ctx.fillStyle = 'rgba(40,60,55,0.9)';
  roundRect(ctx, vx, vy, vw, 10, 5); ctx.fill();
  const frac = a.volume;
  ctx.fillStyle = '#46e0b0';
  roundRect(ctx, vx, vy, Math.max(2, vw * frac), 10, 5); ctx.fill();
  ctx.fillStyle = '#eafff5';
  ctx.beginPath(); ctx.arc(vx + vw * frac, vy + 5, 8, 0, Math.PI * 2); ctx.fill();
  // expose rect for click-drag handling in game.js
  if (typeof window !== 'undefined') window.__volRect = { x: vx, y: vy - 6, w: vw, h: 22 };
  reg(ctx, 'volBar', vx, vy - 8, vw, 26, null, { priority: 4 });

  ctx.fillStyle = 'rgba(140,180,175,0.5)';
  ctx.font = `9px ${FONT_MONO}`;
  ctx.textAlign = 'center'; ctx.textBaseline = 'bottom';
  ctx.fillText('persisted · click outside to close', px + pw / 2, py + ph - 8);
  // panel capture
  reg(ctx, 'settingsPanel', px, py, pw, ph, null, { priority: 3 });
}

// ---------------- HELP / HOW TO PLAY ----------------
export function drawHelp(ctx, game, time) {
  ctx.save();
  ctx.fillStyle = 'rgba(3,8,7,0.92)';
  ctx.fillRect(0, 0, LOGICAL_W, LOGICAL_H);
  const pw = 640, ph = 470;
  const px = (LOGICAL_W - pw) / 2, py = (LOGICAL_H - ph) / 2;
  ctx.fillStyle = 'rgba(8,18,16,0.98)';
  ctx.strokeStyle = '#7df0c0';
  ctx.lineWidth = 1.5;
  roundRect(ctx, px, py, pw, ph, 12); ctx.fill(); ctx.stroke();

  ctx.fillStyle = '#9fffe0';
  ctx.font = `bold 20px ${FONT_SANS}`;
  ctx.textAlign = 'center'; ctx.textBaseline = 'top';
  ctx.fillText('HOW TO PLAY', px + pw / 2, py + 18);

  ctx.textAlign = 'left'; ctx.textBaseline = 'top';
  let y = py + 56;
  const line = (label, val, col) => {
    ctx.fillStyle = '#7df0c0';
    ctx.font = `bold 12px ${FONT_MONO}`;
    ctx.fillText(label, px + 28, y);
    ctx.fillStyle = col || '#cfeae2';
    ctx.font = `12px ${FONT_MONO}`;
    ctx.fillText(val, px + 150, y, pw - 178);
    y += 22;
  };

  ctx.fillStyle = 'rgba(159,255,224,0.7)';
  ctx.font = `bold 12px ${FONT_MONO}`;
  ctx.fillText('CONTROLS', px + 28, y); y += 22;
  line('click pad', 'open build menu, pick a chip');
  line('click tower', 'open upgrade / sell / targeting panel');
  line('Space', 'start next wave   ·   P = pause   ·   Esc = close menu');
  line('1 / 2 / 3', 'game speed   ·   touch: tap pads & towers');

  y += 6;
  ctx.fillStyle = 'rgba(159,255,224,0.7)';
  ctx.font = `bold 12px ${FONT_MONO}`;
  ctx.fillText('CHIPS (TOWERS)', px + 28, y); y += 22;
  PLACE_TYPES.forEach((t) => {
    const c = TOWER_TYPES[t];
    ctx.fillStyle = c.color; ctx.fillRect(px + 28, y + 2, 9, 9);
    line(c.label, c.blurb + '  ·  ' + c.cost + 'g', '#eafff5');
  });

  y += 6;
  ctx.fillStyle = 'rgba(159,255,224,0.7)';
  ctx.font = `bold 12px ${FONT_MONO}`;
  ctx.fillText('TARGETING', px + 28, y); y += 22;
  line('FIRST / LAST', 'enemy progress along the trace');
  line('STRONGEST', 'highest current HP   ·   CLOSEST = nearest');

  ctx.restore();
  // close on click anywhere
  reg(ctx, 'helpOverlay', 0, 0, LOGICAL_W, LOGICAL_H, null, { priority: 5 });
}

export function drawFooter(ctx) {
  ctx.save();
  ctx.fillStyle = 'rgba(140,180,175,0.35)';
  ctx.font = `11px ${FONT_MONO}`;
  ctx.textAlign = 'right'; ctx.textBaseline = 'bottom';
  ctx.fillText('Built autonomously by jcode', LOGICAL_W - 10, LOGICAL_H - 8);
  ctx.restore();
}

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
