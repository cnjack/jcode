# Circuit Defense

A canvas-based tower-defense game with a circuit-board aesthetic. Software bugs
crawl along copper traces toward your CPU core; solder defense chips onto the
build pads to stop them. Survive 20 waves — or push into **endless** mode.

> **Purely static.** No build step, no server code, no external assets, **no audio
> files** — every sound is synthesized at runtime with the WebAudio API. Serve with
> `python3 -m http.server` from the `box/` directory and open the page.

---

## Overview

- **Goal:** defend the CPU. Each enemy that reaches the core costs 1 life. Lose
  all lives → game over. Clear all 20 waves → victory (and an option to continue
  endlessly with scaling difficulty).
- **Loop:** pick a map + difficulty → build & upgrade chips between/within waves
  → spend gold from kills → repeat. Best wave & best score are tracked per
  map+difficulty in `localStorage`.
- **Presentation:** all UI is drawn on a single `<canvas>` (HUD, menus, stats,
  settings). A fixed-timestep simulation keeps gameplay deterministic; rendering
  interpolates between steps for smoothness.

## Controls

| Action | Mouse | Keyboard | Touch |
|---|---|---|---|
| Start game / next wave | `START` button | `Space` | tap button |
| Open build menu | click an empty pad | — | tap pad |
| Build a chip | click a chip option | — | tap option |
| Open tower panel | click a tower | — | tap tower |
| Upgrade / sell / targeting | panel buttons | — | tap buttons |
| Pause | `❚❚` button | `P` | tap button |
| Game speed | `1x/2x/3x` buttons | `1` / `2` / `3` | tap buttons |
| Mute / Help | `⚙` / `?` (HUD) | `M` / `H` | tap icons |
| Close menu/overlay | click away | `Esc` | tap away |

Touch devices (detected via `(pointer: coarse)`) get enlarged hit targets, and
all gestures are captured by the canvas (`touch-action: none`) so the browser
never pans or zooms over the board.

---

## Towers (chips)

Four chip types, each with 3 upgrade levels. **Upgrade cost** rises:
`round(cost × 0.85)` for L1→L2, `round(cost × 1.5)` for L2→L3. Selling refunds
**70%** of total gold invested.

### PULSE — fast single-target projectile (`#5fe3ff`, 70g)
| Lv | Dmg | Rate | DPS | Cost (invested) | DPS/gold |
|----|-----|------|-----|-----------------|----------|
| 1 | 9 | 2.6/s | 23.7 | 70 | 0.34 |
| 2 | 16 | 3.2/s | 51.6 | 130 | 0.40 |
| 3 | 27 | 4.0/s | 108.0 | 235 | 0.46 |

### BEAM — continuous laser, DPS applied per tick (`#ff5db0`, 120g)
| Lv | DPS | Range | Cost (invested) | DPS/gold |
|----|-----|-------|-----------------|----------|
| 1 | 24 | 156 | 120 | 0.20 |
| 2 | 42 | 176 | 222 | 0.19 |
| 3 | 68 | 200 | 402 | 0.17 |

### SPLASH — lobbed AoE + knockback (`#ff9b3d`, 95g)
| Lv | Dmg | Rate | DPS¹ | Splash r | Cost (invested) |
|----|-----|------|------|----------|-----------------|
| 1 | 18 | 0.87/s | 15.7 | 52 | 95 |
| 2 | 30 | 1.00/s | 30.0 | 62 | 176 |
| 3 | 48 | 1.18/s | 56.5 | 74 | 319 |

¹DPS is *per target* — splash multiplies by how many foes sit inside the blast,
so dense clusters make it the most efficient chip in the game.

### SLOW — cryo aura, no damage (`#b06bff`, 85g)
| Lv | Slow | Range | Cost (invested) |
|----|------|-------|-----------------|
| 1 | 40% | 120 | 85 |
| 2 | 45% | 138 | 157 |
| 3 | 50% | 158 | 285 |

> **Curve rationale:** Pulse is the cheap workhorse with the best DPS-per-gold and
> a gently *rising* efficiency curve that rewards upgrading. Beam front-loads
> reliable single-target pressure at a premium and slightly *declining* efficiency
> to balance its "always hits" reliability. Splash trades raw single-target DPS
> for AoE multiplier + knockback (value spikes vs. swarms). Slow is a force
> multiplier — it costs a slot and gold but lets every damage chip land more hits.
> The slow field is capped at **50%** (L3) so it stays a tactical tool, not a
> hard wall.

**Targeting modes (all damage chips):** `FIRST` / `LAST` (progress along trace),
`STRONGEST` (highest HP), `CLOSEST` (nearest).

---

## Enemy roster

