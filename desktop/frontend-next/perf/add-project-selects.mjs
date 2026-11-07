// Adding a project selects it and opens a new session there: the sidebar marks
// the new folder as the current one, and no other folder keeps that mark.
import { chromium } from "playwright";
import { mkdirSync } from "node:fs";
import { join } from "node:path";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = process.env.ADDPROJECT_SHOTS;
if (SHOTS) mkdirSync(SHOTS, { recursive: true });
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1280, height: 800 }, colorScheme: scheme });
  const page = await ctx.newPage();
  page.on("pageerror", (e) => fails.push("page error: " + e.message));
  await page.goto(PAGE, { waitUntil: "domcontentloaded" });
  await page.waitForSelector('.projects-header [data-action="workspace.add"]');
  const current = () => page.evaluate(() => [...document.querySelectorAll(".wsnode[data-current]")].map((el) => el.textContent ?? ""));
  const was = await current();
  if (SHOTS) await page.screenshot({ path: join(SHOTS, `${scheme}-before.png`) });
  await page.locator('.projects-header [data-action="workspace.add"]').click();
  await page.getByRole("dialog", { name: "打开项目", exact: true }).getByRole("button", { name: "选择本机文件夹", exact: true }).click();
  await page.waitForFunction(() => [...document.querySelectorAll(".wsnode[data-current]")].some((el) => el.textContent?.includes("mock-workspace")), null, { timeout: 5000 }).catch(() => {});
  const now = await current();
  check(`${scheme}: the added project becomes the current one`, now.length === 1 && now[0].includes("mock-workspace"), now.join("|").slice(0, 120));
  check(`${scheme}: it was not already current`, !was.some((text) => text.includes("mock-workspace")));
  if (SHOTS) await page.screenshot({ path: join(SHOTS, `${scheme}-after.png`) });
  await ctx.close();
}
await browser.close();
if (fails.length) {
  console.error(`\n${fails.length} failed`);
  process.exit(1);
}
