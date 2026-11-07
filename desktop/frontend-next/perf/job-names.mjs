// A background job is named for a person: the list shows the name its caller gave it, and the whole name and the full command
// appear in a tip that stays inside the window, can be selected, and opens from the keyboard as well as the pointer.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  for (const [width, height] of [[1280, 860], [400, 760]]) {
    const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width, height }, colorScheme: scheme });
    const page = await ctx.newPage();
    page.on("pageerror", (e) => fails.push("page error: " + e.message));
    await page.goto(`${BASE}?jobs=3&jobnames=1`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    await page.waitForTimeout(800);
    await page.locator('[data-action="deck.jobs"]').click();
    await page.waitForSelector(".job-deck-name");
    const tag = `${scheme} ${width}px`;
    const shown = await page.evaluate(() => [...document.querySelectorAll(".job-deck-name")].map((e) => e.textContent));
    check(`${tag}: the list shows the names, not the commands`, shown.length >= 3 && shown.every((t) => !/go test|npm run/.test(t)), shown.join(" | "));
    await page.locator(".job-deck-name").first().hover();
    await page.waitForSelector(".job-tip");
    const tip = await page.evaluate(() => {
      const el = document.querySelector(".job-tip");
      const r = el.getBoundingClientRect();
      return { inside: r.left >= 0 && r.top >= 0 && r.right <= innerWidth + 1 && r.bottom <= innerHeight + 1, lines: el.children.length, select: getComputedStyle(el).userSelect, cmd: el.querySelector(".jt-cmd")?.textContent ?? "" };
    });
    check(`${tag}: the tip has the name and the full command, inside the window`, tip.inside && tip.lines === 2 && /npm run tauri:test/.test(tip.cmd), JSON.stringify(tip));
    check(`${tag}: its text can be selected`, tip.select === "text", tip.select);
    await page.mouse.move(width - 5, 5);
    await page.waitForTimeout(200);
    check(`${tag}: it closes when the pointer leaves`, (await page.locator(".job-tip").count()) === 0);
    await page.locator(".job-deck-name").nth(1).focus();
    await page.waitForSelector(".job-tip");
    check(`${tag}: focus from the keyboard opens it too`, /内核测试/.test(await page.locator(".job-tip .jt-name").textContent()));
    await page.keyboard.press("Escape");
    const closed = await page.locator(".job-tip").waitFor({ state: "detached", timeout: 2000 }).then(() => true, () => false);
    check(`${tag}: Escape closes it`, closed);
    await ctx.close();
  }
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
console.log("\nall ok");
