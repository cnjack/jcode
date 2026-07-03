// waves.js — wave config + spawn queue builder + scaling + endless generator + difficulty.

export const WAVES = [
  { bugs: 6,  sparks: 0,  tanks: 0, swarms: 0, boss: 0, interval: 1.10 }, // 1
  { bugs: 8,  sparks: 0,  tanks: 0, swarms: 0, boss: 0, interval: 1.00 }, // 2
  { bugs: 7,  sparks: 4,  tanks: 0, swarms: 0, boss: 0, interval: 0.95 }, // 3
  { bugs: 9,  sparks: 4,  tanks: 1, swarms: 0, boss: 0, interval: 0.92 }, // 4
  { bugs: 8,  sparks: 6,  tanks: 1, swarms: 0, boss: 1, interval: 0.90 }, // 5
  { bugs: 9,  sparks: 8,  tanks: 1, swarms: 3, boss: 0, interval: 0.86 }, // 6
  { bugs: 11, sparks: 9,  tanks: 2, swarms: 3, boss: 0, interval: 0.82 }, // 7
  { bugs: 11, sparks: 12, tanks: 2, swarms: 4, boss: 0, interval: 0.78 }, // 8
  { bugs: 14, sparks: 10, tanks: 3, swarms: 4, boss: 0, interval: 0.74 }, // 9
  { bugs: 13, sparks: 14, tanks: 3, swarms: 4, boss: 1, interval: 0.70 }, // 10
  { bugs: 16, sparks: 14, tanks: 4, swarms: 5, boss: 0, interval: 0.66 }, // 11
  { bugs: 15, sparks: 18, tanks: 4, swarms: 6, boss: 0, interval: 0.62 }, // 12
  { bugs: 19, sparks: 15, tanks: 5, swarms: 6, boss: 0, interval: 0.58 }, // 13
  { bugs: 18, sparks: 20, tanks: 5, swarms: 7, boss: 0, interval: 0.55 }, // 14
  { bugs: 20, sparks: 18, tanks: 6, swarms: 7, boss: 1, interval: 0.52 }, // 15
  { bugs: 22, sparks: 22, tanks: 6, swarms: 8, boss: 0, interval: 0.50 }, // 16
  { bugs: 26, sparks: 20, tanks: 7, swarms: 8, boss: 0, interval: 0.48 }, // 17
  { bugs: 25, sparks: 28, tanks: 7, swarms: 9, boss: 0, interval: 0.46 }, // 18
  { bugs: 30, sparks: 26, tanks: 8, swarms: 9, boss: 0, interval: 0.44 }, // 19
  { bugs: 36, sparks: 30, tanks: 9, swarms: 10, boss: 1, interval: 0.42 }, // 20
];

export const TOTAL_WAVES = WAVES.length;

// difficulty multipliers
export const DIFFICULTIES = {
  casual: { id: 'casual', name: 'Casual', gold: 340, lives: 25, hpMul: 0.80, countMul: 0.85, blurb: 'relaxed — more gold & lives, weaker foes' },
  normal: { id: 'normal', name: 'Normal', gold: 260, lives: 20, hpMul: 1.00, countMul: 1.00, blurb: 'the intended challenge' },
  hard:   { id: 'hard', name: 'Hard', gold: 220, lives: 15, hpMul: 1.25, countMul: 1.12, blurb: 'tight economy, tougher swarms' },
};
export const DIFFICULTY_ORDER = ['casual', 'normal', 'hard'];

export function getDifficulty(d) { return DIFFICULTIES[d] || DIFFICULTIES.normal; }

export function getWave(n) {
  if (n <= WAVES.length) return WAVES[Math.max(1, n) - 1];
  // endless: generate procedurally beyond wave 20
  const over = n - WAVES.length;
  return {
    bugs: 30 + over * 3, sparks: 28 + over * 3, tanks: 9 + over, swarms: 10 + over,
    boss: (n % 5 === 0) ? 1 + Math.floor(over / 3) : 0,
    interval: Math.max(0.34, 0.42 - over * 0.01),
  };
}

export function hpScale(wave) {
  let m = 1 + 0.16 * (wave - 1);
  if (wave >= 10) m *= 1.12;
  if (wave >= 20) m *= 1.2;
  if (wave > 20) m *= 1 + 0.06 * (wave - 20); // endless keeps climbing
  return m;
}
export function speedScale(wave) { return Math.min(1 + 0.012 * (wave - 1), 1.45); }

export const WAVE_TYPES = ['bug', 'spark', 'tank', 'swarm', 'boss'];

// Build an interleaved spawn queue. countMul scales total counts (difficulty).
export function buildQueue(wave, countMul = 1) {
  const w = getWave(wave);
  const cm = countMul;
  const sc = (x) => Math.max(0, Math.round(x * cm));
  let b = sc(w.bugs), s = sc(w.sparks), t = sc(w.tanks), m = sc(w.swarms), bo = sc(w.boss);
  const queue = [];
  while (b > 0 || s > 0 || t > 0 || m > 0) {
    if (b > 0) { queue.push('bug'); b--; }
    if (s > 0) { queue.push('spark'); s--; }
    if (t > 0) { queue.push('tank'); t--; }
    if (m > 0) { queue.push('swarm'); m--; }
  }
  for (let i = 0; i < bo; i++) {
    const mid = Math.floor(queue.length / 2);
    queue.splice(mid, 0, 'boss');
  }
  return queue;
}
