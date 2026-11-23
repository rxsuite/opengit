// OpenGit animated pixel-art logo.
// An eye opens, the view zooms into the pupil, stars appear in the dark,
// and the pixel-text "OpenGit" fades in on the black. Loops forever.
(function () {
  var W = 192, H = 108, T = 13; // low-res canvas size, loop length (s)

  // 5-wide bitmap glyphs, rows top to bottom; 'row' = first row index (0 = cap line)
  var G = {
    O: { row: 0, d: ['01110','10001','10001','10001','10001','10001','01110'] },
    p: { row: 2, d: ['11110','10001','10001','10001','11110','10000','10000'] },
    e: { row: 2, d: ['01110','10001','11111','10000','01110'] },
    n: { row: 2, d: ['10110','11001','10001','10001','10001'] },
    G: { row: 0, d: ['01110','10001','10000','10111','10001','10001','01110'] },
    i: { row: 0, d: ['00100','00000','00100','00100','00100','00100','00100'] },
    t: { row: 1, d: ['00100','01110','00100','00100','00100','00011'] }
  };
  var TEXT = 'OpenGit', SCALE = 3, GAP = 1;

  function hash(x, y) {
    var h = (x * 374761393 + y * 668265263) | 0;
    h = (h ^ (h >>> 13)) * 1274126177 | 0;
    return ((h ^ (h >>> 16)) >>> 0) / 4294967295;
  }
  function clamp(v, a, b) { return v < a ? a : v > b ? b : v; }
  function smooth(a, b, v) { v = clamp((v - a) / (b - a), 0, 1); return v * v * (3 - 2 * v); }

  // text mask in screen pixels
  var tw = TEXT.length * (5 + GAP) - GAP, tx0 = Math.floor((W - tw * SCALE) / 2), ty0 = Math.floor(H / 2 - 4 * SCALE);
  var mask = new Uint8Array(W * H);
  (function () {
    for (var i = 0; i < TEXT.length; i++) {
      var g = G[TEXT[i]];
      for (var r = 0; r < g.d.length; r++) for (var c = 0; c < 5; c++) {
        if (g.d[r][c] !== '1') continue;
        for (var sy = 0; sy < SCALE; sy++) for (var sx = 0; sx < SCALE; sx++) {
          var X = tx0 + (i * (5 + GAP) + c) * SCALE + sx, Y = ty0 + (g.row + r) * SCALE + sy;
          if (X >= 0 && X < W && Y >= 0 && Y < H) mask[Y * W + X] = 1;
        }
      }
    }
  })();

  var stars = [];
  for (var s = 0; s < 70; s++) stars.push({ x: hash(s, 1) * W | 0, y: hash(s, 2) * H | 0, ph: hash(s, 3) * 6.28, big: hash(s, 4) > 0.8 });

  // 8-colour-ish iris palette (quantised for the pixel look)
  var IRIS = [[70,28,6],[120,52,8],[176,86,10],[222,128,18],[246,176,40],[255,214,90]];
  function quant(v, n) { return Math.floor(clamp(v, 0, 0.999) * n) / (n - 1); }

  function frame(img, t) {
    var d = img.data;
    var zoomP = smooth(1.2, 7.4, t);          // 0..1 zoom progress
    var zoom = Math.exp(zoomP * Math.log(14)); // 1..14
    var blink = t < 1.2 ? Math.abs(Math.sin(smooth(0, 1.2, t) * 3.14159)) * 0 + smooth(0, 1.0, t) : 1; // eye opening
    var starA = smooth(6.0, 8.0, t);
    var textA = smooth(8.4, 10.6, t);
    var fade = 1 - smooth(12.2, 13, t);
    var look = Math.sin(t * 1.3) * 0.015 * (1 - zoomP); // tiny eye drift
    for (var y = 0; y < H; y++) for (var x = 0; x < W; x++) {
      var wx = ((x + 0.5 - W / 2) / H) / zoom - look, wy = ((y + 0.5 - H / 2) / H) / zoom;
      var r = 0, g = 0, b = 0, i = (y * W + x) * 4;
      // eye outline (almond)
      var ax = Math.abs(wx) / 0.78;
      var lid = ax < 1 ? 0.3 * Math.pow(1 - ax * ax, 0.8) * blink : -1;
      var inEye = Math.abs(wy) < lid;
      if (!inEye) {
        // skin / background: dark with faint dither near the lids
        var near = ax < 1.05 && Math.abs(wy) < 0.36;
        var v = 14 + (near && hash(x, y) > 0.55 ? 10 : 0);
        r = v; g = v; b = v + 6;
      } else {
        var rad = Math.sqrt(wx * wx + wy * wy), ang = Math.atan2(wy, wx);
        var irisR = 0.255, pupilR = 0.095 + 0.012 * Math.sin(t * 2.1);
        if (rad < pupilR) {
          r = g = b = 2;
          // catch-light, fades as we dive in
          var hl = Math.hypot(wx + 0.035, wy + 0.035);
          if (hl < 0.018 && zoomP < 0.55) { r = g = b = 250; }
        } else if (rad < irisR) {
          var k = (rad - pupilR) / (irisR - pupilR);
          var streak = 0.5 + 0.5 * Math.sin(ang * 22 + hash((ang * 7) | 0, 3) * 4);
          var tone = clamp(0.25 + 0.55 * (1 - k) + 0.25 * streak - (k > 0.88 ? 0.5 : 0), 0, 1);
          var c = IRIS[Math.floor(quant(tone, IRIS.length) * (IRIS.length - 1) + 0.001)];
          r = c[0]; g = c[1]; b = c[2];
        } else {
          // sclera with veins & shading toward the lids
          var shade = 1 - Math.pow(Math.abs(wy) / (lid + 1e-6), 3) * 0.55;
          var vv = Math.floor(clamp(shade, 0, 1) * 5) / 5;
          r = 235 * (0.45 + 0.55 * vv); g = 232 * (0.45 + 0.55 * vv); b = 228 * (0.45 + 0.55 * vv);
          if (hash(x >> 1, y >> 1) > 0.99) { r = 200; g = 90; b = 90; }
        }
      }
      // dive: pupil dark fills the screen, stars + planet appear in screen space
      if (zoomP > 0.7) {
        var dark = smooth(0.7, 0.95, zoomP);
        r *= 1 - dark; g *= 1 - dark; b *= 1 - dark;
      }
      d[i] = r; d[i + 1] = g; d[i + 2] = b; d[i + 3] = 255;
    }
    // stars (screen space, pixel crosses)
    if (starA > 0) for (var q = 0; q < stars.length; q++) {
      var st = stars[q], tw2 = 0.5 + 0.5 * Math.sin(t * 3 + st.ph);
      if (hash(q, 9) > starA) continue;
      var lum = 90 + 165 * tw2, pts = [[0, 0]];
      if (st.big && tw2 > 0.35) pts.push([1, 0], [-1, 0], [0, 1], [0, -1]);
      for (var p = 0; p < pts.length; p++) {
        var X = st.x + pts[p][0], Y = st.y + pts[p][1];
        if (X < 0 || X >= W || Y < 0 || Y >= H) continue;
        var j = (Y * W + X) * 4; d[j] = d[j + 1] = d[j + 2] = lum;
      }
    }
    // small pink planet (top right) pulsing gently
    if (starA > 0.3) {
      var pcx = W * 0.82, pcy = H * 0.24, pr = 7 * smooth(0.3, 1, starA);
      for (var yy = -9; yy <= 9; yy++) for (var xx = -9; xx <= 9; xx++) {
        var dd = Math.sqrt(xx * xx + yy * yy);
        if (dd > pr) continue;
        var X2 = Math.round(pcx + xx), Y2 = Math.round(pcy + yy), j2 = (Y2 * W + X2) * 4;
        var lit = clamp(1 - Math.hypot(xx + 2.5, yy + 2.5) / (pr * 1.6), 0, 1);
        lit = Math.floor(lit * 4) / 4;
        d[j2] = 120 + 130 * lit; d[j2 + 1] = 20 + 60 * lit; d[j2 + 2] = 90 + 70 * lit;
      }
    }
    // text: pixels revealed in random order, with a pink drop shadow
    if (textA > 0) for (var y2 = 0; y2 < H; y2++) for (var x2 = 0; x2 < W; x2++) {
      if (hash(x2, y2 + 77) > textA) continue;
      if (mask[y2 * W + x2]) {
        var k2 = (y2 * W + x2) * 4, grad = (y2 - ty0) / (8 * SCALE);
        d[k2] = 255; d[k2 + 1] = 240 - 90 * grad; d[k2 + 2] = 230 - 120 * grad;
      } else if (x2 >= SCALE && y2 + SCALE < H && mask[(y2 - SCALE) * W + x2 - SCALE]) {
        var k3 = (y2 * W + x2) * 4; d[k3] = 200; d[k3 + 1] = 40; d[k3 + 2] = 110;
      }
    }
    if (fade < 1) for (var m = 0; m < d.length; m += 4) { d[m] *= fade; d[m + 1] *= fade; d[m + 2] *= fade; }
  }

  function mount(canvas, opts) {
    opts = opts || {};
    canvas.width = W; canvas.height = H;
    canvas.style.imageRendering = 'pixelated';
    var ctx = canvas.getContext('2d'), img = ctx.createImageData(W, H);
    var reduce = window.matchMedia && matchMedia('(prefers-reduced-motion: reduce)').matches;
    var start = null, raf = 0;
    if (reduce || opts.still) { frame(img, 10.5); ctx.putImageData(img, 0, 0); return; }
    function tick(ts) {
      if (start === null) start = ts;
      frame(img, ((ts - start) / 1000) % T);
      ctx.putImageData(img, 0, 0);
      raf = requestAnimationFrame(tick);
    }
    raf = requestAnimationFrame(tick);
    return function stop() { cancelAnimationFrame(raf); };
  }
  window.OpenGitLogo = { mount: mount, frame: frame, W: W, H: H, T: T };
})();
