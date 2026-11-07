// The controls in the composer's bottom row (mode, branch, permission, model, effort) read as one family:
// hovered, each takes the same corner radius and the same ground. A control with its own radius or its own
// hover colour is the kind of slip a reader sees at once. Hover state is a question for a real browser.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=2&sess=6&turns=2";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const CONTROLS = [".studio-mode-picker", ".studio-branch", ".model-picker", ".compose .mode.plain:not(.studio-branch):not(.attach):not(.studio-attach)"];

const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) for (const turns of [2, 0]) {
    const context = `${scheme}, ${turns ? "existing conversation" : "new conversation"}`;
    const url = new URL(BASE);
    url.searchParams.set("turns", String(turns));
    const page = await browser.newPage({ colorScheme: scheme, viewport: { width: 1100, height: 760 } });
    await page.goto(url.href, { waitUntil: "networkidle" });
    await page.waitForSelector(turns ? ".compose .studio-branch" : ".compose .worktree-trigger");
    const inline = await page.locator(".compose .studio-branch").first().evaluate((e) => !!e.closest(".turntools"));
    check(`${context}: workspace control stays in the toolbar`, inline);
    const seen = [];
    for (const sel of CONTROLS) {
      const el = page.locator(`.compose ${sel}`.replace(".compose .compose", ".compose")).first();
      if (!(await el.count())) continue;
      await el.hover();
      await page.waitForTimeout(300);
      seen.push([sel, await el.evaluate((e) => ({ radius: getComputedStyle(e).borderRadius, bg: getComputedStyle(e).backgroundColor }))]);
    }
    check(`${context}: the hovered controls were all found`, seen.length === CONTROLS.length, `${seen.length} of ${CONTROLS.length}`);
    const radii = new Set(seen.map(([, v]) => v.radius));
    const grounds = new Set(seen.map(([, v]) => v.bg));
    check(`${context}: one corner radius across the row`, radii.size === 1, [...radii].join(" / "));
    check(`${context}: one hover ground across the row`, grounds.size === 1, [...grounds].join(" / "));
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(url.href, { waitUntil: "networkidle" });
    const more = page.locator(".compose .turn-more");
    if (await more.isVisible()) await more.click();
    const chip = page.locator(turns ? ".compose .studio-branch" : ".compose .worktree-trigger").first();
    await chip.click();
    check(`${context}: a narrow-pane pointer click opens the workspace menu`, await chip.getAttribute("aria-expanded") === "true");
    await page.close();
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
