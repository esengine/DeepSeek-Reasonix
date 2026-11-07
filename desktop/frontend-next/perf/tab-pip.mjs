// The session tab's dot is the only at-a-glance sign of a working session: it
// breathes while the run is live and stays still in every other state, and a
// person who asked for reduced motion gets it still. Only the browser computes it.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
try {
  for (const [label, reduced] of [["normal", "no-preference"], ["reduced motion", "reduce"]]) {
    const ctx = await browser.newContext({ viewport: { width: 1200, height: 760 }, reducedMotion: reduced });
    const page = await ctx.newPage();
    await page.goto(`${BASE}?pack=photo`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    const names = await page.evaluate(() => {
      const bar = document.createElement("div");
      bar.className = "panetabs";
      bar.style.cssText = "position:fixed;left:0;top:0;opacity:0;pointer-events:none";
      bar.innerHTML = ["idle", "running", "halt", "done"].map((r) => `<div class="ptab" data-run="${r}"><i class="pip"></i></div>`).join("");
      bar.innerHTML += `<div class="ptab" data-run="done"><i class="pip" data-unread></i></div>`;
      document.body.appendChild(bar);
      const out = {};
      for (const r of ["idle", "running", "halt", "done"]) out[r] = getComputedStyle(bar.querySelector(`[data-run="${r}"] .pip`)).animationName;
      const read = getComputedStyle(bar.querySelector('[data-run="done"] .pip:not([data-unread])'));
      const unread = getComputedStyle(bar.querySelector('[data-run="done"] .pip[data-unread]'));
      out.unreadDiffers = unread.backgroundColor !== read.backgroundColor && unread.backgroundColor !== "rgba(0, 0, 0, 0)";
      return out;
    });
    check(`${label}: a running session's dot ${reduced === "reduce" ? "does not move" : "breathes"}`, reduced === "reduce" ? names.running === "none" : names.running === "m-pulse", names.running);
    check(`${label}: idle, halted and finished dots stay still`, names.idle === "none" && names.halt === "none" && names.done === "none", JSON.stringify(names));
    check(`${label}: an unread finished session is told apart by its own dot, not an extra one`, names.unreadDiffers === true);
    await ctx.close();
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
console.log("\nall passed");
