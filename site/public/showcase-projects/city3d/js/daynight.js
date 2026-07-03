// js/daynight.js
// Day/night cycle system. Owns the sky dome, sun + moon (visible discs and the
// directional lights), stars, hemisphere + ambient light, fog and tone-mapping
// exposure — all driven by a continuous time-of-day value in [0,24) hours.
//
// It also drives "night" features on the current city + traffic (building
// window emissives, street-lamp glow, car lights) via the references set with
// setCity() / setTraffic().
//
// The sky/fog gradient is sampled from an hour keyframe table; everything
// (colours, intensities, sun direction, star/window/lamp factors) interpolates
// smoothly through dawn → day → dusk → night.

import * as THREE from 'three';
import { makeGlowTexture, makeStarTexture, makeCloudTexture } from './textures.js';
import { createRNG } from './rng.js';

// Hour keyframes. Colours as hex; the sampler builds THREE.Color targets.
//   win  = building window emissive factor (0 day .. 1 night)
//   lamp = lamp + car-light factor         (0 day .. 1 night)
const KEYS = [
  { h: 0.0,  zen: 0x0a1838, hor: 0x162a4e, glow: 0x000000, gs: 0.0,  fog: 0x162a4e, fd: 0.0013, sun: 0xffb070, si: 0.0,  mi: 0.30, hs: 0x1a2238, hg: 0x05060a, hi: 0.20, ac: 0x0a0e18, ai: 0.35, star: 1.0, win: 1.0,  lamp: 1.0, exp: 1.05, sg: 0.0 },
  { h: 5.0,  zen: 0x101a3c, hor: 0x2c1832, glow: 0x5a2a3a, gs: 0.35, fog: 0x1c1426, fd: 0.0016, sun: 0xff9a5a, si: 0.15, mi: 0.20, hs: 0x2a3358, hg: 0x1a1418, hi: 0.26, ac: 0x20283a, ai: 0.40, star: 0.6, win: 0.85, lamp: 0.7, exp: 1.05, sg: 0.2 },
  { h: 6.5,  zen: 0x2a3a78, hor: 0xd98a5a, glow: 0xff8a4a, gs: 0.8,  fog: 0xc08866, fd: 0.0016, sun: 0xff9a5a, si: 0.9,  mi: 0.05, hs: 0x6a78a8, hg: 0x4a3a30, hi: 0.45, ac: 0x5a5868, ai: 0.55, star: 0.2, win: 0.45, lamp: 0.25, exp: 1.1, sg: 0.6 },
  { h: 8.5,  zen: 0x2358a8, hor: 0xa8c8ec, glow: 0xffd8b0, gs: 0.25, fog: 0xa8c8ec, fd: 0.0009, sun: 0xfff0d8, si: 2.6,  mi: 0.0,  hs: 0xaac4ff, hg: 0x554534, hi: 0.62, ac: 0x687088, ai: 0.55, star: 0.0, win: 0.03, lamp: 0.0, exp: 1.0,  sg: 0.28 },
  { h: 12.0, zen: 0x184f9e, hor: 0x86b4e0, glow: 0xfff0e0, gs: 0.12, fog: 0x86b4e0, fd: 0.0008, sun: 0xfff0d0, si: 3.0,  mi: 0.0,  hs: 0xbcd0ff, hg: 0x504534, hi: 0.72, ac: 0x707888, ai: 0.6,  star: 0.0, win: 0.0,  lamp: 0.0, exp: 0.98, sg: 0.24 },
  { h: 16.0, zen: 0x1f50a4, hor: 0xaab4cc, glow: 0xffd0a0, gs: 0.35, fog: 0xc2bcb0, fd: 0.0010, sun: 0xffe2b8, si: 2.85, mi: 0.0,  hs: 0xb0c4f0, hg: 0x54402c, hi: 0.62, ac: 0x6a6478, ai: 0.6,  star: 0.0, win: 0.03, lamp: 0.0, exp: 1.02, sg: 0.38 },
  { h: 18.0, zen: 0x3a2a72, hor: 0xe07a4a, glow: 0xff6a30, gs: 0.85, fog: 0xc87a5a, fd: 0.0016, sun: 0xff7a44, si: 1.0,  mi: 0.08, hs: 0x7068a0, hg: 0x4a3024, hi: 0.45, ac: 0x604858, ai: 0.55, star: 0.2, win: 0.5,  lamp: 0.3, exp: 1.12, sg: 0.7 },
  { h: 19.5, zen: 0x141440, hor: 0x5a2c4a, glow: 0x9a2a40, gs: 0.45, fog: 0x40243a, fd: 0.0016, sun: 0xff6a44, si: 0.12, mi: 0.22, hs: 0x2c2c58, hg: 0x14101a, hi: 0.28, ac: 0x22182a, ai: 0.42, star: 0.65, win: 0.88, lamp: 0.8, exp: 1.08, sg: 0.25 },
  { h: 21.0, zen: 0x0a1838, hor: 0x162a4e, glow: 0x000000, gs: 0.0,  fog: 0x162a4e, fd: 0.0013, sun: 0xffb070, si: 0.0,  mi: 0.30, hs: 0x1a2238, hg: 0x05060a, hi: 0.20, ac: 0x0a0e18, ai: 0.35, star: 1.0, win: 1.0,  lamp: 1.0, exp: 1.05, sg: 0.0 },
  { h: 24.0, zen: 0x0a1838, hor: 0x162a4e, glow: 0x000000, gs: 0.0,  fog: 0x162a4e, fd: 0.0013, sun: 0xffb070, si: 0.0,  mi: 0.30, hs: 0x1a2238, hg: 0x05060a, hi: 0.20, ac: 0x0a0e18, ai: 0.35, star: 1.0, win: 1.0,  lamp: 1.0, exp: 1.05, sg: 0.0 },
];

