import assert from "node:assert/strict";
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const failures = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? " — " + detail : ""}`);
  if (!ok) failures.push(name);
};

const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: scheme, reducedMotion: "reduce" });
    page.setDefaultTimeout(8000);
    page.on("pageerror", (error) => failures.push(error.message));
    await page.goto(`${BASE}?pref=zh&turns=3`, { waitUntil: "networkidle" });
    const add = page.locator('.projects-header [data-action="workspace.add"]');
    const recent = page.locator('.projects-header [data-action="session.recent"]');
    const fold = page.locator('.projects-header [data-action="workspace.fold-all"]');
    await add.waitFor();
    await page.addStyleTag({ content: "*{transition:none!important;animation:none!important}" });
    const paint = (control) => control.evaluate((button) => {
      const style = getComputedStyle(button);
      return { fill: style.backgroundColor, ink: style.color };
    });
    await page.mouse.move(900, 500);
    const rest = await paint(add);
    await add.hover();
    const addHover = await paint(add);
    await fold.hover();
    const foldHover = await paint(fold);
    check(`${scheme}: the header icon actions visibly highlight on hover`,
      addHover.fill !== rest.fill && foldHover.fill !== rest.fill);
    check(`${scheme}: project header icon actions have the same hover fill and ink`,
      addHover.fill === foldHover.fill && addHover.ink === foldHover.ink,
      JSON.stringify({ add: addHover, fold: foldHover }));
    await fold.click();
    check(`${scheme}: the fold action collapses every project`, await fold.getAttribute("aria-expanded") === "false");
    await fold.click();
    check(`${scheme}: and expands them again`, await fold.getAttribute("aria-expanded") === "true");
    await add.focus();
    await page.keyboard.press("Shift+Tab");
    await page.keyboard.press("Tab");
    const addFocus = await add.evaluate((button) => ({
      focused: button === document.activeElement,
      outline: getComputedStyle(button).outline,
    }));
    check(`${scheme}: the add action retains visible keyboard focus`,
      addFocus.focused && !addFocus.outline.includes("none"), JSON.stringify({ add: addFocus }));
    await add.click();
    const dialog = page.getByRole("dialog", { name: "打开项目", exact: true });
    await dialog.waitFor();
    check(`${scheme}: the add action still offers the local folder picker`,
      await dialog.getByRole("button", { name: "选择本机文件夹", exact: true }).isEnabled());
    await page.keyboard.press("Escape");
    await dialog.waitFor({ state: "hidden" });
    await page.mouse.move(900, 500);
    const after = await paint(add);
    check(`${scheme}: closing the dialog restores the resting background`, after.fill === rest.fill);
    await recent.click();
    const recentDialog = page.getByRole("dialog", { name: "最近的会话", exact: true });
    await recentDialog.locator(".recent-session").first().waitFor();
    check(`${scheme}: Recent opens saved session discovery`, await recentDialog.isVisible());
    await page.keyboard.press("Escape");
    await recentDialog.waitFor({ state: "hidden" });
    check(`${scheme}: closing Recent returns focus to its own header entry`,
      await recent.evaluate((button) => button === document.activeElement));
    await page.goto(`${BASE}?pref=zh&ws=20&sess=8&turns=3`, { waitUntil: "networkidle" });
    await add.waitFor();
    const headerTop = await add.evaluate((button) => button.getBoundingClientRect().top);
    const tree = page.getByRole("tree", { name: "项目与会话", exact: true });
    const rows = tree.getByRole("treeitem");
    await rows.first().focus();
    await page.keyboard.press("End");
    const last = rows.last();
    check(`${scheme}: End reaches the final local or remote row`,
      await last.evaluate((row) => row === document.activeElement));
    const bottom = await last.evaluate((row) => {
      const scroller = row.closest(".scroll");
      const r = row.getBoundingClientRect(), s = scroller.getBoundingClientRect();
      return { top: scroller.scrollTop, height: scroller.clientHeight, visible: r.top >= s.top && r.bottom <= s.bottom };
    });
    check(`${scheme}: a long project list scrolls to its final rows`, bottom.top > bottom.height && bottom.visible);
    const addAtEnd = await add.evaluate((button) => {
      const r = button.getBoundingClientRect();
      return { top: r.top, reached: button.contains(document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)) };
    });
    check(`${scheme}: project controls stay fixed and clickable while the tree scrolls`,
      addAtEnd.top === headerTop && addAtEnd.reached, JSON.stringify(addAtEnd));
    await page.keyboard.press("Home");
    check(`${scheme}: Home returns to the first project`,
      await rows.first().evaluate((row) => row === document.activeElement));
    await page.keyboard.press("Shift+F10");
    await page.getByRole("menu", { name: "项目操作", exact: true }).waitFor();
    await page.keyboard.press("Escape");
    await add.click();
    await dialog.waitFor();
    check(`${scheme}: the fixed add action retains local and remote project locations`,
      await dialog.getByRole("button", { name: "选择本机文件夹", exact: true }).isEnabled()
        && await dialog.getByRole("button", { name: "gpu-box · SSH", exact: true }).isEnabled());
    await page.keyboard.press("Escape");
    await dialog.waitFor({ state: "hidden" });
    await page.close();
  }
} finally {
  await browser.close();
}
assert.deepEqual(failures, [], "project header interaction regressions");
