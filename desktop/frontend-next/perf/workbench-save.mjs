import { mkdirSync } from "node:fs";
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = process.env.SHOTS_DIR;
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"} ${name}${detail ? ` — ${detail}` : ""}`);
  if (!ok) fails.push(name);
};
if (SHOTS) mkdirSync(SHOTS, { recursive: true });

const measure = (page) => page.locator(".workbench-save").evaluate((button) => {
  const style = getComputedStyle(button);
  const bar = button.parentElement;
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 1;
  const ctx = canvas.getContext("2d", { willReadFrequently: true });
  const rgba = (color) => {
    ctx.clearRect(0, 0, 1, 1);
    ctx.fillStyle = color;
    ctx.fillRect(0, 0, 1, 1);
    return [...ctx.getImageData(0, 0, 1, 1).data].map((v, i) => i === 3 ? v / 255 : v);
  };
  const over = (front, back) => front.slice(0, 3).map((v, i) => v * front[3] + back[i] * (1 - front[3]));
  const ancestors = [];
  for (let node = button.parentElement; node; node = node.parentElement) ancestors.unshift(node);
  let ground = [255, 255, 255];
  for (const node of ancestors) ground = over(rgba(getComputedStyle(node).backgroundColor), ground);
  const opacity = Number(style.opacity);
  const fill = over(rgba(style.backgroundColor), ground);
  const ink = over(rgba(style.color), fill);
  const lum = (rgb) => rgb.map((v) => {
    v /= 255;
    return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  }).reduce((sum, v, i) => sum + v * [0.2126, 0.7152, 0.0722][i], 0);
  const [hi, lo] = [lum(over([...ink, opacity], ground)), lum(over([...fill, opacity], ground))].sort((a, b) => b - a);
  const text = [...button.childNodes].find((node) => node.nodeType === Node.TEXT_NODE && node.textContent.trim());
  const range = document.createRange();
  range.selectNodeContents(text);
  const width = button.getBoundingClientRect().width;
  const natural = range.getBoundingClientRect().width + button.querySelector("svg").getBoundingClientRect().width
    + parseFloat(style.columnGap) + parseFloat(style.paddingLeft) + parseFloat(style.paddingRight)
    + parseFloat(style.borderLeftWidth) + parseFloat(style.borderRightWidth);
  const rect = bar.getBoundingClientRect();
  const children = [...bar.children].map((node) => node.getBoundingClientRect());
  return {
    width, natural, contrast: (hi + 0.05) / (lo + 0.05), disabled: button.disabled,
    singleRow: Math.max(...children.map((r) => r.top)) < Math.min(...children.map((r) => r.bottom)),
    contained: children.every((r) => r.left >= rect.left - 1 && r.right <= rect.right + 1),
  };
});

const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    for (const locale of ["zh", "en"]) {
      for (const width of [1440, 390]) {
        for (const desktop of [true, false]) {
          const page = await browser.newPage({ viewport: { width, height: 1000 }, colorScheme: scheme, reducedMotion: "reduce" });
          page.setDefaultTimeout(15000);
          page.on("pageerror", (error) => fails.push(error.message));
          // Native capabilities render both optional toolbar controls; no native action is invoked.
          if (desktop) await page.addInitScript(() => {
            window.reasonixHost = {
              shell: "electron", platform: "win32", titleBar: false,
              isWindowMaximised: async () => false, setUnreadBadge: async () => {},
              revealPath: async () => null, revealWorkspace: async () => null,
            };
          });
          const url = new URL(BASE);
          url.searchParams.set("pref", locale);
          url.searchParams.set("ws", "2");
          url.searchParams.set("sess", "6");
          url.searchParams.set("turns", "2");
          await page.goto(url.href, { waitUntil: "networkidle" });
          const workbench = page.locator('[data-action="pane.view"][data-value="browser"]');
          if (!(await workbench.isVisible())) await page.locator('[data-action="chrome.more"]').click();
          await workbench.click();
          await page.locator('[data-action="workbench.launch-files"]').click();
          await page.locator(".workbench-filelist button").filter({ hasText: "README.md" }).click();
          await page.locator(".workbench-save").waitFor();
          await page.evaluate(() => document.fonts.ready);
          const tag = `${scheme}/${locale}/${width}/${desktop ? "desktop" : "browser"}`;
          const verify = async (state) => {
            await page.locator(".workbench-save").evaluate(async (button) => {
              await Promise.all(button.getAnimations().map((animation) => animation.finished.catch(() => {})));
            });
            const result = await measure(page);
            check(`${tag}/${state}: content-sized Save`, result.width <= result.natural + 1, `${result.width.toFixed(1)}px vs ${result.natural.toFixed(1)}px content`);
            check(`${tag}/${state}: controls share one row`, result.singleRow);
            check(`${tag}/${state}: controls stay inside toolbar`, result.contained);
            check(`${tag}/${state}: readable label`, result.contrast >= 4.5, `${result.contrast.toFixed(2)}:1`);
            check(`${tag}/${state}: native disabled state`, result.disabled === (state !== "modified"));
            if (SHOTS && desktop && locale === "zh") await page.screenshot({ path: `${SHOTS}/${scheme}-${width}-${state}.png` });
          };
          await verify("unchanged");
          await page.locator('[data-action="workbench.mode"][data-value="file"]').click();
          const editor = page.locator(".cm-content");
          await editor.fill("# Save button regression\n\nChanged in the editor.");
          await page.locator(".workbench-save:not(:disabled)").waitFor();
          await verify("modified");
          await page.locator(".workbench-save").click();
          await page.locator(".workbench-save:disabled").waitFor();
          await verify("saved");
          await page.locator('[data-action="workbench.mode"][data-value="read"]').click();
          check(`${tag}: saved content remains readable`, await page.locator(".workbench-read h1").textContent() === "Save button regression");
          await page.close();
        }
      }
    }
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`${fails.length} failures: ${fails.join("; ")}`);
  process.exitCode = 1;
}
