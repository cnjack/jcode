// js/city.js
// Procedural city generation. Deterministic for a given seed.
//
// Layout: a GRID x GRID set of blocks separated by roads. Each block belongs
// to a district chosen by distance from the city centre (downtown core,
// midrise ring, residential outskirts) with one off-center multi-block PARK.
//
// Everything heavy uses THREE.InstancedMesh so draw calls stay tiny:
//   - 8 building InstancedMeshes (one per window-texture variant)
//   - 1 plinth (dark building base) InstancedMesh
//   - 1 antenna InstancedMesh
//   - trunk + canopy InstancedMeshes for trees
//   - 2 road InstancedMeshes (horizontal / vertical strips)
//   - sidewalk + park-pad InstancedMeshes
//
// Per-building window tiling is achieved with a per-instance `aUvScale`
// attribute injected through onBeforeCompile, so tall towers don't stretch
// their windows.

import * as THREE from 'three';
import { createRNG } from './rng.js';
import {
  makeWindowTexture,
  makeRoadTexture,
  makeSidewalkTexture,
  makeGrassTexture,
  makeGlowTexture,
} from './textures.js';

// ---------------------------------------------------------------------------
// Tunables
// ---------------------------------------------------------------------------
const GRID = 14; // blocks per side
const BLOCK = 26; // block size (world units)
const ROAD = 9; // road width
const PITCH = BLOCK + ROAD; // centre-to-centre distance
const CITY_SPAN = GRID * PITCH; // full city extent
const SIDEWALK_TOP = 0.8; // top of sidewalk pad = building base
const DASH_WORLD = 7; // world units per dash cycle on roads

// Park block range (off-center cluster)
const PARK_I = [2, 3, 4];
const PARK_J = [9, 10, 11];

// Window texture variants -> gives visual variety between buildings.
const VARIANT_CFG = [
  { cols: 8, rows: 16, lit: 0.32, litColors: ['#ffd98a', '#ffce6f'], seed: 11 },
  { cols: 6, rows: 14, lit: 0.4, litColors: ['#cfe6ff', '#bfe0ff'], seed: 22 },
  { cols: 8, rows: 18, lit: 0.46, litColors: ['#ffe6a8', '#ffd98a'], seed: 33 },
  { cols: 10, rows: 20, lit: 0.28, litColors: ['#bcdcff', '#a5d0ff'], seed: 44 },
  { cols: 6, rows: 12, lit: 0.5, litColors: ['#ffe9b0', '#ffd0a0'], seed: 55 },
  { cols: 8, rows: 14, lit: 0.36, litColors: ['#cfe6ff', '#e8f0ff'], seed: 66 },
  { cols: 7, rows: 16, lit: 0.42, litColors: ['#ffd98a', '#cfe6ff'], seed: 77 },
  { cols: 9, rows: 18, lit: 0.3, litColors: ['#a5d0ff', '#ffd0a0'], seed: 88 },
];
const N_VAR = VARIANT_CFG.length;

// Muted realistic palettes per district.
const PALETTE = {
  downtown: ['#4a5a6e', '#3a4759', '#5a6b80', '#2f3a48', '#465668', '#62718a', '#384452'],
  midrise: ['#8a8276', '#7a7268', '#948c7e', '#6d6458', '#86796a', '#807464'],
  residential: ['#c9a98c', '#b89a82', '#d4b8a0', '#a89580', '#c2a890', '#d8c0a8', '#b09078'],
};

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------
const _dummy = new THREE.Object3D();
const _color = new THREE.Color();

function blockCenter(i, j) {
  return {
    x: -CITY_SPAN / 2 + PITCH / 2 + i * PITCH,
    z: -CITY_SPAN / 2 + PITCH / 2 + j * PITCH,
  };
}

function isPark(i, j) {
  return PARK_I.includes(i) && PARK_J.includes(j);
}

function districtOf(i, j) {
  const cx = (GRID - 1) / 2;
  const cz = (GRID - 1) / 2;
  const dist = Math.hypot(i - cx, j - cz) / Math.hypot(cx, cz);
  if (dist < 0.34) return 'downtown';
  if (dist < 0.62) return 'midrise';
  return 'residential';
}

