(function () {
  // A dense field of small squares ("tokens") that twinkle and drift behind
  // the hero copy — canvas, no dependencies. Static single frame for anyone
  // who prefers reduced motion.
  var canvas = document.querySelector(".hero canvas");
  if (!canvas) return;
  var ctx = canvas.getContext("2d");
  var reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  var dots = [];
  var dpr = Math.min(window.devicePixelRatio || 1, 2);
  var w = 0, h = 0;

  function accentColor() {
    return getComputedStyle(document.documentElement).getPropertyValue("--accent").trim() || "#5eead4";
  }

  function seed() {
    var hero = canvas.parentElement;
    w = hero.clientWidth;
    h = hero.clientHeight;
    canvas.width = w * dpr;
    canvas.height = h * dpr;
    canvas.style.width = w + "px";
    canvas.style.height = h + "px";
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    var count = Math.round((w * h) / 1800); // dense — this is the focal effect, not a hint of one
    dots = [];
    for (var i = 0; i < count; i++) {
      var big = Math.random() < 0.15;
      dots.push({
        x: Math.random() * w,
        y: Math.random() * h,
        size: big ? 4 + Math.random() * 3 : 1.5 + Math.random() * 2,
        base: big ? 0.55 + Math.random() * 0.4 : 0.18 + Math.random() * 0.3,
        phase: Math.random() * Math.PI * 2,
        speed: 0.8 + Math.random() * 1.6,
        vx: (Math.random() - 0.5) * 0.25,
        vy: (Math.random() - 0.5) * 0.25,
      });
    }
  }

  function draw(t) {
    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = accentColor();
    for (var i = 0; i < dots.length; i++) {
      var d = dots[i];
      var o = d.base;
      if (!reduceMotion) {
        o = d.base * (0.35 + 0.65 * Math.sin(t * 0.0016 * d.speed + d.phase));
        d.x += d.vx; d.y += d.vy;
        if (d.x < -4) d.x = w + 4; else if (d.x > w + 4) d.x = -4;
        if (d.y < -4) d.y = h + 4; else if (d.y > h + 4) d.y = -4;
      }
      ctx.globalAlpha = Math.max(o, 0);
      ctx.fillRect(d.x, d.y, d.size, d.size);
    }
    ctx.globalAlpha = 1;
    if (!reduceMotion) requestAnimationFrame(draw);
  }

  seed();
  draw(0);

  var resizeTimer;
  window.addEventListener("resize", function () {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function () {
      seed();
      if (reduceMotion) draw(0);
    }, 150);
  });
})();
