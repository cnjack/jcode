# Neon Metropolis

A cinematic, procedurally-generated 3D city built with [three.js](https://threejs.org/)
(r160, vendored). Every run produces a dense, atmospheric skyline with distinct
districts, a full **day/night cycle**, glowing window grids after dark, instanced
**traffic** with headlights and taillights, lit **street lamps**, drifting **clouds**,
night **stars**, floating **district labels**, a **loading overlay**, and a one-key
**cinematic flythrough** that weaves through the city — all from a single integer seed.

## Run it

It's a purely static site. From this directory:

```bash
python3 -m http.server 8000
# then open http://localhost:8000/
```

No build step, no server code, no frameworks. All imports are **relative** and all
assets are vendored locally — there are **zero** runtime requests to external origins,
so it works from any sub-path.

## Controls

| Input | Action |
| --- | --- |
| **Drag** | Orbit the camera |
| **Scroll** / pinch | Zoom in / out |
| **Right-drag** / two-finger | Pan |
| **C** | Toggle the cinematic flythrough |
| **Esc** | Exit the flythrough (and close the help panel) |
| **?** | Toggle the controls help panel |

The camera auto-rotates slowly until your first pointer/touch interaction,
then stops permanently. (The same help table is available in-app via the **?**
button on the title card.)

**Control panel** (top-left, collapsible, keyboard-accessible):

- **Time of day** slider (0–24h, live) — dragging it takes over from auto-cycle.
- **Auto-cycle** toggle + **Cycle speed** — by default the city slowly cycles
  (full 24h ≈ 90s at speed 1) through dawn → day → dusk → night.
- **Building density** slider — rebuilds the city on release.
- **Traffic density** slider — applies live (adds/removes cars).
- **Cinematic** button — starts/stops the flythrough (same as **C**).
- **Seed** input + **Regenerate** — rebuilds with a chosen seed (random if blank).
- **Quality** preset (`high`/`low`) — low disables shadows and uses a smaller
  window texture. The **FPS** meter stays visible at all times.

## Architecture

```
index.html        page shell, import map, control-panel markup, help table,
                  loading overlay, canvas container
css/style.css     dark, neon-accented panel styling (collapsible, mobile-safe)
js/
  rng.js          seeded PRNG (mulberry32) + helpers (range, int, pick, fork, ...)
  textures.js     procedural canvas textures (window grids w/ emissive map,
                  road markings, sidewalk tiles, grass, glow/star sprites,
                  cloud + district-label sprites) — zero external image assets
  city.js         procedural generation: grid, districts, buildings, roads,
                  sidewalks, park, trees, pond, street lamps, ground-fade shader
                  — all InstancedMesh. Exposes materials + a roads descriptor.
  daynight.js     the day/night system: sky-dome gradient shader, sun + moon
                  (discs + directional lights), stars, drifting clouds,
                  hemisphere/ambient, fog, tone-mapping exposure, and the
                  night-feature drivers (window emissives, lamps, car lights)
                  from an hour keyframe table.
  traffic.js      deterministic instanced traffic: two-box cars in two opposing
                  lanes per road, billboarded head/tail-light glows, edge wrap.
  flythrough.js   cinematic camera: a closed, collision-validated Catmull-Rom
                  spline circuit through downtown canyons and over rooftops,
                  driven by the C key / Cinematic button (Esc exits).
  main.js         bootstrap: renderer, camera, OrbitControls, day/night system,
                  city + traffic, control-panel wiring, cinematic flythrough,
                  district labels, render loop, FPS meter, loading overlay, and
                  the window.__city verification API (incl. benchmark)
vendor/
  three.module.js three.js r160 (vendored locally)
  OrbitControls.js camera controls
```

### Procedural pipeline

- **Seeded RNG** (`rng.js`, mulberry32) drives *all* randomness — buildings **and**
  the traffic layout — so the same seed always reproduces the identical city.
- **Layout** — a 14×14 grid of blocks. District is chosen by each block's distance
  from the centre:
  - **Downtown** (core): towers 40–120 m with setbacks, rooftop equipment and
    antenna spires.
  - **Midrise** (ring): 12–35 m boxy offices/apartments.
  - **Residential** (outskirts): low 4–10 m houses with yards and warm colours.
  - **Park** (off-center 3×3 cluster): grass, 40+ instanced trees, a reflective pond.
- **Roads** run along every grid line as asphalt strips with dashed centre markings;
  **sidewalks** are lighter concrete pads under each block.
- **Buildings** use `InstancedMesh` grouped into 8 window-texture variants. The
  facade texture is a **regular grid** of glass panes inset into facade-coloured
  gutters (low daytime contrast — the facade colour dominates via `instanceColor`),
  with a matching **emissive map** carrying the lit subset that glows at night. A
  per-instance `aUvScale` attribute — injected via `onBeforeCompile` (three r160
  splits UVs into per-map varyings `vMapUv` / `vEmissiveMapUv`, so each is scaled
  under its own `#ifdef`) — keeps windows from stretching on tall towers.
- **Street lamps** (instanced poles + emissive heads + additive glow) line every
  road and light up only at night.
- **Trees** are instanced trunk + canopy meshes with green hue variation.

### Day / night system

- A continuous **time-of-day** value 0..24h drives everything. Sun and moon arc
  across a gradient **sky dome** (warm horizon glow blending into a deeper zenith),
  with visible sun/moon discs, **stars**, and drifting **clouds** (sprites tinted by
  the time of day so they warm at dusk) at night.
- Sky gradient, `FogExp2` colour/density, hemisphere + ambient intensity/colour, and
  tone-mapping exposure all interpolate smoothly through **keyframed** dawn → day →
  dusk → night presets (dusk/dawn warm oranges & pinks, day blue, night deep indigo).
- The fog colour matches the horizon, and the base ground plane fades into that
  horizon colour through a custom shader — so the ground dissolves into the sky with
  **no hard polygon edge** from any allowed camera angle.
- Building **window emissives** are minimal in daylight and fade in around dusk so
  the city lights up (the "neon metropolis" money shot). **Street lamps** and
  **car head/tail-lights** likewise fade in at night.

### Traffic system

- Cars are `InstancedMesh` two-box silhouettes (body + cabin) in varied paint colours,
  travelling in **two opposing lanes** per road at slightly varied speeds. They wrap
  at the fogged city edges (respawn is invisible and deterministic).
- Each car carries billboarded **headlight** (white, front) and **taillight** (red,
  rear) additive glows that fade in at night. Per-frame updates perform no
  allocations.

### Cinematic flythrough

- Press **C** (or the **Cinematic** button) to hand the camera to a smooth closed
  **Catmull-Rom spline** circuit that dives through downtown street canyons and
  skims over rooftops (~60s per lap, gentle look-ahead). **Esc** exits and
  restores orbit control at the current position.
- The path is **built and validated against the actual building footprints**: any
  waypoint or sampled point that would clip a building is nudged up over a few
  relaxation passes, so the camera never flies through geometry. It rebuilds on
  every `regenerate` / density change.

### Atmosphere & UI

- **Drifting clouds** and night **stars** float overhead; clouds are tinted by the
  time of day.
- **District labels** (DOWNTOWN, MIDRISE, RESIDENTIAL, THE PARK) fade in as the
  camera rises or pulls back, and stay visible during the flythrough.
- A **loading overlay** is shown immediately (plain CSS, before the module boots)
  and fades out once the first frame has rendered; if WebGL is unavailable it
  reports the error in place.

### Rendering

- Hemisphere + ambient lights graded by time of day; the **sun** is a directional
  light with PCFSoft shadows (2048² map) fitted to the central city (high quality).
- ACESFilmic tone mapping + sRGB output for a filmic look.

### Determinism / verification API

`window.__city` is exposed for automated verification:

```js
window.__city.seed              // current seed (integer)
window.__city.buildingCount     // number of buildings (>= 250 at default density)
window.__city.carCount          // number of active cars
window.__city.regenerate(seed?) // rebuild (buildings + traffic); identical per seed
window.__city.getTimeOfDay()    // 0..24
window.__city.setTimeOfDay(h)   // 0..24, applies immediately, pauses auto-cycle
window.__city.setAutoCycle(bool)
window.__city.setQuality('low'|'high')
window.__city.startFlythrough()    // begin the cinematic camera path
window.__city.stopFlythrough()
window.__city.isFlythrough()
window.__city.seekFlythrough(t)    // place camera at fraction t (0..1) + render one frame
window.__city.benchmark(frames=60) // sync-renders N frames in a tight loop
                                   // (direct renderer.render, no rAF) and returns
                                   // { avgMs, fps }; works in a hidden tab
```

`regenerate(seed)` (or `buildCity`/`createTraffic`) called twice with the same seed
yields identical layouts. `benchmark()` synchronously renders the requested number of
frames so it measures real GPU work even in a background tab.

## Performance

Instancing keeps draw calls low (a few dozen). The render loop performs no per-frame
allocations. Targets 60 fps on desktop and stays ≥ 30 fps on high quality (verifiable
via `__city.benchmark(120).fps`). Use the **low** quality preset to disable shadows
and shrink the window texture on weaker devices.

## Notes

- All assets are vendored locally under `vendor/` or generated procedurally at runtime
  — there are **zero** runtime network requests to external origins and all paths are
  relative.

Built autonomously by jcode.
