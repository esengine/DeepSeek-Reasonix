// Walks every settings page of a running build and reports the combinations of
// size, weight and colour its text actually resolves to, then fails on a size
// that is not one of the scale's tokens. The stylesheet cannot answer this: a
// rule written against a literal px lands on screen as a size nothing declares.
//
//   node tools/settings-type-sweep.mjs http://localhost:5173/?pref=zh
import { chromium } from "playwright";

const url = process.argv[2];
if (!url) throw new Error("usage: node tools/settings-type-sweep.mjs <url>");
const HERO = ["uhero-v", "uhero-estv"];

const browser = await chromium.launch();
const page = await (await browser.newContext({ locale: "zh-CN", viewport: { width: 1280, height: 800 } })).newPage();
await page.addInitScript(() => {
  const known = { shell: "electron", platform: "darwin", titleBar: false, isWindowMaximised: async () => false };
  window.reasonixHost = new Proxy(known, { get: (t, k) => (k in t ? t[k] : typeof k === "string" && /^on[A-Z]/.test(k) ? () => () => {} : async () => {}) });
});
await page.goto(url, { waitUntil: "networkidle" });
await page.waitForTimeout(1500);
const skip = page.locator('[data-action="onboarding.skip"]');
if (await skip.count()) { await skip.click(); await page.waitForTimeout(1500); }
await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]').click());
await page.waitForSelector(".prefs-nav");

const scale = await page.evaluate(() => {
  const probe = document.createElement("span");
  document.body.append(probe);
  const px = (v) => { probe.style.fontSize = `var(${v})`; return getComputedStyle(probe).fontSize; };
  const sizes = ["--fs-meta", "--fs-ui", "--fs-lead", "--fs-title", "--fs-display"].map(px);
  probe.remove();
  return sizes;
});

const ids = await page.evaluate(() => [...document.querySelectorAll(".prefs-nav button[role=tab]")].map((e) => e.id));
const combos = new Map();
for (const id of ids) {
  await page.locator(`#${id}`).click();
  await page.waitForTimeout(450);
  const rows = await page.evaluate((hero) => {
    const out = [];
    const walker = document.createTreeWalker(document.querySelector(".prefs-col"), NodeFilter.SHOW_TEXT);
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      if (!n.textContent.trim()) continue;
      const el = n.parentElement;
      const cs = getComputedStyle(el);
      if (cs.display === "none" || cs.visibility === "hidden" || el.closest("option, .sr-only")) continue;
      if (hero.some((c) => el.classList.contains(c))) continue;
      out.push([cs.fontSize, cs.fontWeight, cs.color, el.tagName.toLowerCase() + (el.classList[0] ? `.${el.classList[0]}` : "")]);
    }
    return out;
  }, HERO);
  for (const [size, weight, color, who] of rows) {
    const key = `${size} ${weight} ${color}`;
    const entry = combos.get(key) ?? { n: 0, who: new Set() };
    entry.n++; entry.who.add(`${id.replace("prefs-", "")}:${who}`);
    combos.set(key, entry);
  }
}
await browser.close();

const off = [...combos].filter(([k]) => !scale.includes(k.split(" ")[0]));
for (const [k, e] of [...combos].sort((a, b) => b[1].n - a[1].n)) console.log(String(e.n).padStart(5), k, [...e.who].slice(0, 3).join(" "));
console.log(`${combos.size} combinations over ${ids.length} pages; scale ${scale.join(" ")}`);
if (off.length) {
  console.error(`off the scale: ${off.map(([k, e]) => `${k} (${[...e.who][0]})`).join("; ")}`);
  process.exit(1);
}
