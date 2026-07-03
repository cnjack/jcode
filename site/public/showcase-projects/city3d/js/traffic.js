// js/traffic.js
// Deterministic instanced traffic. Cars travel in two opposing lanes per road
// (one each direction), wrap at the fogged city edges, and show headlights
// (white, front) + taillights (red, rear) that fade in at night.
//
// Everything is InstancedMesh (body + headlight glow + taillight glow) and the
// per-frame update performs no allocations. Same seed -> identical layout.

import * as THREE from 'three';
import { createRNG } from './rng.js';
import { makeGlowTexture } from './textures.js';

const _dummy = new THREE.Object3D();
const _color = new THREE.Color();

// Paint palette (deterministic per index).
const PAINT = [0xcc2a2a, 0x2a4fcc, 0xd8d8d8, 0x3a3a3a, 0x2aa050, 0xcc8a2a, 0x882acc, 0xddddaa];

// Merge several BoxGeometry into one BufferGeometry by hand (three r160 core
// has no BufferGeometryUtils merged in this vendored build).
function mergeBoxes(boxes) {
  let vCount = 0;
  let iCount = 0;
  for (const b of boxes) {
    vCount += b.geometry.attributes.position.count;
    iCount += b.geometry.index ? b.geometry.index.count : b.geometry.attributes.position.count;
  }
  const pos = new Float32Array(vCount * 3);
  const nor = new Float32Array(vCount * 3);
  const uv = new Float32Array(vCount * 2);
  const idx = new Uint32Array(iCount);
  let pOff = 0;
  let iOff = 0;
  let vOff = 0;
  for (const b of boxes) {
    const g = b.geometry;
    pos.set(g.attributes.position.array, pOff * 3);
    nor.set(g.attributes.normal.array, pOff * 3);
    uv.set(g.attributes.uv.array, pOff * 2);
    const gi = g.index ? g.index.array : null;
    if (gi) {
      for (let k = 0; k < gi.length; k++) idx[iOff++] = gi[k] + vOff;
    } else {
      for (let k = 0; k < g.attributes.position.count; k++) idx[iOff++] = k + vOff;
    }
    vOff += g.attributes.position.count;
    pOff += g.attributes.position.count;
    g.dispose();
  }
  const m = new THREE.BufferGeometry();
  m.setAttribute('position', new THREE.BufferAttribute(pos, 3));
  m.setAttribute('normal', new THREE.BufferAttribute(nor, 3));
  m.setAttribute('uv', new THREE.BufferAttribute(uv, 2));
  m.setIndex(new THREE.BufferAttribute(idx, 1));
  return m;
}

// Two-box car silhouette: body + cabin. Forward axis = +Z. Base at y=0.
function makeCarGeometry() {
  const body = new THREE.BoxGeometry(0.9, 0.5, 1.9);
  body.translate(0, 0.35, 0);
  const cabin = new THREE.BoxGeometry(0.78, 0.42, 0.95);
  cabin.translate(0, 0.78, -0.05);
  return mergeBoxes([{ geometry: body }, { geometry: cabin }]);
}

// Build the lane list from the roads descriptor. Two opposing lanes per road.
function buildLanes(roads) {
  const off = roads.road / 4;
  const lanes = [];
  for (const line of roads.lines) {
    if (line.axis === 'z') {
      // road runs along X (constant z); offset in Z, travel ±X
      lanes.push({ along: 'x', perp: line.value, off: +off, dir: +1 });
      lanes.push({ along: 'x', perp: line.value, off: -off, dir: -1 });
    } else {
      // road runs along Z (constant x); offset in X, travel ±Z
      lanes.push({ along: 'z', perp: line.value, off: +off, dir: +1 });
      lanes.push({ along: 'z', perp: line.value, off: -off, dir: -1 });
    }
  }
  return lanes;
}

