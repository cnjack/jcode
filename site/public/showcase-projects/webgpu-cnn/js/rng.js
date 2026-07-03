// js/rng.js
// Seeded pseudo-random number generator (mulberry32) with convenience helpers.
// ALL randomness in the app flows through here so datasets/training are reproducible.

// Core mulberry32 generator: returns a function producing floats in [0, 1).
export function mulberry32(seed) {
  let a = seed >>> 0;
  return function () {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export class RNG {
  constructor(seed = 42) {
    this.seed = seed >>> 0;
    this._rand = mulberry32(this.seed);
    this._spare = null; // cached Box-Muller spare sample
  }

  // Reproduce from the same seed.
  reset() {
    this._rand = mulberry32(this.seed);
    this._spare = null;
    return this;
  }

  next() {
    return this._rand();
  } // [0, 1)

  range(min, max) {
    return min + (max - min) * this._rand();
  } // [min, max)

  int(n) {
    return Math.floor(this._rand() * n);
  } // [0, n)

  bool(p = 0.5) {
    return this._rand() < p;
  }

  pick(arr) {
    return arr[this.int(arr.length)];
  }

  // Standard normal via Box-Muller transform.
  gauss(mean = 0, std = 1) {
    if (this._spare !== null) {
      const v = this._spare;
      this._spare = null;
      return mean + std * v;
    }
    let u, v, s;
    do {
      u = this._rand() * 2 - 1;
      v = this._rand() * 2 - 1;
      s = u * u + v * v;
    } while (s >= 1 || s === 0);
    const mul = Math.sqrt((-2 * Math.log(s)) / s);
    this._spare = v * mul;
    return mean + std * u * mul;
  }

  // In-place Fisher-Yates shuffle (deterministic).
  shuffle(arr) {
    for (let i = arr.length - 1; i > 0; i--) {
      const j = this.int(i + 1);
      const tmp = arr[i];
      arr[i] = arr[j];
      arr[j] = tmp;
    }
    return arr;
  }

  // Create a deterministic sub-stream seeded from this one.
  fork(salt = 0) {
    const s =
      (Math.imul((this.seed ^ 0x9e3779b9) >>> 0, 0x85ebca6b) ^ (salt >>> 0)) >>>
      0;
    return new RNG(s);
  }
}