| Type | HP | Speed | Reward | Notes |
|------|----|----|--------|-------|
| Bug | 34 | 68 | 8g | balanced fodder |
| Spark | 14 | 142 | 5g | fast, fragile — outruns slow projectiles |
| Tank | 130 | 44 | 20g | **armor 4** (flat dmg reduction per hit), soaks beam |
| Swarm | 28 | 92 | 6g | **splits into 2** minis on death |
| Mini | 11 | 165 | 3g | swarm split-off, very fast |
| Boss | 1100 | 34 | 150g | **armor 6**, resists knockback, triggers ominous drone |

HP scales with wave (`1 + 0.16·(wave-1)`, ×1.12 at wave 10, ×1.2 at wave 20);
speed creeps up to +45%. Bosses appear on waves 5, 10, 15, 20.

---

## Maps & Difficulty

**Maps** (selectable on the start screen, each with a live path-thumbnail):
- **Serpentine** — classic winding S-trace spanning the full board (default).
- **Dual Bus** — three long parallel runs forming a layered U.
- **The Grid** — tight, compact zig-zag; fewer pads, faster pressure.

**Difficulty** (multiplies enemy HP & count, sets starting economy):

| Difficulty | Start gold | Lives | Enemy HP | Enemy count |
|------------|-----------|-------|----------|-------------|
| Casual | 340 | 25 | ×0.80 | ×0.85 |
| Normal | 260 | 20 | ×1.00 | ×1.00 |
| Hard | 220 | 15 | ×1.25 | ×1.12 |

**Endless:** after clearing wave 20, the victory screen offers *CONTINUE —
ENDLESS*. Waves 21+ are generated procedurally with climbing HP/count and a boss
every 5 waves; score keeps counting until the core falls.

---

## Architecture

```
js/
  main.js     bootstrap: canvas/DPR, fixed-timestep loop, input, debug API
  game.js     Game class: state, simulation, economy, phases, rendering, input dispatch
  map.js      selectable maps as DATA (waypoints, path, pads, CPU) + live bindings
  waves.js    wave table + difficulty + endless generator + HP/speed scaling
  towers.js   tower types, targeting, projectiles, chip rendering
  enemies.js  enemy types, path movement, slow/armor/knockback, rendering
  render.js   background/trace/pads/spawn/cpu, particles, floaters, rings, vignette
  audio.js    WebAudio synth SFX + generative ambient music + settings persistence
  ui.js       HUD, menus, start screen, settings/help/stats overlays, hit-testing
  utils.js    constants, geometry helpers, fonts
```

- **Fixed-timestep loop:** `main.js` accumulates real time and steps the
  simulation in fixed `1/60`s increments (scaled by the speed multiplier), with a
  renderer that interpolates (`alpha`) between steps. A backlog cap prevents the
  spiral-of-death after a tab switch.
- **Single canvas:** all UI is drawn in a 1280×720 *logical* space and scaled to
  fit any viewport (uniform scale ⇒ letterboxed, fully visible, nothing overlaps).
- **Hit-testing:** `ui.js` registers interactive rects each frame with a
  `priority`; clicks resolve to the topmost rect — so buttons always beat
  full-screen cancel backdrops.
- **Audio (no files):** `AudioEngine` builds its graph lazily on the **first user
  gesture** (autoplay-policy compliant — no console warnings). SFX are short
  oscillator/noise voices with envelopes; ambient music is a slow procedural
  pentatonic arpeggio. All triggers are throttled and **skipped during
  fast-forward** so they never backlog. Mute / volume / music preference persist
  in `localStorage`.
- **Debug API** (`window.__game`): `state` (incl. `map`, `difficulty`,
  `endless`, `muted`), `enemies`, `towers`, `pads`, `place`, `upgrade`, `sell`,
  `setMode`, `setSpeed`, `pause`, `addGold`, `spawnWave`, `fastForward`,
  `selectMap`, `setDifficulty`, `startGame`, `bests`, `audio`.
- **Tests:** `tests/sim.test.mjs` (headless full-run sanity) and
  `tests/bugfix.test.mjs` (placement/upgrade/sell/mode/speed) run under plain
  Node — they are not needed at runtime.

## Balance rationale

Pulse anchors the meta as the cheap, scaling-efficient generalist so a new
player can always make progress. Beam and Splash are premium specialists that
shine against tanks and swarms respectively, encouraging mixed defenses rather
than one dominant build. The slow field is deliberately capped at 50% so it
amplifies damage instead of trivializing waves. Difficulty spreads economy and
enemy multipliers so Casual is forgiving, Normal is the intended tension curve,
and Hard punishes greed. Endless HP scaling (×1.2 at wave 20, then +6%/wave)
guarantees runs eventually end, keeping the per-map+difficulty leaderboards
meaningful.

---

*Built autonomously by jcode.*