// Patch a MeshStandardMaterial so per-instance UV tiling works via the
// `aUvScale` instanced attribute. On three.js r152+ there is no longer a
// monolithic `vUv` varying; each texture channel has its own varying
// (vMapUv, vEmissiveMapUv, ...) assigned inside the `uv_vertex` chunk under
// per-map #ifdefs. We multiply the channels this material actually uses,
// guarded by the SAME #ifdefs three uses, so the program always links and
// window tiling stays per-instance (tall towers don't stretch their windows).
// `instanceColor` tinting is untouched, so per-building colours still work.
function patchInstancedUV(material) {
  material.onBeforeCompile = (shader) => {
    shader.vertexShader = shader.vertexShader
      .replace(
        '#include <common>',
        '#include <common>\nattribute vec2 aUvScale;'
      )
      .replace(
        '#include <uv_vertex>',
        '#include <uv_vertex>\n' +
          '#ifdef USE_MAP\n\tvMapUv *= aUvScale;\n#endif\n' +
          '#ifdef USE_EMISSIVEMAP\n\tvEmissiveMapUv *= aUvScale;\n#endif'
      );
  };
  // Give this patched variant its own cache key so it never collides with a
  // stock program in three's shader cache.
  material.customProgramCacheKey = () => 'instancedAuvScaleV2';
  return material;
}

// ---------------------------------------------------------------------------
// Building generation
// ---------------------------------------------------------------------------
function heightFor(district, rng) {
  if (district === 'downtown') return rng.range(40, 120);
  if (district === 'midrise') return rng.range(12, 35);
  return rng.range(4, 10); // residential
}

// Produce the stacked boxes + optional antenna that make up one building.
// Returns { boxes:[{w,d,h,y}], antenna:H|null } where y is the base offset
// (relative to SIDEWALK_TOP) for each box.
function makeBuilding(rng, district, w, d) {
  const totalH = heightFor(district, rng);
  const boxes = [];
  let curW = w;
  let curD = d;
  let curY = 0;
  let left = totalH;

  // number of stacked masses (setbacks)
  let masses = 1;
  if (district === 'downtown') {
    const r = rng.next();
    masses = r < 0.45 ? 1 : r < 0.85 ? 2 : 3;
  } else if (district === 'midrise') {
    masses = rng.chance(0.25) ? 2 : 1;
  }

  for (let m = 0; m < masses; m++) {
    const last = m === masses - 1;
    const frac = last ? 1 : rng.range(0.45, 0.65);
    const hMass = left * frac;
    left -= hMass;
    boxes.push({ w: curW, d: curD, h: hMass, y: curY });
    curY += hMass;
    // shrink footprint for the next setback
    curW *= rng.range(0.72, 0.86);
    curD *= rng.range(0.72, 0.86);
  }

  // rooftop equipment box
  if (district !== 'residential' && rng.chance(0.3)) {
    boxes.push({
      w: curW * rng.range(0.25, 0.45),
      d: curD * rng.range(0.25, 0.45),
      h: rng.range(2, 5),
      y: curY,
    });
  }

  // antenna spire (downtown only)
  let antenna = null;
  if (district === 'downtown' && rng.chance(0.3)) {
    antenna = rng.range(8, 20);
  }

  return { boxes, antenna, topY: curY, totalH };
}

