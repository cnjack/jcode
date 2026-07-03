// audio.js — WebAudio synth SFX + generative ambient music + settings persistence.
// NO audio files. AudioContext created/resumed only on first user gesture.

const STORE_KEY = 'cd_audio_v1';
const NOTE = [0, 2, 4, 7, 9]; // pentatonic semitone offsets

export class AudioEngine {
  constructor() {
    this.ctx = null;
    this.master = null;
    this.musicGain = null;
    this.sfxGain = null;
    this.muted = false;
    this.volume = 0.55;
    this.musicOn = true;
    this.suppress = false;     // true during fastForward (no scheduling)
    this._last = new Map();    // throttle map: key -> ctxTime
    this._drone = null;        // boss drone nodes
    this._musicStep = 0;
    this.load();
  }

  load() {
    try {
      const raw = localStorage.getItem(STORE_KEY);
      if (raw) {
        const o = JSON.parse(raw);
        if (typeof o.muted === 'boolean') this.muted = o.muted;
        if (typeof o.volume === 'number') this.volume = Math.max(0, Math.min(1, o.volume));
        if (typeof o.musicOn === 'boolean') this.musicOn = o.musicOn;
      }
    } catch (e) { /* ignore */ }
  }
  save() {
    try {
      localStorage.setItem(STORE_KEY, JSON.stringify({ muted: this.muted, volume: this.volume, musicOn: this.musicOn }));
    } catch (e) { /* ignore */ }
  }

  // Lazily build the graph. Call from a user-gesture path only.
  ensure() {
    if (this.ctx) return !!this.ctx;
    if (typeof window === 'undefined') return false;
    const AC = window.AudioContext || window.webkitAudioContext;
    if (!AC) return false;
    try {
      this.ctx = new AC();
      this.master = this.ctx.createGain();
      this.master.gain.value = this.muted ? 0 : this.volume;
      this.master.connect(this.ctx.destination);
      this.sfxGain = this.ctx.createGain();
      this.sfxGain.gain.value = 0.9;
      this.sfxGain.connect(this.master);
      this.musicGain = this.ctx.createGain();
      this.musicGain.gain.value = this.musicOn ? 0.5 : 0;
      this.musicGain.connect(this.master);
    } catch (e) { this.ctx = null; }
    return !!this.ctx;
  }
  resume() { if (this.ctx && this.ctx.state === 'suspended') this.ctx.resume().catch(() => {}); }

  setMuted(m) { this.muted = !!m; if (this.master) this.master.gain.value = this.muted ? 0 : this.volume; this.save(); }
  setVolume(v) { this.volume = Math.max(0, Math.min(1, v)); if (this.master && !this.muted) this.master.gain.value = this.volume; this.save(); }
  setMusic(on) { this.musicOn = !!on; if (this.musicGain) this.musicGain.gain.value = this.musicOn ? 0.5 : 0; this.save(); }

  get ctxState() { return this.ctx ? this.ctx.state : 'none'; }

  // ---- low-level voices ----
  _tone(opt) {
    if (!this.ctx) return;
    const t = this.ctx.currentTime;
    const o = this.ctx.createOscillator();
    const g = this.ctx.createGain();
    o.type = opt.type || 'sine';
    o.frequency.setValueAtTime(opt.freq, t);
    if (opt.freqEnd) o.frequency.exponentialRampToValueAtTime(Math.max(1, opt.freqEnd), t + opt.dur);
    const peak = opt.gain == null ? 0.3 : opt.gain;
    const atk = opt.atk == null ? 0.005 : opt.atk;
    g.gain.setValueAtTime(0.0001, t);
    g.gain.exponentialRampToValueAtTime(peak, t + atk);
    g.gain.exponentialRampToValueAtTime(0.0001, t + opt.dur);
    o.connect(g); g.connect(opt.dest || this.sfxGain);
    o.start(t); o.stop(t + opt.dur + 0.02);
  }
  _noise(opt) {
    if (!this.ctx) return;
    const t = this.ctx.currentTime;
    const len = Math.max(1, Math.floor(this.ctx.sampleRate * opt.dur));
    const buf = this.ctx.createBuffer(1, len, this.ctx.sampleRate);
    const d = buf.getChannelData(0);
    for (let i = 0; i < len; i++) d[i] = (Math.random() * 2 - 1);
    const src = this.ctx.createBufferSource(); src.buffer = buf;
    const filt = this.ctx.createBiquadFilter();
    filt.type = opt.filterType || 'lowpass';
    filt.frequency.value = opt.filterFreq || 1200;
    const g = this.ctx.createGain();
    const peak = opt.gain == null ? 0.3 : opt.gain;
    g.gain.setValueAtTime(peak, t);
    g.gain.exponentialRampToValueAtTime(0.0001, t + opt.dur);
    src.connect(filt); filt.connect(g); g.connect(opt.dest || this.sfxGain);
    src.start(t); src.stop(t + opt.dur + 0.02);
  }

