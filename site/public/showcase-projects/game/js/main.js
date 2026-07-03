// main.js — bootstrap: canvas/dpr setup, fixed-timestep loop, input, debug API

import { Game } from './game.js';
import { LOGICAL_W, LOGICAL_H } from './utils.js';

const canvas = document.getElementById('game');
const ctx = canvas.getContext('2d');
let dpr = Math.max(1, window.devicePixelRatio || 1);

function resize() {
  dpr = Math.max(1, window.devicePixelRatio || 1);
  const scale = Math.min(window.innerWidth / LOGICAL_W, window.innerHeight / LOGICAL_H);
  const dispW = Math.max(1, Math.round(LOGICAL_W * scale));
  const dispH = Math.max(1, Math.round(LOGICAL_H * scale));
  canvas.style.width = dispW + 'px';
  canvas.style.height = dispH + 'px';
  // backing store scaled by dpr; transform maps logical -> backing pixels
  canvas.width = Math.round(dispW * dpr);
  canvas.height = Math.round(dispH * dpr);
  ctx.setTransform(canvas.width / LOGICAL_W, 0, 0, canvas.height / LOGICAL_H, 0, 0);
  ctx.imageSmoothingEnabled = true;
}
window.addEventListener('resize', resize);
resize();

const game = new Game();

// touch / coarse-pointer detection -> bigger hit targets (min ~40px logical)
try {
  const mq = window.matchMedia && window.matchMedia('(pointer: coarse)');
  game.touch = !!(mq && mq.matches);
} catch (e) { game.touch = false; }

// ---------- fixed-timestep loop with interpolation ----------
const STEP = 1 / 60;
let last = performance.now();
let acc = 0;

function frame(now) {
  let dt = (now - last) / 1000;
  last = now;
  if (dt > 0.25) dt = 0.25; // avoid spiral after tab switch
  acc += dt;

  let steps = 0;
  // Speed multiplies the simulation, not the renderer. Pause halts sim only.
  const simSteps = game.paused ? 0 : game.speed;
  while (acc >= STEP && steps < 12) {
    for (let s = 0; s < simSteps; s++) game.update(STEP);
    acc -= STEP;
    steps++;
  }
  if (acc > STEP) acc = 0; // drop backlog

  const alpha = acc / STEP;
  ctx.setTransform(canvas.width / LOGICAL_W, 0, 0, canvas.height / LOGICAL_H, 0, 0);
  game.render(ctx, alpha);
  requestAnimationFrame(frame);
}
requestAnimationFrame(frame);

// ---------- input mapping ----------
function toLogical(clientX, clientY) {
  const rect = canvas.getBoundingClientRect();
  return {
    x: ((clientX - rect.left) / rect.width) * LOGICAL_W,
    y: ((clientY - rect.top) / rect.height) * LOGICAL_H,
  };
}

canvas.addEventListener('mousemove', (ev) => {
  const p = toLogical(ev.clientX, ev.clientY);
  game.handleMove(p.x, p.y);
});
canvas.addEventListener('mousedown', (ev) => {
  // first user gesture: unlock audio (autoplay policy compliant)
  game.audio.ensure();
  game.audio.resume();
  const p = toLogical(ev.clientX, ev.clientY);
  game.handleClick(p.x, p.y);
});
// touch support — tap = move+click; preventDefault to stop double-tap zoom/scroll
canvas.addEventListener('touchstart', (ev) => {
  if (!ev.touches.length) return;
  ev.preventDefault();
  game.audio.ensure();
  game.audio.resume();
  const t = ev.touches[0];
  const p = toLogical(t.clientX, t.clientY);
  game.handleMove(p.x, p.y);
  game.handleClick(p.x, p.y);
}, { passive: false });

// keyboard: space = start wave / start game, P = pause, 1/2/3 = speed, Esc = close, M = mute, H = help
window.addEventListener('keydown', (ev) => {
  if (ev.code === 'Space') {
    ev.preventDefault();
    game.audio.ensure(); game.audio.resume();
    if (game.phase === 'menu') game.startGame();
    else if (game.phase === 'idle') game.spawnWave();
  } else if (ev.code === 'KeyP') {
    if (game.phase !== 'menu' && game.phase !== 'gameover' && game.phase !== 'victory') {
      game.paused = !game.paused;
    }
  } else if (ev.code === 'Digit1') { game.speed = 1; }
  else if (ev.code === 'Digit2') { game.speed = 2; }
  else if (ev.code === 'Digit3') { game.speed = 3; }
  else if (ev.code === 'KeyM') { game.audio.setMuted(!game.audio.muted); }
  else if (ev.code === 'KeyH') { game.showHelp = !game.showHelp; }
  else if (ev.code === 'Escape') {
    game.activePad = null;
    game.selectedTower = null;
    game.showSettings = false;
    game.showHelp = false;
  }
});

// ---------- debug / automated-testing API ----------
window.__game = {
  get state() {
    return {
      gold: game.gold, lives: game.lives, wave: game.wave, score: game.score, phase: game.phase,
      speed: game.speed, paused: game.paused,
      map: game.mapId, difficulty: game.difficulty, endless: game.endless, muted: game.audio.muted,
    };
  },
  get enemies() {
    return game.enemies.map((e) => ({ id: e.id, x: e.x, y: e.y, hp: e.hp, maxHp: e.maxHp, type: e.type, dist: e.dist, slowFactor: e.slowFactor }));
  },
  get towers() {
    return game.towers.map((t) => ({ gx: t.gx, gy: t.gy, type: t.type, level: t.level, kills: t.kills, damageDealt: Math.round(t.damageDealt), mode: t.mode }));
  },
  get pads() {
    return game.pads.map((p) => ({ gx: p.gx, gy: p.gy, occupied: p.occupied }));
  },
  place: (type, gx, gy) => game.place(type, gx, gy),
  upgrade: (gx, gy) => game.upgrade(gx, gy),
  sell: (gx, gy) => game.sell(gx, gy),
  setMode: (gx, gy, mode) => game.setMode(gx, gy, mode),
  setSpeed: (n) => game.setSpeed(n),
  pause: (b) => game.pause(b),
  addGold: (n) => game.addGold(n),
  spawnWave: () => game.spawnWave(),
  fastForward: (seconds) => game.fastForward(seconds),
  // round-3 additions
  selectMap: (id) => game.selectMap(id),
  setDifficulty: (d) => game.setDifficulty(d),
  startGame: () => game.startGame(),
  bests: () => game.bests(),
  get audio() { return { ctxState: game.audio.ctxState, muted: game.audio.muted, volume: game.audio.volume }; },
};
