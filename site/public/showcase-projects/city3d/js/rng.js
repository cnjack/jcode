// js/rng.js
// Seeded pseudo-random number generator (mulberry32) plus helpers.
// Every bit of randomness in the city flows through this so the same seed
// reproduces the exact same city every time.

// mulberry32: fast, 32-bit seeded PRNG with good statistical quality.
function mulberry32(seed) {
  let a = seed >>> 0;
  return function () {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// Create a generator object from an integer seed.
export function createRNG(seed) {
  const next = mulberry32(seed);

  const rng = {
    // Core float in [0, 1)
    next,

    // float in [min, max)
    range: (min, max) => min + next() * (max - min),

    // integer in [min, max] inclusive
    int: (min, max) => Math.floor(min + next() * (max - min + 1)),

    // boolean true with probability p
    chance: (p) => next() < p,

    // pick a random element from an array
    pick: (arr) => arr[Math.floor(next() * arr.length)],

    // Gaussian-ish value via central limit (sum of 3 uniforms), centered ~0.5
    // narrowed spread good for "mostly average" heights etc.
    gaussian: () => (next() + next() + next()) / 3,

    // Fisher-Yates shuffle in place
    shuffle: (arr) => {
      for (let i = arr.length - 1; i > 0; i--) {
        const j = Math.floor(next() * (i + 1));
        const tmp = arr[i];
        arr[i] = arr[j];
        arr[j] = tmp;
      }
      return arr;
    },

    // Seedable sub-generator: derive a new independent RNG for a subsystem.
    fork: () => createRNG(Math.floor(next() * 0xffffffff)),
  };

  return rng;
}

// Generate a fresh random seed (32-bit unsigned).
export function randomSeed() {
  return Math.floor(Math.random() * 0xffffffff) >>> 0;
}