  // throttle: min seconds between identical sounds
  _gate(key, gap) {
    if (this.suppress) return false;
    const now = this.ctx ? this.ctx.currentTime : 0;
    const last = this._last.get(key) || 0;
    if (now - last < gap) return false;
    this._last.set(key, now);
    return true;
  }

  // ---- SFX ----
  shoot(type) {
    if (!this.ctx || this.muted) return;
    if (type === 'pulse') {
      if (!this._gate('pulse', 0.04)) return;
      this._tone({ type: 'square', freq: 880, freqEnd: 560, dur: 0.07, gain: 0.16, atk: 0.002 });
    } else if (type === 'splash') {
      if (!this._gate('splash', 0.05)) return;
      this._tone({ type: 'sawtooth', freq: 180, freqEnd: 90, dur: 0.12, gain: 0.2 });
      this._noise({ dur: 0.1, gain: 0.12, filterFreq: 700 });
    } else if (type === 'beam') {
      if (!this._gate('beam', 0.12)) return;
      this._tone({ type: 'sawtooth', freq: 320, freqEnd: 280, dur: 0.05, gain: 0.06 });
    } else if (type === 'slow') {
      if (!this._gate('slow', 0.5)) return;
      this._tone({ type: 'sine', freq: 300, freqEnd: 240, dur: 0.3, gain: 0.05, atk: 0.05 });
    }
  }
  impact() {
    if (!this.ctx || this.muted) return;
    if (!this._gate('impact', 0.05)) return;
    this._noise({ dur: 0.22, gain: 0.22, filterFreq: 900, filterType: 'lowpass' });
    this._tone({ type: 'sine', freq: 120, freqEnd: 50, dur: 0.25, gain: 0.22 });
  }
  death() {
    if (!this.ctx || this.muted) return;
    if (!this._gate('death', 0.035)) return;
    this._tone({ type: 'sine', freq: 520, freqEnd: 140, dur: 0.12, gain: 0.12 });
    this._noise({ dur: 0.06, gain: 0.06, filterFreq: 2000 });
  }
  leak() {
    if (!this.ctx || this.muted) return;
    if (!this._gate('leak', 0.3)) return;
    this._tone({ type: 'square', freq: 440, dur: 0.09, gain: 0.18 });
    window.setTimeout(() => this._tone({ type: 'square', freq: 300, dur: 0.12, gain: 0.18 }), 90);
  }
  upgrade() {
    if (!this.ctx || this.muted) return;
    this._tone({ type: 'triangle', freq: 520, dur: 0.1, gain: 0.18 });
    window.setTimeout(() => this._tone({ type: 'triangle', freq: 780, dur: 0.14, gain: 0.18 }), 90);
  }
  sell() {
    if (!this.ctx || this.muted) return;
    this._tone({ type: 'square', freq: 1040, dur: 0.05, gain: 0.12 });
    window.setTimeout(() => this._tone({ type: 'square', freq: 1380, dur: 0.07, gain: 0.12 }), 55);
  }
  waveStart() {
    if (!this.ctx || this.muted) return;
    [0, 1, 2].forEach((i) => window.setTimeout(() =>
      this._tone({ type: 'triangle', freq: 330 * Math.pow(2, i / 4), dur: 0.14, gain: 0.16 }), i * 80));
  }
  bossSpawn() {
    if (!this.ctx || this.muted) return;
    this._tone({ type: 'sawtooth', freq: 160, freqEnd: 80, dur: 0.5, gain: 0.3 });
    this._noise({ dur: 0.4, gain: 0.12, filterFreq: 500 });
  }
  bossDown() {
    if (!this.ctx || this.muted) return;
    this._tone({ type: 'sawtooth', freq: 300, freqEnd: 60, dur: 0.6, gain: 0.28 });
  }
  startDrone() {
    if (!this.ctx || this._drone || this.muted) return;
    const o = this.ctx.createOscillator();
    const o2 = this.ctx.createOscillator();
    const g = this.ctx.createGain();
    o.type = 'sawtooth'; o.frequency.value = 55;
    o2.type = 'sine'; o2.frequency.value = 82.5;
    g.gain.value = 0.0;
    g.gain.linearRampToValueAtTime(0.12, this.ctx.currentTime + 0.6);
    const filt = this.ctx.createBiquadFilter(); filt.type = 'lowpass'; filt.frequency.value = 320;
    o.connect(filt); o2.connect(filt); filt.connect(g); g.connect(this.sfxGain);
    o.start(); o2.start();
    this._drone = { o, o2, g };
  }
  stopDrone() {
    if (!this.ctx || !this._drone) return;
    const { o, o2, g } = this._drone;
    try {
      g.gain.cancelScheduledValues(this.ctx.currentTime);
      g.gain.linearRampToValueAtTime(0.0001, this.ctx.currentTime + 0.4);
      o.stop(this.ctx.currentTime + 0.45);
      o2.stop(this.ctx.currentTime + 0.45);
    } catch (e) { /* ignore */ }
    this._drone = null;
  }
  victory() {
    if (!this.ctx || this.muted) return;
    [0, 4, 7, 12].forEach((s, i) => window.setTimeout(() =>
      this._tone({ type: 'triangle', freq: 392 * Math.pow(2, s / 12), dur: 0.3, gain: 0.2 }), i * 140));
  }
  gameOver() {
    if (!this.ctx || this.muted) return;
    [0, -2, -4, -7].forEach((s, i) => window.setTimeout(() =>
      this._tone({ type: 'sawtooth', freq: 300 * Math.pow(2, s / 12), dur: 0.28, gain: 0.2 }), i * 160));
  }

