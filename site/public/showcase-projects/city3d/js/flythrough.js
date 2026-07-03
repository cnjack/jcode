// js/flythrough.js
// Cinematic flythrough mode. The camera follows a smooth closed Catmull-Rom
// spline circuit that weaves through downtown street canyons and skims over
// rooftops, with gentle look-ahead. The path is built relative to the city
// size and VALIDATED against building footprints/heights: any waypoint or
// sampled point that would clip a building is nudged up, so the camera never
// flies through geometry.
//
// Verification API (works in a hidden tab, no rAF):
//   window.__city.startFlythrough() / stopFlythrough()
//   window.__city.seekFlythrough(t)   // t in 0..1: place + render one frame
//   window.__city.isFlythrough()

import * as THREE from 'three';

const _pos = new THREE.Vector3();
const _look = new THREE.Vector3();
const _tan = new THREE.Vector3();
const _up = new THREE.Vector3(0, 1, 0);

// Clearance margin (camera stays this far above any building it passes over).
const MARGIN = 9;

export class Flythrough {
  constructor(camera, controls) {
    this.camera = camera;
    this.controls = controls;
    this.active = false;
    this.t = 0; // 0..1 along the closed curve
    this.curve = null;
    this.length = 1;
    this.lapSeconds = 60; // ~45-75s per lap target
    this.footprints = [];
    this._prevPos = new THREE.Vector3();
    this._saved = null;
  }

  // Build a fresh spline for the given city (called on rebuild/regenerate).
  build(cityState) {
    if (!cityState || !cityState.citySpan) {
      this.curve = null;
      return;
    }
    const span = cityState.citySpan;
    const half = span / 2;
    const pitch = cityState.pitch || 35;
    this.footprints = cityState.footprints || [];
    // road line coordinate for grid index k
    const line = (k) => -half + k * pitch;

    // Hand-picked circuit relative to city size: low street canyons + high
    // rooftop crossings. All low points sit over road centerlines (clear of
    // buildings); high points clear the tallest towers. Validation nudges any
    // remaining offenders up.
    const low = 11;
    const high = span * 0.32; // ~155, clears the tallest towers + antennas
    const mid = span * 0.26;
    const base = [
      [line(3), low, line(1)],
      [line(3), low, line(5)],
      [line(5), mid, line(7)],
      [line(7), high, line(8)],
      [line(10), mid, line(10)],
      [line(12), low, line(11)],
      [line(12), low, line(6)],
      [line(11), mid, line(4)],
      [line(8), high, line(2)],
      [line(5), mid, line(1)],
      [line(2), low, line(3)],
      [line(1), low, line(6)],
    ].map((p) => new THREE.Vector3(p[0], p[1], p[2]));

    this.curve = this._buildValidated(base);
    this.length = this.curve.getLength() || 1;
  }

  // Build a closed Catmull-Rom curve and iteratively raise waypoints so no
  // sampled point clips a building. Returns the collision-free curve.
  _buildValidated(points) {
    const pts = points.map((p) => p.clone());
    const SAMPLES = 520;
    for (let pass = 0; pass < 6; pass++) {
      const curve = new THREE.CatmullRomCurve3(pts, true, 'catmullrom', 0.5);
      let worstDeficit = 0;
      const raises = pts.map(() => 0);
      for (let i = 0; i < SAMPLES; i++) {
        const u = i / SAMPLES;
        curve.getPointAt(u, _pos);
        const need = this._clearance(_pos.x, _pos.z) + MARGIN;
        const deficit = need - _pos.y;
        if (deficit > 0.01) {
          worstDeficit = Math.max(worstDeficit, deficit);
          // credit the two nearest control points by inverse distance.
          let bi = 0, bd = Infinity, bi2 = 1, bd2 = Infinity;
          for (let k = 0; k < pts.length; k++) {
            const d = pts[k].distanceToSquared(_pos);
            if (d < bd) { bd2 = bd; bi2 = bi; bd = d; bi = k; }
            else if (d < bd2) { bd2 = d; bi2 = k; }
          }
          raises[bi] = Math.max(raises[bi], deficit + 1.5);
          raises[bi2] = Math.max(raises[bi2], (deficit + 1.5) * 0.5);
        }
      }
      let changed = false;
      for (let k = 0; k < pts.length; k++) {
        if (raises[k] > 0) { pts[k].y += raises[k]; changed = true; }
      }
      if (!changed) {
        // Final verification: warn if anything still clips (shouldn't happen).
        let clips = 0;
        for (let i = 0; i < SAMPLES; i++) {
          curve.getPointAt(i / SAMPLES, _pos);
          if (_pos.y < this._clearance(_pos.x, _pos.z) + MARGIN - 0.5) clips++;
        }
        if (clips > 0) console.warn('[flythrough]', clips, 'sample points still clip buildings');
        return curve;
      }
      void worstDeficit;
    }
    return new THREE.CatmullRomCurve3(pts, true, 'catmullrom', 0.5);
  }

