// Render smoke test — stubs the canvas context so we can execute game.render()
// (and the PCB background cache) headlessly to catch runtime errors in the draw path.
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

global.document = {
  createElement: () => ({ width: 0, height: 0, getContext: () => makeCtx() }),
};

const { Game } = await import('../js/game.js');
const g = new Game();
let errors = 0;
const ctx = makeCtx();

const tryRun = (label, fn) => {
  try { fn(); console.log('  OK', label); }
  catch (e) { errors++; console.log('  ERR', label, '->', e.message); }
};

tryRun('render menu', () => g.render(ctx, 0.5));
tryRun('startGame', () => g.startGame());
tryRun('render idle', () => g.render(ctx, 0.5));
tryRun('place pulse', () => g.place('pulse', g.pads[0].gx, g.pads[0].gy));
tryRun('open placement menu', () => { g.activePad = g.pads[1]; g.handleMove(g.pads[1].x, g.pads[1].y); });
tryRun('render with menu open', () => g.render(ctx, 0.5));
tryRun('spawnWave', () => g.spawnWave());
tryRun('render combat w/ enemies', () => { g.fastForward(1.0); g.render(ctx, 0.5); });
tryRun('beam tower + render', () => { g.gold = 9999; const p = g.pads.find(x => !x.occupied); g.place('beam', p.gx, p.gy); g.fastForward(0.5); g.render(ctx, 0.5); });
tryRun('gameover render', () => { g.lives = 0; g.phase = 'gameover'; g.render(ctx, 0.5); });
tryRun('victory render', () => { g.phase = 'victory'; g.render(ctx, 0.5); });
tryRun('restart render', () => { g.restart(); g.render(ctx, 0.5); });
tryRun('100 varied frames', () => {
  g.startGame();
  for (let i = 0; i < 100; i++) {
    if (g.phase === 'idle' && i % 20 === 5) g.spawnWave();
    g.fastForward(0.3);
    g.render(ctx, (i % 60) / 60);
  }
});

console.log(`\nRESULT: ${errors} render errors`);
process.exit(errors ? 1 : 0);