  // ---- generative ambient music ----
  musicTick() {
    if (!this.ctx || this.muted || !this.musicOn || this.suppress) return;
    if (!this._gate('music', 0)) return; // not strictly throttled
    const t = this.ctx.currentTime;
    const root = 130.81; // C3
    // evolving: shift root every 8 steps
    const step = this._musicStep++;
    const rootShift = [0, 0, 5, 7][Math.floor(step / 8) % 4]; // root movement
    const idx = NOTE[Math.floor(Math.random() * NOTE.length)];
    const octave = Math.random() < 0.5 ? 12 : 24;
    const freq = root * Math.pow(2, (rootShift + idx + octave) / 12);
    const o = this.ctx.createOscillator();
    const g = this.ctx.createGain();
    o.type = Math.random() < 0.5 ? 'triangle' : 'sine';
    o.frequency.value = freq;
    const dur = 1.6 + Math.random() * 1.4;
    g.gain.setValueAtTime(0.0001, t);
    g.gain.linearRampToValueAtTime(0.05, t + 0.6);
    g.gain.linearRampToValueAtTime(0.0001, t + dur);
    o.connect(g); g.connect(this.musicGain);
    o.start(t); o.stop(t + dur + 0.05);
    // occasional soft bass note
    if (step % 4 === 0) {
      const bo = this.ctx.createOscillator(); const bg = this.ctx.createGain();
      bo.type = 'sine'; bo.frequency.value = root * Math.pow(2, rootShift / 12) / 2;
      bg.gain.setValueAtTime(0.0001, t);
      bg.gain.linearRampToValueAtTime(0.04, t + 0.5);
      bg.gain.linearRampToValueAtTime(0.0001, t + dur + 0.5);
      bo.connect(bg); bg.connect(this.musicGain); bo.start(t); bo.stop(t + dur + 0.6);
    }
  }
}