  // Max building top whose footprint contains (x,z); 0 if over a road.
  _clearance(x, z) {
    let top = 0;
    const fp = this.footprints;
    for (let i = 0; i < fp.length; i++) {
      const f = fp[i];
      const dx = Math.abs(x - f.x);
      const dz = Math.abs(z - f.z);
      if (dx <= f.hw && dz <= f.hd && f.top > top) top = f.top;
    }
    return top;
  }

  isActive() {
    return this.active;
  }

  start() {
    if (!this.curve) return false;
    if (this.active) return true;
    this.active = true;
    // save orbit state for restore
    this._saved = {
      enabled: this.controls.enabled,
      pos: this.camera.position.clone(),
      target: this.controls.target.clone(),
      up: this.camera.up.clone(),
    };
    this.controls.enabled = false;
    this.controls.autoRotate = false;
    this.t = 0;
    return true;
  }

  stop() {
    if (!this.active) return;
    this.active = false;
    // restore orbit controls at the current camera position/target
    this.camera.up.set(0, 1, 0);
    if (this._saved) {
      this.controls.enabled = this._saved.enabled;
    } else {
      this.controls.enabled = true;
    }
    this.controls.target.copy(this._lookTargetAt(this.t));
    this.controls.update();
    this._saved = null;
  }

  // Place camera at fraction t and orient toward the look-ahead point.
  _applyAt(t) {
    if (!this.curve) return;
    const u = ((t % 1) + 1) % 1;
    this.curve.getPointAt(u, _pos);
    // look slightly ahead along the path
    const lu = (u + 0.045) % 1;
    this.curve.getPointAt(lu, _look);
    _look.y = _look.y + 2.5; // bias the gaze a touch upward for skyline framing

    // subtle banking from horizontal curvature
    this.curve.getTangentAt(u, _tan);
    const curv = Math.abs(_tan.x) * 0.0 + 1; // placeholder
    void curv;

    this.camera.position.copy(_pos);
    this.camera.up.set(0, 1, 0);
    this.camera.lookAt(_look);
  }

  _lookTargetAt(t) {
    if (!this.curve) return new THREE.Vector3(0, 45, 0);
    const u = ((t % 1) + 1) % 1;
    const lu = (u + 0.045) % 1;
    const p = this.curve.getPointAt(lu);
    p.y += 2.5;
    return p;
  }

  update(dtMs) {
    if (!this.active || !this.curve) return;
    const dt = dtMs / 1000;
    this.t = (this.t + dt / this.lapSeconds) % 1;
    this._applyAt(this.t);
  }

  // Verification: place camera exactly at fraction t (optionally render).
  seek(t, renderFn) {
    if (!this.curve) return;
    if (this.active) this.t = ((t % 1) + 1) % 1;
    this._applyAt(t);
    if (typeof renderFn === 'function') renderFn();
  }

  dispose() {
    this.curve = null;
    this.footprints = [];
  }
}
