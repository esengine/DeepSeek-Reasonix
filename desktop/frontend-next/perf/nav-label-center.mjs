// A label's ink must centre on its icon's: the line box centres on the row, but a CJK label's ink sits above the
// line box's middle, so a row can look top-heavy while every box measurement says it is level.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: "reduce" });
await ctx.addInitScript(() => localStorage.setItem("rx-nav-rail", "off"));
const page = await ctx.newPage();
page.on("pageerror", (e) => fails.push("page error: " + e.message));
await page.goto(BASE, { waitUntil: "networkidle" });
await page.waitForSelector(".studio-quicknav button");
await page.evaluate(() => document.fonts.ready);
const rows = await page.evaluate(() => {
  return [...document.querySelectorAll(".studio-new-task, .studio-search, .studio-quicknav button")].map((row) => {
    const box = row.getBoundingClientRect();
    const icon = row.querySelector("span").getBoundingClientRect();
    return { name: row.textContent.trim(), x: box.x, y: box.y, w: box.width, h: box.height, iconRight: icon.right - box.x };
  });
});
for (const r of rows) {
  const shot = await page.screenshot({ clip: { x: r.x, y: r.y, width: r.w, height: r.h } });
  const mids = await page.evaluate(async ({ data, split }) => {
    const img = await createImageBitmap(await (await fetch("data:image/png;base64," + data)).blob());
    const c = new OffscreenCanvas(img.width, img.height);
    const g = c.getContext("2d");
    g.drawImage(img, 0, 0);
    const px = g.getImageData(0, 0, img.width, img.height).data;
    const mid = (x0, x1) => {
      let top = Infinity, bot = -1;
      for (let y = 0; y < img.height; y++) for (let x = x0; x < Math.min(x1, img.width); x++) {
        const o = (y * img.width + x) * 4;
        if ((px[o] + px[o + 1] + px[o + 2]) / 3 < 170) { top = Math.min(top, y); bot = Math.max(bot, y); }
      }
      return (top + bot) / 2;
    };
    return { icon: mid(0, split), text: mid(split, img.width) };
  }, { data: shot.toString("base64"), split: Math.round(r.iconRight + 4) });
  const off = Math.abs(mids.text - mids.icon);
  check(`${r.name}: label and icon centre within 1px`, off <= 1, `icon ${mids.icon} text ${mids.text}`);
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
console.log("\nall ok");
