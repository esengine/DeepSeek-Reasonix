// Reasonix — motion & FX layer (all effects respect data-motion + prefers-reduced-motion)
(function () {
  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const rich = () => document.body.dataset.motion === "rich" && !reduced;
  const lerp = (a, b, t) => a + (b - a) * t;
  const mouse = { x: innerWidth / 2, y: innerHeight / 2, active: false };
  window.addEventListener("mousemove", (e) => {
    mouse.x = e.clientX; mouse.y = e.clientY; mouse.active = true;
  }, { passive: true });

  /* particle grid (hero canvas) */
  const canvas = document.querySelector(".hero-dots");
  if (canvas) {
    const ctx = canvas.getContext("2d");
    const hero = canvas.parentElement;
    let w = 0, h = 0, dpr = 1, dots = [];
    const SP = 36;
    const build = () => {
      dpr = Math.min(devicePixelRatio || 1, 2);
      const r = hero.getBoundingClientRect();
      w = r.width; h = r.height;
      canvas.width = w * dpr; canvas.height = h * dpr;
      canvas.style.width = w + "px"; canvas.style.height = h + "px";
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      dots = [];
      for (let y = SP; y < h - 8; y += SP)
        for (let x = SP / 2 + ((y / SP) % 2 ? SP / 2 : 0); x < w - 8; x += SP)
          dots.push({ x, y, a: 0 });
    };
    build();
    window.addEventListener("resize", build);
    let mx = -9999, my = -9999;
    const accent = () =>
      getComputedStyle(document.documentElement).getPropertyValue("--accent").trim() || "#3a5fd0";
    const tick = () => {
      requestAnimationFrame(tick);
      const r = hero.getBoundingClientRect();
      if (r.bottom < 0) return;
      ctx.clearRect(0, 0, w, h);
      if (!rich()) return;
      const tx = mouse.active ? mouse.x - r.left : -9999;
      const ty = mouse.active ? mouse.y - r.top : -9999;
      mx = lerp(mx < -999 ? tx : mx, tx, 0.14);
      my = lerp(my < -999 ? ty : my, ty, 0.14);
      const col = accent();
      for (const d of dots) {
        const dist = Math.hypot(d.x - mx, d.y - my);
        const target = dist < 150 ? 1 - dist / 150 : 0;
        d.a = lerp(d.a, target, 0.12);
        const base = 0.13;
        ctx.beginPath();
        ctx.arc(d.x, d.y, 1.1 + d.a * 1.3, 0, 7);
        if (d.a > 0.02) { ctx.globalAlpha = base + d.a * 0.65; ctx.fillStyle = col; }
        else { ctx.globalAlpha = base; ctx.fillStyle = "#9aa0a6"; }
        ctx.fill();
      }
      ctx.globalAlpha = 1;
    };
    tick();
  }

  /* cursor glow */
  const glowEl = document.querySelector(".cursor-glow");
  if (glowEl && matchMedia("(pointer: fine)").matches) {
    let gx = mouse.x, gy = mouse.y;
    const move = () => {
      requestAnimationFrame(move);
      if (!rich() || !mouse.active) { glowEl.style.opacity = 0; return; }
      glowEl.style.opacity = 1;
      gx = lerp(gx, mouse.x, 0.09);
      gy = lerp(gy, mouse.y, 0.09);
      glowEl.style.transform = `translate(${gx}px, ${gy}px) translate(-50%, -50%)`;
    };
    move();
  }

  /* typewriter headline (re-runs per language) */
  const typeH1 = () => {
    const lang = document.body.dataset.lang || "en";
    const span = document.querySelector(`.hero h1 .l-${lang}`);
    if (!span || span.dataset.typed || !rich()) return;
    span.dataset.typed = "1";
    const walker = document.createTreeWalker(span, NodeFilter.SHOW_TEXT);
    const texts = [];
    while (walker.nextNode()) texts.push(walker.currentNode);
    const chars = [];
    texts.forEach((node) => {
      const frag = document.createDocumentFragment();
      for (const ch of node.textContent) {
        const s = document.createElement("span");
        s.className = "ch";
        s.textContent = ch;
        frag.appendChild(s);
        chars.push(s);
      }
      node.parentNode.replaceChild(frag, node);
    });
    const caret = document.createElement("span");
    caret.className = "type-caret";
    let i = 0;
    const step = () => {
      if (i >= chars.length) { setTimeout(() => caret.remove(), 900); return; }
      const c = chars[i++];
      c.classList.add("on");
      c.after(caret);
      setTimeout(step, c.textContent.trim() ? 34 : 10);
    };
    setTimeout(step, 250);
  };
  typeH1();
  new MutationObserver(() => typeH1())
    .observe(document.body, { attributes: true, attributeFilter: ["data-lang"] });

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
    const tilt = () => {
      requestAnimationFrame(tilt);
      if (!rich()) { term.style.transform = ""; return; }
      rx = lerp(rx, trx, 0.08);
      ry = lerp(ry, try_, 0.08);
      term.style.transform = `rotateX(${rx.toFixed(2)}deg) rotateY(${ry.toFixed(2)}deg)`;
    };
    tilt();
  }

  /* magnetic buttons */
  if (matchMedia("(pointer: fine)").matches) {
    document.querySelectorAll(".btn").forEach((btn) => {
      btn.addEventListener("mousemove", (e) => {
        if (!rich()) return;
        const r = btn.getBoundingClientRect();
        const dx = (e.clientX - r.left - r.width / 2) / r.width;
        const dy = (e.clientY - r.top - r.height / 2) / r.height;
        btn.style.transform = `translate(${dx * 5}px, ${dy * 4 - 1}px)`;
      });
      btn.addEventListener("mouseleave", () => { btn.style.transform = ""; });
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
