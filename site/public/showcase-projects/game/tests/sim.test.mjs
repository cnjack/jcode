// Headless simulation test — verifies core loop without a browser/DOM.
import { Game } from '../js/game.js';

const g = new Game();
let pass = 0, fail = 0;
const ok = (c, m) => { if (c) { pass++; console.log('  PASS', m); } else { fail++; console.log('  FAIL', m); } };

console.log('# initial state');
ok(g.phase === 'menu', 'starts in menu phase');
ok(g.gold === 260, 'start gold 260');
ok(g.lives === 20, 'start lives 20');
ok(g.wave === 0, 'wave 0');

console.log('# pads exist');
ok(g.pads.length > 10, `has ${g.pads.length} build pads`);
const sample = g.pads.find(p => !p.occupied);
ok(!!sample, 'found an empty pad');

console.log('# start game -> idle');
g.startGame();
ok(g.phase === 'idle', 'now idle');

console.log('# place a pulse tower on an empty pad near path');
// pick a pad adjacent to the top path row for coverage
const pad = g.pads[0];
const placed = g.place('pulse', pad.gx, pad.gy);
ok(placed === true, 'place() returned true');
ok(g.gold === 260 - 70, 'gold deducted to 190');
ok(g.pads[0].occupied === true, 'pad now occupied');
ok(g.towers.length === 1, 'one tower exists');
ok(g.place('pulse', pad.gx, pad.gy) === false, 'cannot double-place on same pad');

console.log('# cannot afford test');
g.gold = 0;
const poor = g.pads.find(p => !p.occupied);
ok(g.place('beam', poor.gx, poor.gy) === false, 'place denied when broke');
g.gold = 1000;

console.log('# spawn wave 1 and fast-forward until cleared');
g.spawnWave();
ok(g.phase === 'combat', 'combat after spawnWave');
ok(g.wave === 1, 'wave == 1');
const enemiesBefore = g.enemies.length;
// give the simulation plenty of time; towers placed earlier cover early path
g.fastForward(90);
console.log('  -> enemies left:', g.enemies.length, 'phase:', g.phase, 'gold:', g.gold, 'score:', g.score, 'lives:', g.lives);
ok(g.score > 0, 'score increased (kills happened)');
ok(g.gold > 190, 'gold increased from rewards');
ok(g.lives >= 0 && g.lives <= 20, 'lives in range');

console.log('# full 20-wave run sanity (auto economy)');
const g2 = new Game();
g2.startGame();
// sprinkle towers across many pads to defend
let placedCount = 0;
for (const p of g2.pads) {
  const type = placedCount % 3 === 0 ? 'beam' : 'pulse';
  if (g2.gold >= (type === 'beam' ? 120 : 70)) {
    if (g2.place(type, p.gx, p.gy)) placedCount++;
  }
  if (placedCount >= 14) break;
}
let guard = 0;
while (g2.phase !== 'victory' && g2.phase !== 'gameover' && guard < 4000) {
  if (g2.phase === 'idle') g2.spawnWave();
  // top up gold occasionally so economy doesn't stall in idle forever
  g2.fastForward(40);
  guard++;
  // keep adding towers if affordable
  for (const p of g2.pads) {
    if (!p.occupied && g2.gold >= 120) { g2.place('beam', p.gx, p.gy); }
  }
}
console.log('  -> full run phase:', g2.phase, 'wave:', g2.wave, 'score:', g2.score, 'lives:', g2.lives, 'loops:', guard);
ok(g2.phase === 'victory' || g2.phase === 'gameover', '20-wave run reached a terminal phase');
ok(g2.wave === 20, 'reached wave 20');

console.log('# restart resets');
g2.restart();
ok(g2.phase === 'idle', 'restart -> idle');
ok(g2.gold === 260 && g2.lives === 20 && g2.wave === 0, 'restart reset economy');
ok(g2.towers.length === 0 && g2.enemies.length === 0, 'restart cleared entities');
ok(g2.pads.every(p => !p.occupied), 'restart freed pads');

console.log(`\nRESULT: ${pass} passed, ${fail} failed`);
process.exit(fail ? 1 : 0);
