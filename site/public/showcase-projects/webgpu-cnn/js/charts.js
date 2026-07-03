// js/charts.js
// Hand-drawn retina line charts (no libraries). Multiple series, auto-scaling,
// gridlines, ticks, and a legend.

export class LineChart {
  constructor(canvas, opts = {}) {
    this.canvas = canvas;
    this.ctx = canvas.getContext('2d');
    this.series = opts.series || []; // [{name,color}]
    this.xlabel = opts.xlabel || '';
    this.ylabel = opts.ylabel || '';
    this.yMin = opts.yMin; // optional fixed
    this.yMax = opts.yMax;
    this.yfmt = opts.yfmt || ((v) => v.toFixed(2));
    this.xfmt = opts.xfmt || ((v) => String(Math.round(v)));
    this.points = new Map(); // name -> {xs:[], ys:[]}
    for (const s of this.series) this.points.set(s.name, { xs: [], ys: [] });
    this._resize();
  }

  _resize() {
    const dpr = window.devicePixelRatio || 1;
    const w = this.canvas.clientWidth || 300;
    const h = this.canvas.clientHeight || 140;
    this.cw = w;
    this.ch = h;
    this.canvas.width = Math.round(w * dpr);
    this.canvas.height = Math.round(h * dpr);
    this.ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  }

  resize() {
    this._resize();
    this.draw();
  }

  reset(name) {
    if (name) {
      const p = this.points.get(name);
      if (p) {
        p.xs = [];
        p.ys = [];
      }
    } else {
      for (const s of this.series) {
        const p = this.points.get(s.name);
        p.xs = [];
        p.ys = [];
      }
    }
  }

  push(name, x, y) {
    const p = this.points.get(name);
    if (!p) return;
    p.xs.push(x);
    p.ys.push(y);
  }

  draw() {
    const ctx = this.ctx;
    const w = this.cw;
    const h = this.ch;
    const padL = 48;
    const padR = 12;
    const padT = 12;
    const padB = 26;
    ctx.clearRect(0, 0, w, h);

    const plotW = w - padL - padR;
    const plotH = h - padT - padB;

    // Compute ranges.
    let xmin = Infinity;
    let xmax = -Infinity;
    let ymin = this.yMin ?? Infinity;
    let ymax = this.yMax ?? -Infinity;
    let any = false;
    for (const s of this.series) {
      const p = this.points.get(s.name);
      for (let i = 0; i < p.xs.length; i++) {
        any = true;
        if (p.xs[i] < xmin) xmin = p.xs[i];
        if (p.xs[i] > xmax) xmax = p.xs[i];
        if (this.yMin == null && p.ys[i] < ymin) ymin = p.ys[i];
        if (this.yMax == null && p.ys[i] > ymax) ymax = p.ys[i];
      }
    }
    if (!any) {
      xmin = 0;
      xmax = 1;
      ymin = 0;
      ymax = 1;
    }
    if (xmax === xmin) xmax = xmin + 1;
    if (ymax === ymin) ymax = ymin + 1;
    const ypad = (ymax - ymin) * 0.08;
    if (this.yMin == null) ymin -= ypad;
    if (this.yMax == null) ymax += ypad;

    const sx = (x) => padL + ((x - xmin) / (xmax - xmin)) * plotW;
    const sy = (y) => padT + plotH - ((y - ymin) / (ymax - ymin)) * plotH;

    // Gridlines + tick labels.
    ctx.font = '10px ui-monospace, SFMono-Regular, Menlo, monospace';
    ctx.fillStyle = '#8b95a7';
    ctx.strokeStyle = 'rgba(255,255,255,0.06)';
    ctx.lineWidth = 1;
    const yticks = 4;
    for (let i = 0; i <= yticks; i++) {
      const yv = ymin + ((ymax - ymin) * i) / yticks;
      const yy = sy(yv);
      ctx.beginPath();
      ctx.moveTo(padL, yy);
      ctx.lineTo(padL + plotW, yy);
      ctx.stroke();
      ctx.textAlign = 'right';
      ctx.textBaseline = 'middle';
      ctx.fillText(this.yfmt(yv), padL - 6, yy);
    }
    const xticks = 4;
    for (let i = 0; i <= xticks; i++) {
      const xv = xmin + ((xmax - xmin) * i) / xticks;
      const xx = sx(xv);
      ctx.beginPath();
      ctx.moveTo(xx, padT);
      ctx.lineTo(xx, padT + plotH);
      ctx.stroke();
      ctx.textAlign = 'center';
      ctx.textBaseline = 'top';
      ctx.fillText(this.xfmt(xv), xx, padT + plotH + 5);
    }

    // Axes frame.
    ctx.strokeStyle = 'rgba(255,255,255,0.18)';
    ctx.strokeRect(padL, padT, plotW, plotH);

    // Series.
    for (const s of this.series) {
      const p = this.points.get(s.name);
      if (p.xs.length < 1) continue;
      ctx.strokeStyle = s.color;
      ctx.lineWidth = 2;
      ctx.lineJoin = 'round';
      ctx.beginPath();
      ctx.moveTo(sx(p.xs[0]), sy(p.ys[0]));
      for (let i = 1; i < p.xs.length; i++) ctx.lineTo(sx(p.xs[i]), sy(p.ys[i]));
      ctx.stroke();
    }

    // Legend.
    let lx = padL + 8;
    const ly = padT + 6;
    ctx.font = '11px system-ui, sans-serif';
    ctx.textBaseline = 'middle';
    ctx.textAlign = 'left';
    for (const s of this.series) {
      ctx.fillStyle = s.color;
      ctx.fillRect(lx, ly - 1, 10, 3);
      ctx.fillStyle = '#cdd6e6';
      ctx.fillText(s.name, lx + 14, ly + 2);
      lx += 16 + ctx.measureText(s.name).width + 14;
    }
  }
}
