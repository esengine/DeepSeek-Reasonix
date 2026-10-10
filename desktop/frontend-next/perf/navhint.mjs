// Icon-rail hover hints: each must be drawn, fully inside the window, and not clipped by any ancestor.
// Clipping is a layout question jsdom cannot answer. Modes are the stored rail choice; "collapsed" also closes the workspace rail.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = process.env.NAVHINT_SHOTS;
const fails = [];

const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const measure = (page, index) =>
  page.evaluate((index) => {
    const tip = document.querySelectorAll(".nav .navbtn")[index]?.querySelector(".navhint");
    if (!tip) return null;
    const box = tip.getBoundingClientRect();
    let opacity = 1;
    let clipped = null;
    const view = { left: 0, top: 0, right: innerWidth, bottom: innerHeight };
    for (let el = tip; el; el = el.parentElement) {
      const cs = getComputedStyle(el);
      opacity *= parseFloat(cs.opacity);
      if (el === tip) continue;
      if ([cs.overflowX, cs.overflowY].every((o) => o === "visible")) continue;
      const r = el.getBoundingClientRect();
      const out = Math.max(r.left - box.left, box.right - r.right, r.top - box.top, box.bottom - r.bottom, 0);
      if (out > 0.5 && !clipped) clipped = { by: el.className || el.tagName, px: out };
    }
    const outside = Math.max(view.left - box.left, box.right - view.right, view.top - box.top, box.bottom - view.bottom, 0);
    const prior = tip.style.pointerEvents;
    tip.style.pointerEvents = "auto";
    const top = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
    tip.style.pointerEvents = prior;
    const covered = top && !tip.contains(top) ? top.getAttribute("class") || top.tagName : null;
    return { covered, opacity, visibility: getComputedStyle(tip).visibility, clipped, outside, width: box.width, text: tip.textContent };
  }, index);

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  for (const mode of ["on", "collapsed"]) {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: scheme, reducedMotion: "reduce" });
    page.setDefaultTimeout(8000);
    page.on("pageerror", (e) => fails.push("page error: " + e.message));
    await page.addInitScript((m) => localStorage.setItem("rx-nav-rail", m), mode);
    await page.goto(`${BASE}?pref=en&turns=2`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    await page.evaluate(() => document.fonts.ready);
    if (mode === "collapsed") {
      const edge = await page.evaluate(() => document.querySelector(".nav").getBoundingClientRect().right);
      check(`${scheme}/collapsed: rail is off screen while the workspace rail is open`, edge <= 0.5, `right edge ${edge}px`);
      await page.click('[data-action="chrome.rail"]');
      await page.waitForTimeout(600);
    }
    const count = await page.locator(".nav .navbtn").count();
    check(`${scheme}/${mode}: rail has icons`, count >= 6, `${count}`);
    for (let i = 0; i < count; i++) {
      const btn = page.locator(".nav .navbtn").nth(i);
      await btn.hover();
      await page.waitForTimeout(300);
      const m = await measure(page, i);
      const tag = `${scheme}/${mode}/#${i} ${m?.text ?? "?"}`;
      if (!m) { check(`${tag}: hint exists`, false); continue; }
      check(`${tag}: hint drawn`, m.opacity === 1 && m.visibility === "visible", `opacity ${m.opacity.toFixed(2)}`);
      check(`${tag}: hint inside window`, m.outside <= 0.5, `${m.outside.toFixed(1)}px out`);
      check(`${tag}: no ancestor clips the hint`, !m.clipped, m.clipped ? `${m.clipped.by} clips ${m.clipped.px.toFixed(1)}px` : "");
      check(`${tag}: nothing paints over the hint`, !m.covered, m.covered ? `${m.covered} is on top` : "");
      if (SHOTS && (i === 0 || i === count - 1)) {
        await page.screenshot({ path: `${SHOTS}-${scheme}-${mode}-${i}.png`, clip: { x: 0, y: 0, width: 420, height: 900 } });
      }
    }
    await page.close();
  }
  const off = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: scheme, reducedMotion: "reduce" });
  await off.addInitScript(() => localStorage.setItem("rx-nav-rail", "off"));
  await off.goto(`${BASE}?pref=en&turns=2`, { waitUntil: "networkidle" });
  await off.waitForSelector(".app");
  check(`${scheme}/off: no rail drawn`, (await off.locator(".nav").count()) === 0);
  await off.close();
  const phone = await browser.newPage({ viewport: { width: 420, height: 900 }, colorScheme: scheme, reducedMotion: "reduce" });
  await phone.goto(`${BASE}?pref=en&turns=2`, { waitUntil: "networkidle" });
  await phone.waitForSelector(".app");
  check(`${scheme}/420px: no rail drawn`, (await phone.locator(".nav").count()) === 0);
  await phone.close();
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
