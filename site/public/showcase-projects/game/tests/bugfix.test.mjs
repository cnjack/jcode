// bugfix.test.mjs — verifies the placement-menu hit-test priority fix by simulating
// the exact click sequence: open menu on a pad -> click the PULSE button -> tower placed.
// Drives the REAL handleClick/hitTest code path (regions registered by render()).
import { LOGICAL_W } from '../js/utils.js';

// stub DOM so render.js's offscreen background cache works headlessly
global.document = {
  createElement: () => ({ width: 0, height: 0, getContext: () => ctx }),
};

import { Game } from '../js/game.js';

function makeCtx() {
  const grad = { addColorStop() {} };
  const handler = {
    get(_t, prop) {
      if (prop === 'createLinearGradient' || prop === 'createRadialGradient' || prop === 'createPattern') return () => grad;
      if (prop === 'measureText') return () => ({ width: 10 });
      if (prop === 'canvas') return { width: 1280, height: 720 };
      if (prop === 'getContext') return () => ctx;
      return () => {};
    },
    set() { return true; },
  };
  const ctx = new Proxy({}, handler);
  return ctx;
}

const ctx = makeCtx();
let pass = 0, fail = 0;
const ok = (c, m) => { if (c) { pass++; console.log('  PASS', m); } else { fail++; console.log('  FAIL', m); } };

const g = new Game();
g.startGame();
g.render(ctx, 0.5); // populate baseline hits

const pad = g.pads.find((p) => !p.occupied);
// replicate drawPlacementMenu panel layout to find the PULSE button centre
const pw = 176, ph = 28 + 4 * 28 + 10;
let px = pad.x - pw / 2;
let py = pad.y - 50 - ph;
px = Math.max(8, Math.min(px, LOGICAL_W - pw - 8));
if (py < 64) py = pad.y + 50;
const btnCx = px + 10 + (pw - 20) / 2;
const btnCy = py + 26 + 0 * 28 + 12; // first button = PULSE

// 1. click the pad to open the menu
g.handleClick(pad.x, pad.y);
ok(g.activePad === pad, 'click pad opens placement menu');

// re-render so menu's place_*/cancel regions are registered this frame
g.render(ctx, 0.5);

// 2. click the PULSE button centre (this is where the bug struck)
const goldBefore = g.gold;
g.handleMove(btnCx, btnCy);
ok(g.menuHoverType === 'pulse', 'hover resolves to place_pulse (not cancel)');
const placed = g.handleClick(btnCx, btnCy);
ok(placed === true, 'click on PULSE consumed');
ok(g.towers.length === 1, 'tower was placed');
ok(g.gold === goldBefore - 70, '70g spent');
ok(g.activePad === null, 'menu closed after placing');

console.log('# new tower types placeable via debug API');
g.gold = 9999;
for (const t of ['beam', 'splash', 'slow']) {
  const p = g.pads.find((x) => !x.occupied);
  ok(g.place(t, p.gx, p.gy) === true, `place ${t}`);
}

console.log('# upgrade / sell / setMode via debug API');
const tw = g.towers[0];
ok(g.upgrade(tw.gx, tw.gy) === true, 'upgrade level 1->2');
ok(g.towers[0].level === 2, 'level now 2');
ok(g.setMode(tw.gx, tw.gy, 'strongest') === true, 'setMode strongest');
ok(g.towers[0].mode === 'strongest', 'mode applied');
ok(g.sell(tw.gx, tw.gy) === true, 'sell tower');
ok(g.towers.length === 3, 'tower removed after sell');

console.log('# speed / pause via debug API');
g.setSpeed(3); ok(g.speed === 3, 'setSpeed 3');
g.setSpeed(7); ok(g.speed === 3, 'setSpeed rejects invalid');
g.pause(true); ok(g.paused === true, 'pause(true)');

console.log(`\nRESULT: ${pass} passed, ${fail} failed`);
process.exit(fail ? 1 : 0);