// Fill one block with buildings according to its district. Pushes records
// into the per-variant box arrays, plinths, and antennas.
function fillBlock(rng, i, j, district, variantBoxes, plinths, antennas, footprints, beacons, density) {
  const { x: cx, z: cz } = blockCenter(i, j);
  const margin = 1.5;
  const cell = (BLOCK - margin * 2) / 2; // 2x2 subdivision cell size

  // Decide how many lots & footprints
  let lots = []; // {lx,lz,w,d}
  const cellPos = (gx, gz) => ({
    lx: cx - cell / 2 - margin / 2 + gx * (cell + margin),
    lz: cz - cell / 2 - margin / 2 + gz * (cell + margin),
  });

  if (district === 'downtown') {
    if (rng.chance(0.5)) {
      // single big tower covering most of the block
      lots.push({ ...{ x: cx, z: cz }, w: rng.range(13, 20), d: rng.range(13, 20) });
      // rewrite keys
      lots[0].lx = cx;
      lots[0].lz = cz;
    } else {
      // two towers in two cells
      const cells = rng.shuffle([[0, 0], [1, 0], [0, 1], [1, 1]]).slice(0, 2);
      for (const [gx, gz] of cells) {
        const p = cellPos(gx, gz);
        lots.push({ lx: p.lx, lz: p.lz, w: rng.range(8, 12), d: rng.range(8, 12) });
      }
    }
  } else if (district === 'midrise') {
    const cells = rng.shuffle([[0, 0], [1, 0], [0, 1], [1, 1]]).slice(0, rng.chance(0.5) ? 2 : 3);
    for (const [gx, gz] of cells) {
      const p = cellPos(gx, gz);
      lots.push({ lx: p.lx, lz: p.lz, w: rng.range(8, 12), d: rng.range(8, 12) });
    }
  } else {
    // residential: more lots, smaller footprints with yards
    const cells = rng.shuffle([[0, 0], [1, 0], [0, 1], [1, 1]]).slice(0, rng.int(2, 4));
    for (const [gx, gz] of cells) {
      const p = cellPos(gx, gz);
      lots.push({ lx: p.lx, lz: p.lz, w: rng.range(6, 9), d: rng.range(6, 9) });
    }
  }

  // density control: probabilistically drop lots, always keep >=1 per block.
  if (density < 1) {
    const kept = [];
    for (const lot of lots) {
      if (rng.next() < density) kept.push(lot);
    }
    if (kept.length === 0) kept.push(lots[0]);
    lots = kept;
  }

  for (const lot of lots) {
    const variant = rng.int(0, N_VAR - 1);
    const palette = PALETTE[district];
    const hex = palette[Math.floor(rng.next() * palette.length)];
    _color.set(hex);
    const r = _color.r, g = _color.g, b = _color.b;

    const { boxes, antenna, topY, totalH } = makeBuilding(rng, district, lot.w, lot.d);

    const vc = VARIANT_CFG[variant];
    for (const box of boxes) {
      variantBoxes[variant].push({
        px: lot.lx,
        py: SIDEWALK_TOP + box.y + box.h / 2,
        pz: lot.lz,
        sx: box.w,
        sy: box.h,
        sz: box.d,
        r, g, b,
        // Variant-aware tiling: ~2.2u wide x 2.8u tall windows, uniform across
        // towers (no stretched/dense windows).
        uvx: box.w / (vc.cols * 2.2),
        uvy: box.h / (vc.rows * 2.8),
      });
    }

    // plinth: slightly wider dark base for grounding (AO cheat)
    plinths.push({
      px: lot.lx,
      py: (SIDEWALK_TOP + 0.6) / 2,
      pz: lot.lz,
      sx: lot.w + 1.6,
      sy: SIDEWALK_TOP + 0.6,
      sz: lot.d + 1.6,
    });

    if (antenna) {
      antennas.push({ px: lot.lx, py: SIDEWALK_TOP + boxes[boxes.length - 1].y + boxes[boxes.length - 1].h, h: antenna });
    }

    // Record the building footprint + total height (for flythrough collision
    // validation). topY is the stacked-mass height (excl. rooftop equip).
    const buildingTop = SIDEWALK_TOP + (antenna ? topY + antenna : topY);
    footprints.push({ x: lot.lx, z: lot.lz, hw: lot.w / 2 + 0.5, hd: lot.d / 2 + 0.5, top: buildingTop });

    // Aviation beacon on the tallest downtown towers.
    if (district === 'downtown' && totalH > 78) {
      beacons.push({ x: lot.lx, z: lot.lz, y: buildingTop + 1.5 });
    }
  }
}

// ---------------------------------------------------------------------------
// Tree generation on park blocks
// ---------------------------------------------------------------------------
function fillPark(rng, trees) {
  for (const i of PARK_I) {
    for (const j of PARK_J) {
      const { x: cx, z: cz } = blockCenter(i, j);
      const isCenter = i === PARK_I[1] && j === PARK_J[1];
      const n = isCenter ? 3 : rng.int(5, 8);
      for (let t = 0; t < n; t++) {
        const lx = cx + rng.range(-BLOCK / 2 + 2, BLOCK / 2 - 2);
        const lz = cz + rng.range(-BLOCK / 2 + 2, BLOCK / 2 - 2);
        // avoid pond on center block
        if (isCenter && Math.hypot(lx - cx, lz - cz) < 9) continue;
        trees.push({ x: lx, z: lz, s: rng.range(0.7, 1.4), hue: rng.range(-0.04, 0.04) });
      }
    }
  }
}

