(function () {
  // A sparse field of small squares ("tokens") that twinkle behind the hero
  // copy — canvas, no dependencies. Static single frame for anyone who
  // prefers reduced motion.
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

    var count = Math.round((w * h) / 5500); // density scales with hero size
    dots = [];
    for (var i = 0; i < count; i++) {
      dots.push({
        x: Math.random() * w,
        y: Math.random() * h,
        size: Math.random() < 0.15 ? 3 : 2,
        base: 0.08 + Math.random() * 0.22,
        phase: Math.random() * Math.PI * 2,
        speed: 0.4 + Math.random() * 0.5,
      });
    }
  }

  function draw(t) {
    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = accentColor();
    for (var i = 0; i < dots.length; i++) {
      var d = dots[i];
      var o = reduceMotion ? d.base : d.base * (0.55 + 0.45 * Math.sin(t * 0.001 * d.speed + d.phase));
      ctx.globalAlpha = o;
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