// Reusable scratch colours (avoid per-frame allocation).
const cZen = new THREE.Color();
const cHor = new THREE.Color();
const cGlow = new THREE.Color();
const cFog = new THREE.Color();
const cSun = new THREE.Color();
const cHemiS = new THREE.Color();
const cHemiG = new THREE.Color();
const cAmb = new THREE.Color();
const _ca = new THREE.Color();
const _cb = new THREE.Color();

function lerp(a, b, t) {
  return a + (b - a) * t;
}

// Sample all keyframed channels for a given hour into the scratch targets.
function sampleChannels(h) {
  // find bracketing keys
  let k0 = KEYS[0];
  let k1 = KEYS[KEYS.length - 1];
  for (let i = 0; i < KEYS.length - 1; i++) {
    if (h >= KEYS[i].h && h <= KEYS[i + 1].h) {
      k0 = KEYS[i];
      k1 = KEYS[i + 1];
      break;
    }
  }
  const span = k1.h - k0.h || 1;
  const t = Math.max(0, Math.min(1, (h - k0.h) / span));

  cZen.setHex(k0.zen).lerp(_cb.setHex(k1.zen), t);
  cHor.setHex(k0.hor).lerp(_cb.setHex(k1.hor), t);
  cGlow.setHex(k0.glow).lerp(_cb.setHex(k1.glow), t);
  cFog.setHex(k0.fog).lerp(_cb.setHex(k1.fog), t);
  cSun.setHex(k0.sun).lerp(_cb.setHex(k1.sun), t);
  cHemiS.setHex(k0.hs).lerp(_cb.setHex(k1.hs), t);
  cHemiG.setHex(k0.hg).lerp(_cb.setHex(k1.hg), t);
  cAmb.setHex(k0.ac).lerp(_cb.setHex(k1.ac), t);

  return {
    zen: cZen,
    hor: cHor,
    glow: cGlow,
    gs: lerp(k0.gs, k1.gs, t),
    fog: cFog,
    fd: lerp(k0.fd, k1.fd, t),
    sun: cSun,
    si: lerp(k0.si, k1.si, t),
    mi: lerp(k0.mi, k1.mi, t),
    hs: cHemiS,
    hg: cHemiG,
    hi: lerp(k0.hi, k1.hi, t),
    ac: cAmb,
    ai: lerp(k0.ai, k1.ai, t),
    star: lerp(k0.star, k1.star, t),
    win: lerp(k0.win, k1.win, t),
    lamp: lerp(k0.lamp, k1.lamp, t),
    exp: lerp(k0.exp, k1.exp, t),
    sg: lerp(k0.sg, k1.sg, t),
  };
}

