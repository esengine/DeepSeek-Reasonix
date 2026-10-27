import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail) => {
  console.log(`${ok ? "  ok" : "FAIL"} ${name}: ${JSON.stringify(detail)}`);
  if (!ok) fails.push(name);
};
const textKeys = ["title", "count", "name", "arg", "normalization", "originalInput"];
const weightKeys = textKeys.filter(key => key !== "title" && key !== "count");
const browser = await chromium.launch();
for (const lang of ["zh", "en"]) {
  for (const fold of [false, true]) {
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, reducedMotion: "reduce" });
    page.on("pageerror", (e) => fails.push(e.message));
    await page.goto(`${BASE}?pref=${lang}${fold ? "&fold" : ""}`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    await page.evaluate(async () => {
      const frame = () => new Promise((r) => requestAnimationFrame(r));
      window.__feed({ kind: "turn_started", authoredTurn: 1, msgIndex: 0, text: "Inspect the file" });
      window.__feed({ kind: "text", text: "I will inspect it first." });
      window.__feed({ kind: "message", text: "I will inspect it first." });
      const tool = {
        id: "typography-read", name: "read_file", args: '{"path":"src/main.go"}',
        argumentNormalization: { reason: "json_string_unwrapped", original: '"{\\"path\\":\\"src/main.go\\"}"', effective: '{"path":"src/main.go"}' },
      };
      window.__feed({ kind: "tool_dispatch", tool });
      window.__feed({ kind: "tool_result", tool: { ...tool, output: "package main", durationMs: 120 } });
      await frame();
      window.__feed({ kind: "text", text: "The file declares package main." });
      window.__feed({ kind: "message", text: "The file declares package main." });
      window.__feed({ kind: "turn_done" });
    });
    await page.waitForSelector(".activity-title");
    while (await page.locator(".activity-group:not([open]) > summary").count()) {
      await page.locator(".activity-group:not([open]) > summary").first().click();
    }
    await page.waitForSelector(".activity-body .hl .nm", { state: "visible" });
    await page.locator(".activity-body .tool-disclosure > summary .nm").first().click();
    await page.waitForSelector(".activity-call-input > details > summary", { state: "visible" });
    check(`${lang}/${fold}: original input starts folded`, await page.locator(".activity-call-input > details").evaluate((el) => !el.open));
    await page.locator(".activity-call-input > details > summary").click();
    check(`${lang}/${fold}: original input opens with its source`, await page.locator(".activity-call-input > details pre").textContent() === '"{\\"path\\":\\"src/main.go\\"}"');
    const measure = async () => {
      await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      return page.evaluate(() => {
        const style = (selector) => {
          const s = getComputedStyle(document.querySelector(selector));
          return { size: parseFloat(s.fontSize), weight: Number(s.fontWeight), family: s.fontFamily };
        };
        return {
          title: style(".activity-title"), count: style(".activity-count"),
          name: style(".activity-body .hl .nm"), arg: style(".activity-body .hl .arg"),
          normalization: style(".activity-call-input > p"), originalInput: style(".activity-call-input > details > summary"),
          sidebar: style(".sesstitle"), answer: style('.call[data-k="say"] .out .txt'),
        };
      });
    };
    const base = await measure();
    check(`${lang}/${fold}: activity labels preserve the 11px/500 default`, base.title.size === 11 && base.count.size === 11 && base.title.weight === 500 && base.count.weight === 500, { title: base.title, count: base.count });
    const pick = async (action, index) => {
      await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]').click());
      await page.waitForSelector(".prefs-nav button");
      await page.evaluate(() => document.getElementById("prefs-appearance").click());
      const variable = action === "appearance.reading-size" ? "--read" : "--fs-scale";
      const expected = action === "appearance.reading-size"
        ? `${(lang === "zh" ? [12, 13, 15, 17] : [11.5, 12.5, 14.5, 16.5])[index]}px` : "1.3";
      await page.locator(`[data-action="${action}"]`).nth(index).click();
      await page.waitForFunction(([name, value]) => document.documentElement.style.getPropertyValue(name) === value, [variable, expected]);
      await page.keyboard.press("Escape");
    };
    await pick("appearance.reading-size", 3);
    const large = await measure();
    const ratio = large.answer.size / base.answer.size;
    const tag = `${lang}/${fold ? "folded turn" : "activity"}`;
    for (const key of textKeys) {
      check(`${tag} ${key} follows body size`, Math.abs(large[key].size / base[key].size - ratio) < .02, { before: base[key].size, after: large[key].size, ratio });
    }
    check(`${tag} body size leaves sidebar alone`, large.sidebar.size === base.sidebar.size, large.sidebar);
    await pick("appearance.text-size", 3);
    const uiLarge = await measure();
    for (const key of textKeys) {
      const expectedRatio = key === "title" || key === "count" ? 1.3 : 1;
      check(`${tag} ${key} ${expectedRatio === 1 ? "independent of" : "scales with"} interface size`,
        Math.abs(uiLarge[key].size / large[key].size - expectedRatio) < .02, uiLarge[key]);
    }
    check(`${tag} interface size still enlarges sidebar`, uiLarge.sidebar.size > large.sidebar.size, uiLarge.sidebar);
    await page.evaluate(() => { document.documentElement.dataset.weight = "light"; });
    const light = await measure();
    await page.evaluate(() => { document.documentElement.dataset.weight = "heavy"; });
    const heavy = await measure();
    for (const key of weightKeys) {
      check(`${tag} ${key} follows weight preference`, heavy[key].weight > light[key].weight, { light: light[key].weight, heavy: heavy[key].weight });
    }
    check(`${tag} activity labels retain one 500 weight`, heavy.title.weight === 500 && heavy.count.weight === 500, { title: heavy.title, count: heavy.count });
    await page.evaluate(() => {
      document.documentElement.style.setProperty("--ui", "Georgia, serif");
      document.documentElement.style.setProperty("--mono", "Menlo, monospace");
    });
    const fonts = await measure();
    check(`${tag} labels use prose family; commands use mono`, fonts.title.family.startsWith("Georgia") && fonts.name.family.startsWith("Menlo") && fonts.arg.family.startsWith("Menlo"), fonts);
    await page.evaluate(() => {
      window.__feed({ kind: "turn_started", authoredTurn: 2, msgIndex: 1, text: "Inspect failure and running labels" });
      const failed = { id: "scale-failure", name: "bash", args: '{"command":"false"}' };
      window.__feed({ kind: "tool_dispatch", tool: failed });
      window.__feed({ kind: "tool_result", tool: { ...failed, err: "exit status 1", output: "failed", durationMs: 120 } });
      window.__feed({ kind: "tool_dispatch", tool: { id: "scale-running", name: "bash", args: '{"command":"sleep 10"}' } });
    });
    await page.waitForSelector(".activity-running");
    for (const step of [0, 1, 2, 3]) {
      await pick("appearance.reading-size", step);
      await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      const labels = await page.locator(".activity-group[data-running] > summary").evaluate(el =>
        [...el.querySelectorAll(".activity-title, .activity-running, .activity-errors, .activity-count")].map(span => {
          const css = getComputedStyle(span);
          return { label: span.className, size: parseFloat(css.fontSize), weight: Number(css.fontWeight), lineHeight: css.lineHeight };
        }));
      const read = (lang === "zh" ? [12, 13, 15, 17] : [11.5, 12.5, 14.5, 16.5])[step];
      const expected = 11 * 1.3 * read / (lang === "zh" ? 13 : 12.5);
      check(`${tag} body step ${step}: title/running/failed/count compose text and reading scales at weight 500`,
        labels.length === 4 && labels.every(label => Math.abs(label.size - expected) < .01 && label.weight === 500 && label.lineHeight === labels[0].lineHeight), labels);
    }
    await page.close();
  }
}
await browser.close();
if (fails.length) throw new Error(`${fails.length} checks failed: ${fails.join(", ")}`);
