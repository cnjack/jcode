// map.js — selectable circuit-board layouts: enemy trace, build pads, CPU & spawn.
// The active map's structures are mutated in place by loadMap(); exported names
// are `let` so importers see the current map via ES module live bindings.

import { cellCenter, inBounds, lerp, dist, COLS, ROWS, GRID_X, GRID_Y, CELL } from './utils.js';

// ---- map data definitions (waypoint cells + cpu cell) ----
export const MAPS = {
  serpentine: {
    id: 'serpentine', name: 'Serpentine', blurb: 'classic winding S-trace, full board',
    cpu: [18, 9],
    cells: [[-1, 1], [17, 1], [17, 3], [3, 3], [3, 5], [17, 5], [17, 7], [3, 7], [3, 9], [18, 9]],
  },
  dualbus: {
    id: 'dualbus', name: 'Dual Bus', blurb: 'three long parallel runs, layered bus',
    cpu: [18, 8],
    cells: [[-1, 2], [18, 2], [18, 5], [2, 5], [2, 8], [18, 8]],
  },
  grid: {
    id: 'grid', name: 'The Grid', blurb: 'tight compact zig-zag, denser & faster',
    cpu: [18, 9],
    cells: [[-1, 1], [18, 1], [18, 2], [1, 2], [1, 3], [18, 3], [18, 4], [1, 4], [1, 5], [18, 5], [18, 6], [1, 6], [1, 7], [18, 7], [18, 8], [1, 8], [1, 9], [18, 9]],
  },
};
export const MAP_ORDER = ['serpentine', 'dualbus', 'grid'];

// ---- current map state (rebuilt by loadMap) ----
export let currentMapId = 'serpentine';
export let WAYPOINT_CELLS = MAPS.serpentine.cells;
export let WAYPOINTS = [];
export let SEGMENTS = [];
export let PATH_LENGTH = 0;
export let SPAWN_POINT = { x: 0, y: 0 };
export let CPU_CELL = { gx: 18, gy: 9 };
export let CPU_POINT = { x: 0, y: 0 };

export const PADS = [];
let padIndex = new Map();
let pathSet = new Set();

export function loadMap(id) {
  const def = MAPS[id] || MAPS.serpentine;
  currentMapId = id;
  WAYPOINT_CELLS = def.cells;
  WAYPOINTS = def.cells.map(([gx, gy]) => cellCenter(gx, gy));

  // cumulative segments
  SEGMENTS.length = 0;
  let cum = 0;
  for (let i = 0; i < WAYPOINTS.length - 1; i++) {
    const a = WAYPOINTS[i], b = WAYPOINTS[i + 1];
    const len = dist(a.x, a.y, b.x, b.y);
    const ang = Math.atan2(b.y - a.y, b.x - a.x);
    SEGMENTS.push({ a, b, len, ang, start: cum, end: cum + len });
    cum += len;
  }
  PATH_LENGTH = cum;
  SPAWN_POINT = WAYPOINTS[0];
  CPU_CELL = { gx: def.cpu[0], gy: def.cpu[1] };
  CPU_POINT = cellCenter(CPU_CELL.gx, CPU_CELL.gy);

  // path cell set
  pathSet = new Set();
  for (let i = 0; i < WAYPOINT_CELLS.length - 1; i++) {
    let [c, r] = WAYPOINT_CELLS[i];
    const [c2, r2] = WAYPOINT_CELLS[i + 1];
    const dc = Math.sign(c2 - c), dr = Math.sign(r2 - r);
    if (inBounds(c, r)) pathSet.add(c + ',' + r);
    while (c !== c2 || r !== r2) {
      c += dc; r += dr;
      if (inBounds(c, r)) pathSet.add(c + ',' + r);
    }
  }

  // pads: non-path cells orthogonally adjacent to a path cell
  const padMap = new Map();
  const dirs = [[1, 0], [-1, 0], [0, 1], [0, -1]];
  for (const key of pathSet) {
    const [c, r] = key.split(',').map(Number);
    for (const [dc, dr] of dirs) {
      const nc = c + dc, nr = r + dr;
      if (!inBounds(nc, nr)) continue;
      const k = nc + ',' + nr;
      if (pathSet.has(k) || padMap.has(k)) continue;
      const ctr = cellCenter(nc, nr);
      padMap.set(k, { gx: nc, gy: nr, x: ctr.x, y: ctr.y, occupied: false });
    }
  }
  PADS.length = 0;
  for (const p of padMap.values()) PADS.push(p);
  padIndex = new Map(PADS.map((p) => [p.gx + ',' + p.gy, p]));
  return def;
}

// initial map
loadMap('serpentine');

export function pointAtDistance(d) {
  if (d <= 0) {
    const s = SEGMENTS[0];
    return { x: s.a.x, y: s.a.y, angle: s.ang };
  }
  if (d >= PATH_LENGTH) {
    const s = SEGMENTS[SEGMENTS.length - 1];
    return { x: s.b.x, y: s.b.y, angle: s.ang };
  }
  for (let i = 0; i < SEGMENTS.length; i++) {
    const s = SEGMENTS[i];
    if (d <= s.end) {
      const t = (d - s.start) / s.len;
      return { x: lerp(s.a.x, s.b.x, t), y: lerp(s.a.y, s.b.y, t), angle: s.ang };
    }
  }
  const s = SEGMENTS[SEGMENTS.length - 1];
  return { x: s.b.x, y: s.b.y, angle: s.ang };
}

export function isPathCell(gx, gy) { return pathSet.has(gx + ',' + gy); }

export function getPad(gx, gy) { return padIndex.get(gx + ',' + gy) || null; }
export function isBuildable(gx, gy) { const p = getPad(gx, gy); return !!(p && !p.occupied); }
export function resetPads() { for (const p of PADS) p.occupied = false; }

export { GRID_X, GRID_Y, CELL, COLS, ROWS };