// Sun direction on the sky dome for a given hour (east → overhead → west).
// elevation in [-1,1] (sin), azimuth sweeps east(0)→west(PI) across the day.
function sunDirection(h) {
  const elev = Math.sin(((h - 6) / 24) * Math.PI * 2); // -1..1
  const elevRad = elev * (Math.PI / 180) * 70; // cap altitude ~70°
  const az = ((h - 6) / 12) * Math.PI; // east(0) → west(PI)
  const ce = Math.cos(elevRad);
  const dir = new THREE.Vector3(ce * Math.cos(az), Math.sin(elevRad), ce * Math.sin(az));
  return dir.normalize();
}

const _v = new THREE.Vector3();

export class DayNightSystem {
  constructor(scene, renderer) {
    this.scene = scene;
    this.renderer = renderer;

    this.time = 13.0; // start mid-afternoon
    this.autoCycle = true;
    this.cycleSpeed = 1; // 1 = full 24h cycle in ~90s

    this._city = null;
    this._traffic = null;
    this.elapsed = 0; // running clock (ms) for cloud drift + beacon blink

    // --- lights ---
    this.hemi = new THREE.HemisphereLight(0xbcd0ff, 0x504534, 0.65);
    scene.add(this.hemi);

    this.ambient = new THREE.AmbientLight(0x707888, 0.6);
    scene.add(this.ambient);

    this.sun = new THREE.DirectionalLight(0xfff4e6, 2.2);
    this.sun.castShadow = true;
    this.sun.shadow.mapSize.set(2048, 2048);
    this.sun.shadow.camera.near = 10;
    this.sun.shadow.camera.far = 1400;
    const SH = 380;
    this.sun.shadow.camera.left = -SH;
    this.sun.shadow.camera.right = SH;
    this.sun.shadow.camera.top = SH;
    this.sun.shadow.camera.bottom = -SH;
    this.sun.shadow.bias = -0.0004;
    this.sun.shadow.normalBias = 0.6;
    scene.add(this.sun);
    scene.add(this.sun.target);

    this.moon = new THREE.DirectionalLight(0x9ab4ff, 0.0);
    this.moon.castShadow = false;
    scene.add(this.moon);
    scene.add(this.moon.target);

    // --- sky dome (gradient + sun glow) ---
    const glowTex = makeGlowTexture();
    const skyGeo = new THREE.SphereGeometry(2000, 32, 24);
    this.skyUniforms = {
      horizonColor: { value: new THREE.Color(0xd6e4f2) },
      glowColor: { value: new THREE.Color(0xfff0e0) },
      glowStrength: { value: 0.15 },
      zenithColor: { value: new THREE.Color(0x2a5ab0) },
      sunDir: { value: new THREE.Vector3(0, 1, 0) },
      sunColor: { value: new THREE.Color(0xfff4e6) },
      sunGlow: { value: 0.25 },
    };
    this.skyMat = new THREE.ShaderMaterial({
      side: THREE.BackSide,
      depthWrite: false,
      fog: false,
      uniforms: this.skyUniforms,
      vertexShader: /* glsl */ `
        varying vec3 vWorldPos;
        void main() {
          vec4 wp = modelMatrix * vec4(position, 1.0);
          vWorldPos = wp.xyz;
          gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
        }
      `,
      fragmentShader: /* glsl */ `
        varying vec3 vWorldPos;
        uniform vec3 horizonColor;
        uniform vec3 glowColor;
        uniform float glowStrength;
        uniform vec3 zenithColor;
        uniform vec3 sunDir;
        uniform vec3 sunColor;
        uniform float sunGlow;
        void main() {
          vec3 dir = normalize(vWorldPos);
          float h = dir.y;
          float t = clamp(h * 0.5 + 0.5, 0.0, 1.0); // 0..1, horizon 0.5
          vec3 col = mix(horizonColor, zenithColor, smoothstep(0.50, 1.0, t));
          // soft warm glow band at the horizon
          float band = exp(-pow((t - 0.5) * 5.5, 2.0));
          col += glowColor * band * glowStrength;
          // tight sun disc-glow + broad halo around the sun direction
          float sd = max(dot(dir, normalize(sunDir)), 0.0);
          col += sunColor * pow(sd, 80.0) * sunGlow;
          col += sunColor * pow(sd, 6.0) * 0.12 * sunGlow;
          gl_FragColor = vec4(col, 1.0);
        }
      `,
    });
    this.sky = new THREE.Mesh(skyGeo, this.skyMat);
    this.sky.renderOrder = -2;
    scene.add(this.sky);

    // --- stars (deterministic, fixed seed; brightness varies per star) ---
    {
      const starTex = makeStarTexture();
      const rng = createRNG(0x5eed1234);
      const N = 900;
      const positions = new Float32Array(N * 3);
      const colors = new Float32Array(N * 3); // per-star brightness variation
      for (let i = 0; i < N; i++) {
        // uniform-ish on the upper hemisphere shell
        const u = rng.next();
        const v = rng.next();
        const theta = u * Math.PI * 2;
        const phi = Math.acos(rng.range(0.08, 1)); // keep above horizon
        const r = 1900;
        positions[i * 3] = r * Math.sin(phi) * Math.cos(theta);
        positions[i * 3 + 1] = r * Math.cos(phi);
        positions[i * 3 + 2] = r * Math.sin(phi) * Math.sin(theta);
        // brightness: most dim, a few bright. subtle warm/cool tint variety.
        const b = Math.pow(rng.next(), 1.8) * 0.7 + 0.3; // 0.3..1.0, weighted dim
        const warm = rng.range(-0.04, 0.06);
        colors[i * 3] = THREE.MathUtils.clamp(b + warm, 0, 1);
        colors[i * 3 + 1] = THREE.MathUtils.clamp(b, 0, 1);
        colors[i * 3 + 2] = THREE.MathUtils.clamp(b - warm, 0, 1);
      }
      const g = new THREE.BufferGeometry();
      g.setAttribute('position', new THREE.BufferAttribute(positions, 3));
      g.setAttribute('color', new THREE.BufferAttribute(colors, 3));
      const m = new THREE.PointsMaterial({
        size: 3.0,
        sizeAttenuation: false,
        map: starTex,
        transparent: true,
        opacity: 0,
        depthWrite: false,
        depthTest: true,
        blending: THREE.AdditiveBlending,
        vertexColors: true,
      });
      this.stars = new THREE.Points(g, m);
      this.stars.renderOrder = -1;
      scene.add(this.stars);
    }

    // --- visible sun + moon discs ---
    this.sunSprite = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: glowTex,
        color: 0xfff0d0,
        transparent: true,
        opacity: 1,
        depthWrite: false,
        depthTest: true,
        blending: THREE.AdditiveBlending,
        fog: false,
      })
    );
    this.sunSprite.scale.set(150, 150, 1);
    scene.add(this.sunSprite);

    this.moonSprite = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: glowTex,
        color: 0xcdd8ff,
        transparent: true,
        opacity: 0,
        depthWrite: false,
        depthTest: true,
        blending: THREE.AdditiveBlending,
        fog: false,
      })
    );
    this.moonSprite.scale.set(70, 70, 1);
    scene.add(this.moonSprite);

    // --- sun/moon glow halos (larger, fainter additive discs) ---
    this.sunHalo = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: glowTex,
        color: 0xffe6b0,
        transparent: true,
        opacity: 0,
        depthWrite: false,
        depthTest: true,
        blending: THREE.AdditiveBlending,
        fog: false,
      })
    );
    this.sunHalo.scale.set(420, 420, 1);
    scene.add(this.sunHalo);

    this.moonHalo = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: glowTex,
        color: 0xbcd0ff,
        transparent: true,
        opacity: 0,
        depthWrite: false,
        depthTest: true,
        blending: THREE.AdditiveBlending,
        fog: false,
      })
    );
    this.moonHalo.scale.set(220, 220, 1);
    scene.add(this.moonHalo);

    // --- drifting clouds (deterministic; tinted by time of day) ---
    this._cloudMat = new THREE.SpriteMaterial({
      map: makeCloudTexture(),
      color: 0xffffff,
      transparent: true,
      opacity: 0.85,
      depthWrite: false,
      depthTest: true,
      fog: true,
    });
    this.clouds = new THREE.Group();
    const cloudRng = createRNG(0xc1a0d001);
    this._cloudData = [];
    const N_CLOUDS = 11;
    for (let i = 0; i < N_CLOUDS; i++) {
      const s = new THREE.Sprite(this._cloudMat);
      const scale = 160 + cloudRng.next() * 160;
      s.scale.set(scale, scale * 0.6, 1);
      const x = cloudRng.range(-900, 900);
      const z = cloudRng.range(-900, 900);
      const y = 230 + cloudRng.next() * 120;
      s.position.set(x, y, z);
      this._cloudData.push({ speed: 4 + cloudRng.next() * 7, baseX: x, z });
      this.clouds.add(s);
    }
    scene.add(this.clouds);

    // fog on the scene
    scene.fog = new THREE.FogExp2(0xd6e4f2, 0.001);

    this.apply();
  }

  setCity(city) {
    this._city = city;
  }
  setTraffic(traffic) {
    this._traffic = traffic;
  }

  setTimeOfDay(hours) {
    let h = hours % 24;
    if (h < 0) h += 24;
    this.time = h;
    this.apply();
  }
  getTimeOfDay() {
    return this.time;
  }
  setAutoCycle(on) {
    this.autoCycle = !!on;
  }
  setCycleSpeed(s) {
    this.cycleSpeed = Math.max(0.05, Number(s) || 1);
  }

  update(dtMs) {
    this.elapsed += dtMs;
    if (this.autoCycle) {
      // full 24h cycle in ~90s at speed 1
      const rate = (24 / 90) * this.cycleSpeed; // hours per second
      this.time += (dtMs / 1000) * rate;
      this.time = ((this.time % 24) + 24) % 24;
    }
    this.apply();

    // drift clouds slowly + wrap around the city
    const dt = dtMs / 1000;
    const span = 1900;
    for (let i = 0; i < this.clouds.children.length; i++) {
      const s = this.clouds.children[i];
      const d = this._cloudData[i];
      let nx = s.position.x + d.speed * dt;
      if (nx - d.baseX > span / 2) nx -= span;
      if (nx - d.baseX < -span / 2) nx += span;
      s.position.x = nx;
    }

    // blink aviation beacons (slow double-pulse, brighter at night)
    const city = this._city;
    if (city && city.beaconSprites && city.beaconSprites.length) {
      const phase = (this.elapsed % 1600) / 1600; // ~1.6s cycle
      const pulse = phase < 0.12 ? 1 : phase < 0.2 ? 0.15 : phase < 0.32 ? 1 : 0.12;
      const night = this.stars.material.opacity; // 0 day .. 1 night
      city.beaconMaterial.opacity = 0.18 + pulse * Math.max(0.25, night) * 0.82;
    }
  }

  apply() {
    const ch = sampleChannels(this.time);
    const sunDir = sunDirection(this.time);
    const moonDir = _v.copy(sunDir).multiplyScalar(-1);

    // --- sky uniforms ---
    this.skyUniforms.horizonColor.value.copy(ch.hor);
    this.skyUniforms.zenithColor.value.copy(ch.zen);
    this.skyUniforms.glowColor.value.copy(ch.glow);
    this.skyUniforms.glowStrength.value = ch.gs;
    this.skyUniforms.sunColor.value.copy(ch.sun);
    this.skyUniforms.sunGlow.value = ch.sg;
    this.skyUniforms.sunDir.value.copy(sunDir);

    // --- fog (colour == horizon so the city dissolves into the sky) ---
    if (this.scene.fog) {
      this.scene.fog.color.copy(ch.fog);
      this.scene.fog.density = ch.fd;
    }

    // --- sun light + disc ---
    this.sun.position.copy(sunDir).multiplyScalar(1000);
    this.sun.target.position.set(0, 40, 0);
    this.sun.color.copy(ch.sun);
    this.sun.intensity = ch.si;
    this.sunSprite.position.copy(sunDir).multiplyScalar(1850);
    this.sunSprite.material.color.copy(ch.sun);
    // visible when the sun is above the horizon
    const sunVis = THREE.MathUtils.clamp((sunDir.y + 0.06) / 0.12, 0, 1);
    this.sunSprite.material.opacity = sunVis;

    // --- moon light + disc ---
    this.moon.position.copy(moonDir).multiplyScalar(1000);
    this.moon.target.position.set(0, 40, 0);
    this.moon.intensity = ch.mi;
    this.moonSprite.position.copy(moonDir).multiplyScalar(1850);
    const moonVis = THREE.MathUtils.clamp((moonDir.y + 0.06) / 0.12, 0, 1);
    this.moonSprite.material.opacity = Math.min(1, ch.mi * 3.3) * moonVis;

    // --- sun/moon glow halos (follow the discs, fade with visibility) ---
    this.sunHalo.position.copy(this.sunSprite.position);
    this.sunHalo.material.color.copy(ch.sun);
    this.sunHalo.material.opacity = sunVis * 0.5;
    this.moonHalo.position.copy(this.moonSprite.position);
    this.moonHalo.material.color.copy(this.moonSprite.material.color);
    this.moonHalo.material.opacity = Math.min(1, ch.mi * 3.3) * moonVis * 0.45;

    // --- hemi + ambient ---
    this.hemi.color.copy(ch.hs);
    this.hemi.groundColor.copy(ch.hg);
    this.hemi.intensity = ch.hi;
    this.ambient.color.copy(ch.ac);
    this.ambient.intensity = ch.ai;

    // --- stars ---
    this.stars.material.opacity = ch.star;

    // --- clouds tinted by time of day (white day, warm dusk, dark night) ---
    {
      const t = ch.star; // 0 day .. 1 night
      _ca.setRGB(0.94, 0.96, 1.0); // day white
      _cb.setRGB(0.13, 0.16, 0.22); // night dark
      _ca.lerp(_cb, t);
      // warm tint at dusk/dawn (glow band)
      _ca.r += ch.gs * 0.18 * (1 - Math.abs(t - 0.5) * 2);
      this._cloudMat.color.copy(_ca);
      this._cloudMat.opacity = 0.85 - t * 0.45;
    }

    // --- tone mapping ---
    this.renderer.toneMappingExposure = ch.exp;

    // --- city night features (windows, lamps, ground horizon) ---
    const city = this._city;
    if (city) {
      if (city.windowMaterials) {
        const ei = ch.win * 1.15;
        for (let i = 0; i < city.windowMaterials.length; i++) {
          city.windowMaterials[i].emissiveIntensity = ei;
        }
      }
      if (city.lampHeadMaterial) city.lampHeadMaterial.emissiveIntensity = ch.lamp * 2.6;
      if (city.lampGlowMaterial) city.lampGlowMaterial.opacity = ch.lamp * 0.9;
      if (city.setHorizon) city.setHorizon(ch.hor);
    }

    // --- traffic lights ---
    if (this._traffic && this._traffic.setNight) this._traffic.setNight(ch.lamp);
  }

  dispose() {
    this.stars.geometry.dispose();
    this.stars.material.dispose();
    this.skyMat.dispose();
    this.sky.geometry.dispose();
    this.sunSprite.material.dispose();
    this.moonSprite.material.dispose();
    this.sunHalo.material.dispose();
    this.moonHalo.material.dispose();
    this._cloudMat.dispose();
  }
}
