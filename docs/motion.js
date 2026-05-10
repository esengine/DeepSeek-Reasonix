/* Counter count-up — fires once when a `.counter` enters the viewport.
 *
 * That's it. No spotlight, no tilt, no scroll reveal, no stagger, no
 * parallax. Static layout does the heavy lifting; this file exists only
 * because metric numbers count up from zero, and that's a one-shot
 * effect that stops as soon as the counter reaches its target. */

(function () {
  "use strict";

  var prefersReduced =
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  function format(n, decimals, prefix, suffix) {
    var s = n.toFixed(decimals);
    var parts = s.split(".");
    parts[0] = parts[0].replace(/\B(?=(\d{3})+(?!\d))/g, ",");
    return (prefix || "") + parts.join(".") + (suffix || "");
  }

  function fill(el) {
    var target = parseFloat(el.getAttribute("data-target") || "0");
    var decimals = parseInt(el.getAttribute("data-decimals") || "0", 10);
    var prefix = el.getAttribute("data-prefix") || "";
    var suffix = el.getAttribute("data-suffix") || "";
    el.textContent = format(target, decimals, prefix, suffix);
  }

  function animate(el) {
    var target = parseFloat(el.getAttribute("data-target") || "0");
    var decimals = parseInt(el.getAttribute("data-decimals") || "0", 10);
    var prefix = el.getAttribute("data-prefix") || "";
    var suffix = el.getAttribute("data-suffix") || "";
    var dur = 1400;
    var t0 = 0;
    function step(t) {
      if (!t0) t0 = t;
      var p = Math.min(1, (t - t0) / dur);
      var eased = 1 - Math.pow(1 - p, 3);
      el.textContent = format(target * eased, decimals, prefix, suffix);
      if (p < 1) window.requestAnimationFrame(step);
      else fill(el);
    }
    window.requestAnimationFrame(step);
  }

  function init() {
    var nodes = document.querySelectorAll(".counter");
    if (!nodes.length) return;

    if (prefersReduced || !("IntersectionObserver" in window)) {
      nodes.forEach(fill);
      return;
    }

    var io = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (e) {
          if (e.isIntersecting) {
            animate(e.target);
            io.unobserve(e.target);
          }
        });
      },
      { threshold: 0.4 },
    );
    nodes.forEach(function (n) { io.observe(n); });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