export function createTraffic(seed, roads, density = 0.6) {
  const group = new THREE.Group();
  group.name = 'traffic';

  const lanes = buildLanes(roads);
  const len = roads.citySpan;
  const half = len / 2;
  const carY = 0.05;

  // shared geometry + materials
  const bodyGeo = makeCarGeometry();
  const glowGeo = new THREE.PlaneGeometry(1, 1);
  const glowTex = makeGlowTexture();
  const bodyMat = new THREE.MeshStandardMaterial({ roughness: 0.5, metalness: 0.35 });
  const headMat = new THREE.MeshBasicMaterial({
    map: glowTex,
    color: 0xfff4e0,
    transparent: true,
    opacity: 0,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
    fog: false,
  });
  const tailMat = new THREE.MeshBasicMaterial({
    map: glowTex,
    color: 0xff2a18,
    transparent: true,
    opacity: 0,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
    fog: false,
  });

  let bodyMesh = null;
  let headMesh = null;
  let tailMesh = null;
  let cars = null; // { n, laneIdx:Uint16Array, dist:Float32Array, speed:Float32Array }
  let carN = 0;

  function rebuild(d) {
    d = Math.max(0, Math.min(1, d));
    // dispose previous instanced meshes (keep shared geo/materials)
    if (bodyMesh) { group.remove(bodyMesh); bodyMesh.dispose(); }
    if (headMesh) { group.remove(headMesh); headMesh.dispose(); }
    if (tailMesh) { group.remove(tailMesh); tailMesh.dispose(); }

    const n = Math.max(0, Math.min(180, Math.round(lanes.length * 2.5 * d)));
    carN = n;
    if (n === 0) { cars = { n: 0, laneIdx: new Uint16Array(0), dist: new Float32Array(0), speed: new Float32Array(0) }; return; }

    const rng = createRNG(((seed >>> 0) ^ 0x9e3779b9) >>> 0);
    const laneIdx = new Uint16Array(n);
    const dist = new Float32Array(n);
    const speed = new Float32Array(n);
    for (let i = 0; i < n; i++) {
      laneIdx[i] = Math.floor(rng.next() * lanes.length);
      dist[i] = rng.next() * len;
      speed[i] = rng.range(6, 14);
    }
    cars = { n, laneIdx, dist, speed };

    bodyMesh = new THREE.InstancedMesh(bodyGeo, bodyMat, n);
    bodyMesh.castShadow = true;
    bodyMesh.receiveShadow = true;
    bodyMesh.instanceMatrix.setUsage(THREE.DynamicDrawUsage);
    for (let i = 0; i < n; i++) bodyMesh.setColorAt(i, _color.set(PAINT[i % PAINT.length]));
    if (bodyMesh.instanceColor) bodyMesh.instanceColor.needsUpdate = true;

    headMesh = new THREE.InstancedMesh(glowGeo, headMat, n);
    headMesh.instanceMatrix.setUsage(THREE.DynamicDrawUsage);
    tailMesh = new THREE.InstancedMesh(glowGeo, tailMat, n);
    tailMesh.instanceMatrix.setUsage(THREE.DynamicDrawUsage);

    group.add(bodyMesh, headMesh, tailMesh);
  }

  rebuild(density);

  // Deterministic edge-respawn RNG (so the layout stays reproducible per seed;
  // never use Math.random() — that would break same-seed determinism).
  let _rr = ((seed >>> 0) ^ 0x85ebca6b) >>> 0;
  function rrnd() {
    _rr = (Math.imul(_rr ^ (_rr >>> 15), 0x2c1b3c6d) + 0x9e3779b1) >>> 0;
    return _rr / 4294967296;
  }

  function update(dtMs, camera) {
    if (!cars || cars.n === 0) return;
    const dt = dtMs / 1000;
    const camQuat = camera.quaternion;
    const { n, laneIdx, dist, speed } = cars;
    for (let i = 0; i < n; i++) {
      let d = dist[i] + speed[i] * dt;
      if (d > len) {
        d -= len;
        // occasional invisible respawn on a different lane (edge is fogged).
        // Uses the deterministic stream so this never breaks same-seed parity.
        if (rrnd() < 0.2) laneIdx[i] = (rrnd() * lanes.length) | 0;
      }
      dist[i] = d;
      const ln = lanes[laneIdx[i]];
      let x, z, yaw;
      if (ln.along === 'x') {
        const aw = ln.dir > 0 ? -half + d : half - d;
        x = aw;
        z = ln.perp + ln.off;
        yaw = ln.dir > 0 ? Math.PI / 2 : -Math.PI / 2;
      } else {
        const aw = ln.dir > 0 ? -half + d : half - d;
        x = ln.perp + ln.off;
        z = aw;
        yaw = ln.dir > 0 ? 0 : Math.PI;
      }
      const fx = Math.sin(yaw);
      const fz = Math.cos(yaw);

      // body
      _dummy.position.set(x, carY, z);
      _dummy.scale.set(1, 1, 1);
      _dummy.rotation.set(0, yaw, 0);
      _dummy.quaternion.setFromEuler(_dummy.rotation);
      _dummy.updateMatrix();
      bodyMesh.setMatrixAt(i, _dummy.matrix);

      // headlight glow (front) — billboard toward camera
      _dummy.position.set(x + fx * 0.95, 0.5, z + fz * 0.95);
      _dummy.scale.set(0.55, 0.55, 1);
      _dummy.quaternion.copy(camQuat);
      _dummy.updateMatrix();
      headMesh.setMatrixAt(i, _dummy.matrix);

      // taillight glow (rear)
      _dummy.position.set(x - fx * 0.95, 0.5, z - fz * 0.95);
      _dummy.scale.set(0.4, 0.4, 1);
      _dummy.quaternion.copy(camQuat);
      _dummy.updateMatrix();
      tailMesh.setMatrixAt(i, _dummy.matrix);
    }
    bodyMesh.instanceMatrix.needsUpdate = true;
    headMesh.instanceMatrix.needsUpdate = true;
    tailMesh.instanceMatrix.needsUpdate = true;
  }

  function setNight(factor) {
    const f = Math.max(0, Math.min(1, factor));
    headMat.opacity = f;
    tailMat.opacity = f * 0.9;
  }

  function setDensity(d) {
    rebuild(d);
  }

  function dispose() {
    if (bodyMesh) bodyMesh.dispose();
    if (headMesh) headMesh.dispose();
    if (tailMesh) tailMesh.dispose();
    bodyGeo.dispose();
    glowGeo.dispose();
    bodyMat.dispose();
    headMat.dispose();
    tailMat.dispose();
  }

  return {
    group,
    update,
    setNight,
    setDensity,
    dispose,
    get carCount() {
      return carN;
    },
  };
}
