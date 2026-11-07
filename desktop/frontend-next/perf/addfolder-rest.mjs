// The Projects heading's add-folder button stays visible and hit-testable
// before hover. Opacity and hit-testing need a real browser.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
try {
  for (const theme of ["light", "dark"]) {
    for (const width of [1280, 1600]) {
      const page = await browser.newPage({ viewport: { width, height: 800 }, colorScheme: theme, reducedMotion: "reduce" });
      page.on("pageerror", (e) => fails.push("page error: " + e.message));
      await page.goto(BASE, { waitUntil: "networkidle" });
      await page.waitForSelector(".projects-header");
      await page.mouse.move(width - 2, 790);
      await page.waitForTimeout(300);
      const g = await page.evaluate(() => {
        const row = document.querySelector(".projects-header");
        const add = row?.querySelector('[data-action="workspace.add"]');
        if (!add) return null;
        const r = add.getBoundingClientRect();
        const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
        let seen = 1;
        for (let el = add; el && el !== document.body; el = el.parentElement) seen *= Number(getComputedStyle(el).opacity);
        return { opacity: seen, width: r.width, height: r.height, hits: hit === add || add.contains(hit), shown: row.matches(":hover") };
      });
      const where = `${theme} ${width}`;
      check(`${where}: the Projects heading has the add-folder button`, g !== null);
      if (!g) continue;
      check(`${where}: it is visible without hovering the row`, !g.shown && g.opacity === 1 && g.width > 0 && g.height > 0, JSON.stringify(g));
      check(`${where}: it takes the pointer`, g.hits);
      await page.close();
    }
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
console.log("\nall passed");
