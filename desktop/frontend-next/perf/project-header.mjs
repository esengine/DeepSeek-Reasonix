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
    const add = page.locator('.projects-heading [data-action="workspace.add"]');
    const recent = page.locator('.projects-heading [data-action="session.recent"]');
    const more = page.locator(".projects-heading .wsmore");
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
    await more.hover();
    const moreHover = await paint(more);
    await recent.hover();
    const recentHover = await paint(recent);
    check(`${scheme}: all header actions visibly highlight on hover`,
      addHover.fill !== rest.fill && moreHover.fill !== rest.fill && recentHover.fill !== rest.fill);
    check(`${scheme}: project header actions have the same hover fill and ink`,
      addHover.fill === moreHover.fill && addHover.ink === moreHover.ink
        && recentHover.fill === addHover.fill && recentHover.ink === addHover.ink,
      JSON.stringify({ add: addHover, more: moreHover, recent: recentHover }));
    await more.click();
    const menu = page.getByRole("menu", { name: "项目列表的更多操作", exact: true });
    await menu.waitFor();
    await page.mouse.move(900, 500);
    const moreOpen = await paint(more);
    check(`${scheme}: the open project menu keeps the header action highlight`,
      moreOpen.fill === addHover.fill && moreOpen.ink === addHover.ink,
      JSON.stringify(moreOpen));
    await page.keyboard.press("Escape");
    check(`${scheme}: Escape closes the project menu and returns focus`,
      await more.getAttribute("aria-expanded") === "false" && await more.evaluate((button) => button === document.activeElement));
    await page.keyboard.press("Shift+Tab");
    const addFocus = await add.evaluate((button) => ({
      focused: button === document.activeElement,
      outline: getComputedStyle(button).outline,
    }));
    await page.keyboard.press("Shift+Tab");
    const recentFocus = await recent.evaluate((button) => ({
      focused: button === document.activeElement,
      outline: getComputedStyle(button).outline,
    }));
    await page.keyboard.press("Tab");
    await page.keyboard.press("Tab");
    const moreFocus = await more.evaluate((button) => ({
      focused: button === document.activeElement,
      outline: getComputedStyle(button).outline,
    }));
    check(`${scheme}: all header actions retain visible keyboard focus`,
      addFocus.focused && moreFocus.focused && recentFocus.focused
        && !addFocus.outline.includes("none") && !moreFocus.outline.includes("none")
        && !recentFocus.outline.includes("none"),
      JSON.stringify({ add: addFocus, more: moreFocus, recent: recentFocus }));
    await more.click();
    await menu.waitFor();
    await page.locator(".projects-heading-title").click();
    check(`${scheme}: clicking outside dismisses the menu`, await more.getAttribute("aria-expanded") === "false");
    await add.click();
    const dialog = page.getByRole("dialog", { name: "打开项目", exact: true });
    await dialog.waitFor();
    check(`${scheme}: the add action still offers the local folder picker`,
      await dialog.getByRole("button", { name: "选择本机文件夹", exact: true }).isEnabled());
    await page.keyboard.press("Escape");
    await dialog.waitFor({ state: "hidden" });
    await page.mouse.move(900, 500);
    const after = await paint(more);
    check(`${scheme}: dismissing the menu restores the resting background`, after.fill === rest.fill);
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
    await more.click();
    await page.getByRole("menuitem", { name: "选择本机会话", exact: true }).click();
    await page.getByRole("button", { name: "完成选择", exact: true }).click();
    check(`${scheme}: leaving batch selection returns focus to the fixed header`,
      await more.evaluate((button) => button === document.activeElement));
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
