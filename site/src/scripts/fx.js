// Reasonix — motion & FX layer (all effects respect data-motion + prefers-reduced-motion)
(function () {
  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const rich = () => document.body.dataset.motion === "rich" && !reduced;

  /* spotlight border on cards */
  document.querySelectorAll(".feat, .os-card, .doc-card").forEach((card) => {
    card.addEventListener("mousemove", (e) => {
      const r = card.getBoundingClientRect();
      card.style.setProperty("--mx", e.clientX - r.left + "px");
      card.style.setProperty("--my", e.clientY - r.top + "px");
    });
  });

  /* stat counters (run when terminal playback finishes) */
  const counters = Array.from(document.querySelectorAll("[data-cnt]"));
  const fmt = (el, v) => {
    if (el.dataset.fmt === "hm") {
      const m = Math.round(v);
      return Math.floor(m / 60) + "h " + (m % 60) + "m";
    }
    const dec = +(el.dataset.dec || 0);
    return (el.dataset.pre || "") + v.toFixed(dec) + (el.dataset.suf || "");
  };
  const runCounters = () => {
    counters.forEach((el) => {
      const target = parseFloat(el.dataset.cnt);
      if (!rich()) { el.textContent = fmt(el, target); return; }
      const t0 = performance.now(), dur = 1500;
      const frame = (t) => {
        const p = Math.min(1, (t - t0) / dur);
        const e = 1 - Math.pow(1 - p, 3);
        el.textContent = fmt(el, target * e);
        if (p < 1) requestAnimationFrame(frame);
      };
      requestAnimationFrame(frame);
    });
  };
  document.addEventListener("rx:term-played", runCounters);
})();
