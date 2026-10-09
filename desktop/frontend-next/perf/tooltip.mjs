// Composer control tooltips: the box must hold its own text, must not sit under a
// dimmed ancestor, and must stay readable. All three are layout/paint questions jsdom cannot answer.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const BOX = 'textarea[role="combobox"]';
const fails = [];

const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

// Controls are found by structure, never by label: the label is translated,
// so a lookup by its wording matches one language and skips the other.
const CONTROLS = { "prompt.refine": '[data-action="prompt.refine"]', attach: "button.attach" };

const measure = (page, selector) =>
  page.evaluate((selector) => {
    const button = document.querySelector(`.compose ${selector}`);
    const tip = button?.querySelector(".studio-control-tip");
    if (!button || !tip) return null;
    let opacity = 1;
    for (let el = tip; el; el = el.parentElement) opacity *= parseFloat(getComputedStyle(el).opacity);
    const ink = document.createElement("canvas").getContext("2d", { willReadFrequently: true });
    const rgba = (css) => {
      ink.clearRect(0, 0, 1, 1);
      ink.fillStyle = "#000";
      ink.fillStyle = css;
      ink.fillRect(0, 0, 1, 1);
      const [r, g, b, a] = ink.getImageData(0, 0, 1, 1).data;
      return [r, g, b, a / 255];
    };
    const lum = ([r, g, b]) => {
      const f = (c) => ((c /= 255) <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
      return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
    };
    const over = (top, bottom) => {
      const a = top[3];
      return [0, 1, 2].map((i) => top[i] * a + bottom[i] * (1 - a)).concat(1);
    };
    const bg = rgba(getComputedStyle(tip).backgroundColor);
    const ratios = [...tip.children].map((child) => {
      const fg = over(rgba(getComputedStyle(child).color), bg);
      const [hi, lo] = [lum(fg), lum(bg)].sort((x, y) => y - x);
      return (hi + 0.05) / (lo + 0.05);
    });
    const box = tip.getBoundingClientRect();
    const spill = [...tip.children].map((child) => {
      const range = document.createRange();
      range.selectNodeContents(child);
      return Math.max(0, range.getBoundingClientRect().right - box.right);
    });
    return {
      opacity,
      visibility: getComputedStyle(tip).visibility,
      minRatio: Math.min(...ratios),
      spill: Math.max(...spill),
      scroll: tip.scrollWidth - tip.clientWidth,
      left: box.left,
      right: box.right,
      width: box.width,
      viewport: innerWidth,
      disabled: button.disabled,
    };
  }, selector);

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  for (const lang of ["zh", "en"]) {
    for (const width of [1440, 420]) {
      const page = await browser.newPage({ viewport: { width, height: 900 }, colorScheme: scheme, reducedMotion: "reduce" });
      page.setDefaultTimeout(8000);
      page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
      await page.goto(`${BASE}?pref=${lang}&turns=2`, { waitUntil: "networkidle" });
      await page.waitForSelector(".compose");
      await page.evaluate(() => document.fonts.ready);
      for (const [text, control] of [["", "prompt.refine"], ["1111", "prompt.refine"], ["", "attach"]]) {
        await page.fill(BOX, text);
        const tag = `${scheme}/${lang}/${width}px/${control}/${text ? "有字" : "空"}`;
        if (!(await page.$(`.compose ${CONTROLS[control]}`))) { check(`${tag}：找得到按钮`, false); continue; }
        await page.hover(`.compose ${CONTROLS[control]}`, { force: true });
        await page.waitForTimeout(250);
        const m = await measure(page, CONTROLS[control]);
        if (!m) { check(`${tag}：找得到提示`, false); continue; }
        check(`${tag}：提示不在半透明的祖先之下`, m.opacity === 1 && m.visibility === "visible", `有效不透明度 ${m.opacity.toFixed(2)}`);
        check(`${tag}：文字不溢出提示框`, m.spill <= 0.5 && m.scroll <= 0, `溢出 ${m.spill.toFixed(1)}px，scroll ${m.scroll}px`);
        check(`${tag}：提示框留在窗口内`, m.left >= 0 && m.right <= m.viewport, `${Math.round(m.left)}–${Math.round(m.right)} / ${m.viewport}`);
        check(`${tag}：正文对比度达 AA`, m.minRatio >= 4.5, `${m.minRatio.toFixed(2)}:1`);
      }
      await page.close();
    }
  }
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} 项不合格`);
  process.exit(1);
}
