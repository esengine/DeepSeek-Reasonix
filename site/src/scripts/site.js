import { downloadPaneFromURL } from "./download-link.js";
import { initTheme } from "./theme.js";

// Reasonix site — vanilla interactions
(function () {
  initTheme();
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

  const flashOSCard = () => {
    if (!osCard) return;
    osCard.classList.remove("flash");
    void osCard.offsetWidth;
    setTimeout(() => osCard.classList.add("flash"), 450);
    setTimeout(() => osCard.classList.remove("flash"), 2600);
  };

  const requestedPane = downloadPaneFromURL(window.location.href);
  if (requestedPane) {
    activatePane(requestedPane);
    if (requestedPane === "desktop") flashOSCard();
    requestAnimationFrame(() => {
      document.getElementById("start")?.scrollIntoView({ block: "start" });
      queueSweep();
    });
  }

  /* links that deep-link into a specific download tab */
  document.querySelectorAll("[data-goto]").forEach((a) => {
    a.addEventListener("click", () => {
      activatePane(a.dataset.goto);
      if (a.hasAttribute("data-os-dl")) flashOSCard();
      setTimeout(queueSweep, 500);
    });
  });

  /* language switch */
  const LANG_KEY = "reasonix-lang";
  const langBtns = Array.from(document.querySelectorAll(".lang-switch button"));
  const setLang = (l, alignHash) => {
    document.body.dataset.lang = l;
    document.documentElement.lang = l === "zh" ? "zh-CN" : "en";
    const t = document.body.dataset[l === "zh" ? "titleZh" : "titleEn"];
    if (t) document.title = t;
    langBtns.forEach((b) => b.classList.toggle("active", b.dataset.lang === l));
    try { localStorage.setItem(LANG_KEY, l); } catch (e) {}
    if (alignHash && window.location.hash) {
      const target = document.getElementById(window.location.hash.slice(1));
      if (target) requestAnimationFrame(() => target.scrollIntoView({ block: "start" }));
    }
  };
  langBtns.forEach((b) => b.addEventListener("click", () => setLang(b.dataset.lang)));
  let savedLang = "";
  try { savedLang = localStorage.getItem(LANG_KEY) || ""; } catch (e) {}
  const requestedLang = new URLSearchParams(window.location.search).get("lang");
  const initialLang = requestedLang === "zh" || requestedLang === "en"
    ? requestedLang
    : savedLang || ((navigator.language || "").toLowerCase().startsWith("zh") ? "zh" : "en");
  setLang(initialLang, true);

  /* docs scrollspy */
  const sideLinks = Array.from(document.querySelectorAll(".docs-side a[href^='#']"));
  if (sideLinks.length) {
    const targets = sideLinks
      .map((a) => document.getElementById(a.getAttribute("href").slice(1)))
      .filter(Boolean)
      // Sidebar links are grouped editorially, so their order differs from the
      // page order. The spy below picks the last section past the 140px line,
      // which is only correct when targets are sorted in document order.
      .sort((a, b) =>
        a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1);
    const setActive = (id) =>
      sideLinks.forEach((a) => {
        const on = a.getAttribute("href") === "#" + id;
        a.classList.toggle("active", on);
        if (on) a.setAttribute("aria-current", "true");
        else a.removeAttribute("aria-current");
      });
    // While a click smooth-scrolls to a section, pin the highlight to it so it
    // doesn't sweep through every section scrolled past on the way. scrollend
    // releases the pin when the scroll settles — on arrival or when the user
    // takes over (wheel, touch, keyboard, scrollbar). Browsers without
    // scrollend just skip pinning: correct destination, no sweep suppression.
    let pinned = null;
    const spy = () => {
      if (pinned) return;
      let current = targets[0];
      for (const t of targets) if (t.getBoundingClientRect().top < 140) current = t;
      if (current) setActive(current.id);
    };
    if ("onscrollend" in window) {
      const ids = new Set(targets.map((t) => t.id));
      document.querySelectorAll("a[href^='#']").forEach((a) => {
        const id = a.getAttribute("href").slice(1);
        if (ids.has(id)) a.addEventListener("click", () => { pinned = id; setActive(id); });
      });
      window.addEventListener("scrollend", () => { pinned = null; spy(); }, { passive: true });
    }
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

  /* refresh the published version and immutable desktop download links between rebuilds */
  const desktopAssets = [
    "Reasonix-darwin-universal.dmg",
    "Reasonix-darwin-arm64.zip",
    "Reasonix-darwin-amd64.zip",
    "Reasonix-windows-amd64-installer.exe",
    "Reasonix-windows-arm64-installer.exe",
    "Reasonix-windows-amd64.zip",
    "Reasonix-linux-amd64.deb",
    "Reasonix-linux-amd64.tar.gz",
  ];
  const localPreview = window.location.hostname === "localhost" || window.location.hostname === "127.0.0.1";
  if (!localPreview) {
    fetch("https://dl.reasonix.io/latest/latest.json", { cache: "no-cache" })
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
      const rawVersion = String((d && d.version) || "");
      const versionMatch = rawVersion.match(/^v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)$/);
      if (!versionMatch) return;
      const v = versionMatch[1];
      const desktopBase = "https://dl.reasonix.io/desktop-v" + v;
      document.querySelectorAll(".rxv").forEach((e) => { e.textContent = v; });
      desktopAssets.forEach((asset) => {
        document.querySelectorAll('[data-desktop-asset="' + asset + '"]').forEach((a) => {
          a.href = desktopBase + "/" + asset;
        });
      });
      document.querySelectorAll("a.rxnotes").forEach((a) => {
        a.href = new URL("changelog/v" + v + "/", window.location.origin + "/").href;
      });
      })
      .catch(() => {});
  }
})();
