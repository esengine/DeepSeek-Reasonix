// Reasonix — motion & FX layer (all effects respect data-motion + prefers-reduced-motion)
(function () {
  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const rich = () => document.body.dataset.motion === "rich" && !reduced;
  const lerp = (a, b, t) => a + (b - a) * t;

  /* run a rAF loop only while `el` is on screen — no idle spinning */
  const rafWhileVisible = (el, fn) => {
    let rafId = 0, running = false;
    const loop = () => {
      if (!running) return;
      fn();
      rafId = requestAnimationFrame(loop);
    };
    const setRunning = (on) => {
      if (on === running) return;
      running = on;
      if (on) rafId = requestAnimationFrame(loop);
      else cancelAnimationFrame(rafId);
    };
    new IntersectionObserver((entries) => {
      setRunning(entries[entries.length - 1].isIntersecting);
    }).observe(el);
  };

  /* terminal 3D tilt toward cursor */
  const term = document.querySelector(".term");
  if (term && matchMedia("(pointer: fine)").matches) {
    const stage = term.parentElement;
    let rx = 0, ry = 0, trx = 0, try_ = 0;
    stage.addEventListener("mousemove", (e) => {
      const r = stage.getBoundingClientRect();
      try_ = ((e.clientX - r.left) / r.width - 0.5) * 5;
      trx = (0.5 - (e.clientY - r.top) / r.height) * 4;
    });
    stage.addEventListener("mouseleave", () => { trx = 0; try_ = 0; });
    rafWhileVisible(stage, () => {
      if (!rich()) { term.style.transform = ""; return; }
      rx = lerp(rx, trx, 0.08);
      ry = lerp(ry, try_, 0.08);
      term.style.transform = `rotateX(${rx.toFixed(2)}deg) rotateY(${ry.toFixed(2)}deg)`;
    });
  }

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

  /* scroll-driven cache narrative (sticky) */
  const track = document.querySelector(".how-track");
  if (track) {
    const rows = Array.from(track.querySelectorAll(".cache-row"));
    const caps = Array.from(track.querySelectorAll(".cap"));
    rows.forEach((row) =>
      row.querySelectorAll(".blk").forEach((b, i) => b.style.setProperty("--i", i)));
    const stickyOK = () => rich() && innerWidth > 900;
    const setStep = (n) => {
      rows.forEach((r, i) => r.classList.toggle("row-on", i < n));
      caps.forEach((c) => c.classList.toggle("on", +c.dataset.step === n));
    };
    const onScroll = () => {
      if (!stickyOK()) {
        document.body.classList.add("how-flat");
        setStep(4);
        return;
      }
      document.body.classList.remove("how-flat");
      const r = track.getBoundingClientRect();
      const total = r.height - innerHeight;
      const p = Math.min(1, Math.max(0, -r.top / total));
      if (r.top > innerHeight) { setStep(0); return; }
      setStep(Math.min(4, 1 + Math.floor(p * 4)));
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", onScroll);
    onScroll();
  }
})();