// ---------------------------------------------------------------------------
// Main builder
// ---------------------------------------------------------------------------
export function buildCity(seed, opts = {}) {
  const density = Math.max(0.1, Math.min(1.5, opts.density ?? 1.0));
  const quality = opts.quality === 'low' ? 'low' : 'high';
  const rng = createRNG(seed >>> 0);

  // --- generate data first (so we know exact instance counts) ---
  const variantBoxes = Array.from({ length: N_VAR }, () => []);
  const plinths = [];
  const antennas = [];
  const trees = [];
  const footprints = []; // {x,z,hw,hd,top} for flythrough collision validation
  const beacons = []; // aviation beacon positions on tall towers
  let buildingCount = 0;

  for (let i = 0; i < GRID; i++) {
    for (let j = 0; j < GRID; j++) {
      if (isPark(i, j)) continue;
      const before = variantBoxes.reduce((s, a) => s + a.length, 0);
      const beforeB = buildingCount;
      fillBlock(rng, i, j, districtOf(i, j), variantBoxes, plinths, antennas, footprints, beacons, density);
      // count logical buildings = number of plinths added this block
      buildingCount += plinths.length - beforeB;
      void before;
    }
  }
  fillPark(rng, trees);

  // --- textures ---
  const winUnit = quality === 'high' ? 28 : 14;
  const windowTex = VARIANT_CFG.map((cfg) =>
    makeWindowTexture({ ...cfg, unit: winUnit })
  );
  const roadTex = makeRoadTexture();
  roadTex.repeat.set(CITY_SPAN / DASH_WORLD, 1);
  const sidewalkTex = makeSidewalkTexture();
  sidewalkTex.repeat.set(BLOCK / 6, BLOCK / 6);
  const grassTex = makeGrassTexture();
  grassTex.repeat.set(BLOCK / 6, BLOCK / 6);

  const group = new THREE.Group();
  group.name = 'city';

  // --- building InstancedMeshes (one per variant) ---
  const windowMaterials = [];
  for (let v = 0; v < N_VAR; v++) {
    const list = variantBoxes[v];
    if (!list.length) continue;
    const geo = new THREE.BoxGeometry(1, 1, 1);
    const mat = patchInstancedUV(
      new THREE.MeshStandardMaterial({
        color: 0xffffff,
        map: windowTex[v].color,
        emissive: 0xffffff,
        emissiveMap: windowTex[v].emissive,
        emissiveIntensity: 0.0, // off in daylight; day/night system raises it at night
        roughness: 0.62,
        metalness: 0.12,
      })
    );
    windowMaterials.push(mat);
    const mesh = new THREE.InstancedMesh(geo, mat, list.length);
    mesh.castShadow = true;
    mesh.receiveShadow = true;
    const uvArr = new Float32Array(list.length * 2);
    for (let k = 0; k < list.length; k++) {
      const r = list[k];
      _dummy.position.set(r.px, r.py, r.pz);
      _dummy.scale.set(r.sx, r.sy, r.sz);
      _dummy.rotation.set(0, 0, 0);
      _dummy.updateMatrix();
      mesh.setMatrixAt(k, _dummy.matrix);
      mesh.setColorAt(k, _color.setRGB(r.r, r.g, r.b));
      uvArr[k * 2] = r.uvx;
      uvArr[k * 2 + 1] = r.uvy;
    }
    geo.setAttribute('aUvScale', new THREE.InstancedBufferAttribute(uvArr, 2));
    mesh.instanceMatrix.needsUpdate = true;
    if (mesh.instanceColor) mesh.instanceColor.needsUpdate = true;
    mesh.computeBoundingSphere();
    group.add(mesh);
  }

  // --- plinths (dark base, no texture) ---
  {
    const geo = new THREE.BoxGeometry(1, 1, 1);
    const mat = new THREE.MeshStandardMaterial({
      color: 0x202428,
      roughness: 0.9,
      metalness: 0.0,
    });
    const mesh = new THREE.InstancedMesh(geo, mat, plinths.length);
    mesh.castShadow = true;
    mesh.receiveShadow = true;
    const dark = new THREE.Color(0x202428);
    for (let k = 0; k < plinths.length; k++) {
      const p = plinths[k];
      _dummy.position.set(p.px, p.py, p.pz);
      _dummy.scale.set(p.sx, p.sy, p.sz);
      _dummy.rotation.set(0, 0, 0);
      _dummy.updateMatrix();
      mesh.setMatrixAt(k, _dummy.matrix);
      mesh.setColorAt(k, dark);
    }
    mesh.instanceMatrix.needsUpdate = true;
    mesh.computeBoundingSphere();
    group.add(mesh);
  }

  // --- antennas ---
  if (antennas.length) {
    const geo = new THREE.CylinderGeometry(0.08, 0.14, 1, 6);
    geo.translate(0, 0.5, 0); // base at origin so scale.y = height
    const mat = new THREE.MeshStandardMaterial({
      color: 0x2a2d33,
      roughness: 0.4,
      metalness: 0.8,
      emissive: 0xff3322,
      emissiveIntensity: 0.0,
    });
    const mesh = new THREE.InstancedMesh(geo, mat, antennas.length);
    mesh.castShadow = true;
    for (let k = 0; k < antennas.length; k++) {
      const a = antennas[k];
      _dummy.position.set(a.px, a.py, 0);
      _dummy.position.z = a.pz ?? 0;
      _dummy.scale.set(1, a.h, 1);
      _dummy.rotation.set(0, 0, 0);
      _dummy.updateMatrix();
      mesh.setMatrixAt(k, _dummy.matrix);
    }
    mesh.instanceMatrix.needsUpdate = true;
    mesh.computeBoundingSphere();
    group.add(mesh);
  }

  // --- roads (horizontal + vertical strips) ---
  // horizontal strips: length along X (geometry u along X after rotateX)
  const roadMat = new THREE.MeshStandardMaterial({
    map: roadTex,
    roughness: 0.95,
    metalness: 0.0,
    polygonOffset: true,
    polygonOffsetFactor: -1,
  });
  const lineCount = GRID + 1;
  // horizontal
  {
    const geo = new THREE.PlaneGeometry(1, 1);
    geo.rotateX(-Math.PI / 2);
    const mesh = new THREE.InstancedMesh(geo, roadMat, lineCount);
    mesh.receiveShadow = true;
    for (let k = 0; k < lineCount; k++) {
      const z = -CITY_SPAN / 2 + k * PITCH;
      _dummy.position.set(0, 0.02, z);
      _dummy.scale.set(CITY_SPAN, ROAD, 1);
      _dummy.rotation.set(0, 0, 0);
      _dummy.updateMatrix();
      mesh.setMatrixAt(k, _dummy.matrix);
    }
    mesh.instanceMatrix.needsUpdate = true;
    mesh.computeBoundingSphere();
    group.add(mesh);
  }
  // vertical: geometry u along Z (extra rotateY so dashes run along length)
  {
    const geo = new THREE.PlaneGeometry(1, 1);
    geo.rotateX(-Math.PI / 2);
    geo.rotateY(Math.PI / 2);
    const mesh = new THREE.InstancedMesh(geo, roadMat, lineCount);
    mesh.receiveShadow = true;
    for (let k = 0; k < lineCount; k++) {
      const x = -CITY_SPAN / 2 + k * PITCH;
      _dummy.position.set(x, 0.02, 0);
      _dummy.scale.set(CITY_SPAN, ROAD, 1);
      _dummy.rotation.set(0, 0, 0);
      _dummy.updateMatrix();
      mesh.setMatrixAt(k, _dummy.matrix);
    }
    mesh.instanceMatrix.needsUpdate = true;
    mesh.computeBoundingSphere();
    group.add(mesh);
  }

  // --- sidewalks (one box per non-park block) ---
  {
    const geo = new THREE.BoxGeometry(1, 1, 1);
    const mat = new THREE.MeshStandardMaterial({
      color: 0xffffff,
      map: sidewalkTex,
      roughness: 0.95,
      metalness: 0.0,
    });
    const padH = SIDEWALK_TOP;
    let count = 0;
    for (let i = 0; i < GRID; i++)
      for (let j = 0; j < GRID; j++) if (!isPark(i, j)) count++;
    const mesh = new THREE.InstancedMesh(geo, mat, count);
    mesh.receiveShadow = true;
    mesh.castShadow = false;
    let k = 0;
    for (let i = 0; i < GRID; i++) {
      for (let j = 0; j < GRID; j++) {
        if (isPark(i, j)) continue;
        const c = blockCenter(i, j);
        _dummy.position.set(c.x, padH / 2, c.z);
        _dummy.scale.set(BLOCK, padH, BLOCK);
        _dummy.rotation.set(0, 0, 0);
        _dummy.updateMatrix();
        mesh.setMatrixAt(k++, _dummy.matrix);
      }
    }
    mesh.instanceMatrix.needsUpdate = true;
    mesh.computeBoundingSphere();
    group.add(mesh);
  }

  // --- park pads + pond ---
  {
    const geo = new THREE.BoxGeometry(1, 1, 1);
    const mat = new THREE.MeshStandardMaterial({
      color: 0xffffff,
      map: grassTex,
      roughness: 1.0,
      metalness: 0.0,
    });
    const padH = 0.5;
    const mesh = new THREE.InstancedMesh(geo, mat, PARK_I.length * PARK_J.length);
    mesh.receiveShadow = true;
    let k = 0;
    for (const i of PARK_I)
      for (const j of PARK_J) {
        const c = blockCenter(i, j);
        _dummy.position.set(c.x, padH / 2, c.z);
        _dummy.scale.set(BLOCK, padH, BLOCK);
        _dummy.rotation.set(0, 0, 0);
        _dummy.updateMatrix();
        mesh.setMatrixAt(k++, _dummy.matrix);
      }
    mesh.instanceMatrix.needsUpdate = true;
    mesh.computeBoundingSphere();
    group.add(mesh);

    // pond on the center park block
    const center = blockCenter(PARK_I[1], PARK_J[1]);
    const pondGeo = new THREE.CircleGeometry(7, 28);
    pondGeo.rotateX(-Math.PI / 2);
    const pondMat = new THREE.MeshStandardMaterial({
      color: 0x2a5a8a,
      roughness: 0.15,
      metalness: 0.5,
      envMapIntensity: 1.0,
    });
    const pond = new THREE.Mesh(pondGeo, pondMat);
    pond.position.set(center.x, 0.42, center.z);
    pond.receiveShadow = true;
    group.add(pond);
  }

  // --- trees ---
  if (trees.length) {
    // trunks
    const trunkGeo = new THREE.CylinderGeometry(0.18, 0.26, 1, 6);
    trunkGeo.translate(0, 0.5, 0);
    const trunkMat = new THREE.MeshStandardMaterial({ color: 0x5a4326, roughness: 1.0 });
    const trunkMesh = new THREE.InstancedMesh(trunkGeo, trunkMat, trees.length);
    // canopies
    const canopyGeo = new THREE.IcosahedronGeometry(1, 0);
    const canopyMat = new THREE.MeshStandardMaterial({
      color: 0xffffff,
      flatShading: true,
      roughness: 0.9,
      metalness: 0.0,
    });
    const canopyMesh = new THREE.InstancedMesh(canopyGeo, canopyMat, trees.length);
    canopyMesh.castShadow = true;
    canopyMesh.receiveShadow = true;

    for (let k = 0; k < trees.length; k++) {
      const t = trees[k];
      const trunkH = 3 * t.s;
      _dummy.position.set(t.x, 0.5, t.z);
      _dummy.scale.set(1, trunkH, 1);
      _dummy.rotation.set(0, 0, 0);
      _dummy.updateMatrix();
      trunkMesh.setMatrixAt(k, _dummy.matrix);

      _dummy.position.set(t.x, trunkH + 1.6 * t.s, t.z);
      _dummy.scale.set(2.1 * t.s, 2.4 * t.s, 2.1 * t.s);
      _dummy.rotation.set(0, rng.range(0, Math.PI), 0);
      _dummy.updateMatrix();
      canopyMesh.setMatrixAt(k, _dummy.matrix);

      _color.setHSL(0.28 + t.hue, 0.45, 0.32);
      canopyMesh.setColorAt(k, _color);
    }
    trunkMesh.instanceMatrix.needsUpdate = true;
    canopyMesh.instanceMatrix.needsUpdate = true;
    if (canopyMesh.instanceColor) canopyMesh.instanceColor.needsUpdate = true;
    trunkMesh.computeBoundingSphere();
    canopyMesh.computeBoundingSphere();
    group.add(trunkMesh);
    group.add(canopyMesh);
  }

  // --- base ground plane with a radial fade to the horizon colour ---
  // The rim fades smoothly into the (day/night) horizon colour, so the ground
  // dissolves into the sky/fog with NO hard polygon edge from any angle.
  let groundUniforms = null;
  const groundMat = new THREE.MeshStandardMaterial({
    color: 0xffffff,
    roughness: 1.0,
    metalness: 0.0,
  });
  groundMat.onBeforeCompile = (shader) => {
    shader.uniforms.uHorizon = { value: new THREE.Color(0x86b4e0) };
    shader.uniforms.uGround = { value: new THREE.Color(0x14181e) };
    shader.uniforms.uFadeNear = { value: CITY_SPAN * 0.34 };
    shader.uniforms.uFadeFar = { value: CITY_SPAN * 0.78 };
    groundUniforms = shader.uniforms;
    shader.vertexShader = shader.vertexShader
      .replace('#include <common>', '#include <common>\nvarying vec2 vGroundXZ;')
      .replace(
        '#include <project_vertex>',
        '#include <project_vertex>\nvGroundXZ = (modelMatrix * vec4(position, 1.0)).xz;'
      );
    shader.fragmentShader = shader.fragmentShader
      .replace(
        '#include <common>',
        '#include <common>\nvarying vec2 vGroundXZ;\nuniform vec3 uHorizon;\nuniform vec3 uGround;\nuniform float uFadeNear;\nuniform float uFadeFar;'
      )
      .replace(
        '#include <color_fragment>',
        '#include <color_fragment>\n  { float gd = length(vGroundXZ); float gf = smoothstep(uFadeNear, uFadeFar, gd); diffuseColor.rgb = mix(uGround, uHorizon, gf); }'
      )
      // After lighting + fog are applied, blend the final pixel toward the pure
      // horizon colour at the rim. This makes the ground's far edge EXACTLY match
      // the sky dome's horizon colour (bypassing lighting), so there is no visible
      // polygon edge from any allowed camera angle.
      .replace(
        '#include <fog_fragment>',
        '#include <fog_fragment>\n  { float gd2 = length(vGroundXZ); float gf2 = smoothstep(uFadeFar * 0.7, uFadeFar * 1.35, gd2); gl_FragColor.rgb = mix(gl_FragColor.rgb, uHorizon, gf2); }'
      );
  };
  groundMat.customProgramCacheKey = () => 'groundFadeV2';
  function setHorizon(color) {
    if (groundUniforms) groundUniforms.uHorizon.value.copy(color);
  }
  {
    const geo = new THREE.PlaneGeometry(CITY_SPAN * 5, CITY_SPAN * 5);
    geo.rotateX(-Math.PI / 2);
    const ground = new THREE.Mesh(geo, groundMat);
    ground.position.y = -0.05;
    ground.receiveShadow = true;
    group.add(ground);
  }

  // --- street lamps along every road (poles + emissive heads + glow) ---
  // Deterministic sub-stream so lamp layout is reproducible per seed.
  const lampRng = rng.fork();
  const lampPos = [];
  const half = CITY_SPAN / 2;
  for (let k = 0; k <= GRID; k++) {
    const line = -half + k * PITCH; // road center coordinate
    for (let m = 0; m < GRID; m++) {
      const mid = -half + PITCH / 2 + m * PITCH; // mid-block (not at intersections)
      const side = m % 2 === 0 ? 1 : -1;
      const off = (ROAD / 2 + 1.2) * side;
      lampPos.push({ x: mid, z: line + off }); // along horizontal road (runs in X)
      lampPos.push({ x: line + off, z: mid }); // along vertical road (runs in Z)
    }
  }
  let lampHeadMaterial = null;
  let lampGlowMaterial = null;
  if (lampPos.length) {
    const glowTex = makeGlowTexture();
    // poles
    const poleGeo = new THREE.CylinderGeometry(0.12, 0.16, 5, 6);
    poleGeo.translate(0, 2.5, 0);
    const poleMat = new THREE.MeshStandardMaterial({ color: 0x2a2d33, roughness: 0.7, metalness: 0.5 });
    const poles = new THREE.InstancedMesh(poleGeo, poleMat, lampPos.length);
    poles.castShadow = true;
    // heads (emissive, driven by day/night)
    const headGeo = new THREE.BoxGeometry(0.5, 0.35, 0.5);
    lampHeadMaterial = new THREE.MeshStandardMaterial({
      color: 0x1a1a1a,
      emissive: 0xffcf8a,
      emissiveIntensity: 0.0, // day/night raises it at night
      roughness: 0.6,
      metalness: 0.2,
    });
    const heads = new THREE.InstancedMesh(headGeo, lampHeadMaterial, lampPos.length);
    // glow sprites (additive warm), opacity driven by day/night
    const glowGeo = new THREE.PlaneGeometry(1, 1);
    lampGlowMaterial = new THREE.MeshBasicMaterial({
      map: glowTex,
      color: 0xffcf8a,
      transparent: true,
      opacity: 0.0,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
      fog: false,
    });
    const glows = new THREE.InstancedMesh(glowGeo, lampGlowMaterial, lampPos.length);
    for (let k = 0; k < lampPos.length; k++) {
      const p = lampPos[k];
      _dummy.position.set(p.x, 0, p.z);
      _dummy.scale.set(1, 1, 1);
      _dummy.rotation.set(0, 0, 0);
      _dummy.updateMatrix();
      poles.setMatrixAt(k, _dummy.matrix);
      _dummy.position.set(p.x, 5.0, p.z);
      _dummy.updateMatrix();
      heads.setMatrixAt(k, _dummy.matrix);
      _dummy.position.set(p.x, 5.0, p.z);
      _dummy.scale.set(6, 6, 1);
      _dummy.rotation.set(0, 0, 0);
      _dummy.updateMatrix();
      glows.setMatrixAt(k, _dummy.matrix);
      _dummy.scale.set(1, 1, 1);
    }
    poles.instanceMatrix.needsUpdate = true;
    heads.instanceMatrix.needsUpdate = true;
    glows.instanceMatrix.needsUpdate = true;
    poles.computeBoundingSphere();
    heads.computeBoundingSphere();
    glows.computeBoundingSphere();
    group.add(poles);
    group.add(heads);
    group.add(glows);
  }

  // --- aviation beacons (small red blinking glows on the tallest towers) ---
  let beaconSprites = [];
  let beaconMaterial = null;
  if (beacons.length) {
    const beaconTex = makeGlowTexture();
    beaconMaterial = new THREE.SpriteMaterial({
      map: beaconTex,
      color: 0xff2a18,
      transparent: true,
      opacity: 0.9,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
      depthTest: true,
      fog: false,
    });
    for (const b of beacons) {
      const s = new THREE.Sprite(beaconMaterial);
      s.position.set(b.x, b.y, b.z);
      s.scale.set(7, 7, 1);
      group.add(s);
      beaconSprites.push(s);
    }
  }

  // --- roads descriptor (for the traffic system) ---
  const lines = [];
  for (let k = 0; k <= GRID; k++) {
    const v = -CITY_SPAN / 2 + k * PITCH;
    lines.push({ axis: 'x', value: v });
    lines.push({ axis: 'z', value: v });
  }
  const roads = { pitch: PITCH, grid: GRID, citySpan: CITY_SPAN, road: ROAD, lines };

  // --- disposal (frees GPU resources on regenerate) ---
  const allTextures = [
    roadTex,
    sidewalkTex,
    grassTex,
    ...windowTex.flatMap((t) => [t.color, t.emissive]),
  ];
  function dispose() {
    group.traverse((obj) => {
      if (obj.isMesh) {
        obj.geometry.dispose();
        const m = obj.material;
        if (Array.isArray(m)) m.forEach((mm) => mm.dispose());
        else m.dispose();
      }
    });
    for (const t of allTextures) t.dispose();
  }

  return {
    group,
    buildingCount,
    treeCount: trees.length,
    windowMaterials,
    lampHeadMaterial,
    lampGlowMaterial,
    beaconSprites,
    beaconMaterial,
    footprints,
    citySpan: CITY_SPAN,
    pitch: PITCH,
    grid: GRID,
    setHorizon,
    roads,
    dispose,
    currentSeed: seed >>> 0,
  };
}
