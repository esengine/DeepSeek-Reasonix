// Reasonix site — vanilla interactions
(function () {
  const motionOK = () =>
    document.body.dataset.motion === "rich" &&
    !window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  const nav = document.querySelector(".nav");
  if (nav) {
    const onScroll = () => nav.classList.toggle("scrolled", window.scrollY > 12);
    window.addEventListener("scroll", onScroll, { passive: true });
    onScroll();
  }

  const revealEls = Array.from(document.querySelectorAll(".reveal"));
  const inView = (el, factor) =>
    el.getBoundingClientRect().top < window.innerHeight * (factor || 0.95);

  const term = document.querySelector(".term");
  const lines = Array.from(document.querySelectorAll(".term-body .tl"));
  let played = false;
  const playTerm = () => {
    if (played) return;
    played = true;
    const fire = () => document.dispatchEvent(new CustomEvent("rx:term-played"));
    if (!motionOK()) {
      lines.forEach((l) => l.classList.add("on"));
      fire();
      return;
    }
    lines.forEach((l, i) => setTimeout(() => l.classList.add("on"), 350 + i * 520));
    setTimeout(fire, 350 + Math.max(0, lines.length - 2) * 520);
  };

  let sweepQueued = false;
  const sweep = () => {
    sweepQueued = false;
    revealEls.forEach((el) => {
      if (!el.classList.contains("in") && inView(el, 0.95)) el.classList.add("in");
    });
    if (term && !played && inView(term, 0.85)) playTerm();
  };
  const queueSweep = () => {
    if (sweepQueued) return;
    sweepQueued = true;
    requestAnimationFrame(sweep);
  };
  window.addEventListener("scroll", queueSweep, { passive: true });
  window.addEventListener("resize", queueSweep, { passive: true });
  window.addEventListener("load", queueSweep);
  sweep();
  setTimeout(sweep, 400);

  /* contributors marquee — duplicate the server-rendered set for a seamless loop */
  document.querySelectorAll(".crew-row").forEach((row) => {
    const set = row.querySelector(".crew-set");
    if (set) row.appendChild(set.cloneNode(true));
  });

  /* download / channel tabs */
  const tabs = Array.from(document.querySelectorAll(".dl-tab"));
  const panes = Array.from(document.querySelectorAll(".dl-pane"));
  const activatePane = (name) => {
    tabs.forEach((b) => b.classList.toggle("active", b.dataset.pane === name));
    panes.forEach((p) => p.classList.toggle("active", p.dataset.pane === name));
  };
  tabs.forEach((tab) => {
    tab.addEventListener("click", () => activatePane(tab.dataset.pane));
  });

  /* OS detection — hero download button + card badge + highlight */
  const ua = navigator.userAgent;
  const os = /Windows/i.test(ua) ? "win" : /Mac|iPhone|iPad/i.test(ua) ? "mac" : /Linux|X11/i.test(ua) ? "linux" : "mac";
  const osNames = { mac: "macOS", win: "Windows", linux: "Linux" };
  document.querySelectorAll("[data-os-dl] .os-name").forEach((s) => (s.textContent = osNames[os]));
  const osCard = document.querySelector('.os-card[data-os="' + os + '"]');
  if (osCard) {
    osCard.classList.add("detected");
    const chip = document.createElement("span");
    chip.className = "os-chip";
    chip.innerHTML = '<span class="l-en">your OS</span><span class="l-zh">当前系统</span>';
    osCard.appendChild(chip);
  }

  /* links that deep-link into a specific download tab */
  document.querySelectorAll("[data-goto]").forEach((a) => {
    a.addEventListener("click", () => {
      activatePane(a.dataset.goto);
      if (a.hasAttribute("data-os-dl") && osCard) {
        osCard.classList.remove("flash");
        void osCard.offsetWidth;
        setTimeout(() => osCard.classList.add("flash"), 450);
        setTimeout(() => osCard.classList.remove("flash"), 2600);
      }
      setTimeout(queueSweep, 500);
    });
  });

  /* language switch */
  const LANG_KEY = "reasonix-lang";
  const langBtns = Array.from(document.querySelectorAll(".lang-switch button"));
  const setLang = (l) => {
    document.body.dataset.lang = l;
    document.documentElement.lang = l === "zh" ? "zh-CN" : "en";
    const t = document.body.dataset[l === "zh" ? "titleZh" : "titleEn"];
    if (t) document.title = t;
    langBtns.forEach((b) => b.classList.toggle("active", b.dataset.lang === l));
    try { localStorage.setItem(LANG_KEY, l); } catch (e) {}
  };
  langBtns.forEach((b) => b.addEventListener("click", () => setLang(b.dataset.lang)));
  let savedLang = "";
  try { savedLang = localStorage.getItem(LANG_KEY) || ""; } catch (e) {}
  setLang(savedLang || ((navigator.language || "").toLowerCase().startsWith("zh") ? "zh" : "en"));

  /* docs scrollspy */
  const sideLinks = Array.from(document.querySelectorAll(".docs-side a[href^='#']"));
  if (sideLinks.length) {
    const targets = sideLinks
      .map((a) => document.getElementById(a.getAttribute("href").slice(1)))
      .filter(Boolean);
    const spy = () => {
      let current = targets[0];
      for (const t of targets) if (t.getBoundingClientRect().top < 140) current = t;
      sideLinks.forEach((a) =>
        a.classList.toggle("active", current && a.getAttribute("href") === "#" + current.id));
    };
    window.addEventListener("scroll", spy, { passive: true });
    spy();
  }

  /* copy-to-clipboard */
  document.querySelectorAll("[data-copy]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const text = btn.getAttribute("data-copy");
      const done = () => {
        btn.classList.add("copied");
        const prev = btn.textContent;
        btn.textContent = "Copied";
        setTimeout(() => { btn.classList.remove("copied"); btn.textContent = prev; }, 1600);
      };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(done).catch(done);
      } else done();
    });
  });

  /* refresh the Go-preview version from the published manifest between rebuilds */
  fetch("https://dl.reasonix.io/latest/latest.json", { cache: "no-cache" })
    .then((r) => (r.ok ? r.json() : null))
    .then((d) => {
      const v = String((d && d.version) || "").replace(/^v/, "");
      if (!v) return;
      document.querySelectorAll(".rxv").forEach((e) => { e.textContent = v; });
      document.querySelectorAll("a.rxnotes").forEach((a) => {
        a.href = a.href.replace(/releases\/tag\/v[^/]*$/, "releases/tag/v" + v);
      });
    })
    .catch(() => {});
})();
