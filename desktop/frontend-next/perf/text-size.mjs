// The interface text size enlarges type and nothing else: the window keeps its
// layout, no column spills sideways at the largest step, and the control that
// sets it reaches the page.
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=2&sess=6&turns=3";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};
const shotDir = process.env.SHOTS;
const STEPS = [["标准", 1], ["大", 1.15], ["更大", 1.3]];
const measure = (page) => page.evaluate(() => {
  const px = (sel, prop = "fontSize") => parseFloat(getComputedStyle(document.querySelector(sel))[prop]);
  const box = (sel) => document.querySelector(sel).getBoundingClientRect();
  const spill = [".rail", ".compose", ".panetabs", ".chrome"].map((s) => document.querySelector(s)).filter(Boolean).filter((el) => el.scrollWidth > el.clientWidth + 1).map((el) => el.className.split(" ")[0]);
  return {
    sessionTitle: px(".sesstitle"),
    newTask: px(".studio-new-task b"),
    railWidth: Math.round(box(".rail").width),
    composeWidth: Math.round(box(".compose").width),
    zoom: getComputedStyle(document.documentElement).getPropertyValue("--zoom"),
    scale: document.documentElement.style.getPropertyValue("--fs-scale"),
    spill,
  };
});

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, locale: "zh-CN", colorScheme: scheme });
  page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".sesstitle");
  const base = await measure(page);
  for (const [name, scale] of STEPS) {
    await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
    await page.waitForSelector(".prefs-nav button");
    await page.locator(".prefs-nav button", { hasText: "外观" }).first().click();
    await page.locator('[data-action="settings.page-index"][data-target="size"]').click();
    await page.locator('[data-action="appearance.text-size"]', { hasText: new RegExp(`^${name}$`) }).click();
    await page.waitForTimeout(200);
    if (shotDir && scale === 1.3) await page.screenshot({ path: `${shotDir}/${scheme}-settings-1.3.png` });
    await page.keyboard.press("Escape");
    await page.waitForTimeout(300);
    if (await page.locator(".prefs-nav").count()) {
      await page.keyboard.press("Escape");
      await page.waitForTimeout(300);
    }
    const m = await measure(page);
    const tag = `${scheme} ${name}`;
    check(`${tag}: the control reaches the page`, scale === 1 ? m.scale === "" : Number(m.scale) === scale, `--fs-scale "${m.scale}"`);
    check(`${tag}: type grows by the step`, Math.abs(m.sessionTitle / base.sessionTitle - scale) < 0.03 && Math.abs(m.newTask / base.newTask - scale) < 0.03, `${base.sessionTitle}→${m.sessionTitle}, ${base.newTask}→${m.newTask}`);
    check(`${tag}: the layout keeps its room`, m.railWidth === base.railWidth && m.composeWidth === base.composeWidth && m.zoom === base.zoom, `rail ${m.railWidth}/${base.railWidth}, composer ${m.composeWidth}/${base.composeWidth}`);
    check(`${tag}: nothing spills sideways`, m.spill.length === 0, m.spill.join(","));
    if (shotDir) await page.screenshot({ path: `${shotDir}/${scheme}-${scale}.png` });
  }
  await page.close();
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
