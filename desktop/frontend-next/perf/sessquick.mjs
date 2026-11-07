// Opt-in session actions put Pin and Archive after the title.
// Their visibility, geometry and pointer targets need a real browser.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = process.env.SHOTS;
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    for (const on of [null, false, true]) {
      const mode = on === null ? "default" : on ? "on" : "off";
      const ctx = await browser.newContext({ locale: "zh-CN", colorScheme: scheme, viewport: { width: 1400, height: 800 } });
      if (on !== null) await ctx.addInitScript((enabled) => localStorage.setItem("rx-session-quick-actions", enabled ? "on" : "off"), on);
      const page = await ctx.newPage();
      page.on("pageerror", (e) => fails.push(e.message));
      await page.goto(BASE, { waitUntil: "networkidle" });
      await page.waitForSelector(".sessrow", { timeout: 20000 });
      for (const kind of ["context", "remote"]) {
        const tag = `${scheme}/${mode}/${kind}`;
        const row = page.locator(`.sessrow-${kind}`).first();
        await row.hover();
        await page.waitForTimeout(150);
        const quick = row.locator('[data-action="session.pin"], [data-action="session.archive"]');
        if (!on) {
          check(`${tag}: hover draws no quick buttons`, (await quick.count()) === 0);
        } else {
          const state = await row.evaluate((el) => {
            const title = el.querySelector(".sesstitle > span").getBoundingClientRect();
            const pin = el.querySelector('[data-action="session.pin"]');
            const archive = el.querySelector('[data-action="session.archive"]');
            const hit = (button) => {
              if (!button) return false;
              const r = button.getBoundingClientRect();
              const at = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
              let opacity = 1;
              for (let node = button; node && node !== el; node = node.parentElement) opacity *= Number(getComputedStyle(node).opacity);
              return opacity === 1 && r.width > 0 && r.height > 0 && !button.disabled && (at === button || button.contains(at));
            };
            return { pin: hit(pin), archive: hit(archive), leading: pin.getBoundingClientRect().left >= title.right - 1,
              trailing: archive.getBoundingClientRect().left >= title.right - 1 };
          });
          check(`${tag}: Pin beside the title and Archive after it are hit-testable`, state.pin && state.archive && state.leading && state.trailing, JSON.stringify(state));
          await page.mouse.move(1398, 798);
          await row.locator('[data-action="session.pin"]').focus();
          await page.waitForTimeout(150);
          check(`${tag}: keyboard focus reveals both actions`, await quick.evaluateAll((buttons) => buttons.every((b) => {
            const r = b.getBoundingClientRect();
            const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
            let opacity = 1;
            for (let node = b; node && !node.classList.contains("sessrow"); node = node.parentElement) opacity *= Number(getComputedStyle(node).opacity);
            return opacity === 1 && (b === hit || b.contains(hit));
          })));
        }
        await row.click({ button: "right" });
        check(`${tag}: the menu retains Pin and Archive`, await page.getByRole("menuitem", { name: "置顶会话", exact: true }).count() === 1 && await page.getByRole("menuitem", { name: "归档会话", exact: true }).count() === 1);
        check(`${tag}: Delete stays in the menu`, await page.getByRole("menuitem", { name: "删除会话", exact: true }).count() === 1);
        await page.keyboard.press("Escape");
      }
      if (SHOTS) await page.screenshot({ path: `${SHOTS}/sessquick-${mode}-${scheme}.png`, clip: { x: 0, y: 0, width: 420, height: 800 } });
      await ctx.close();
    }
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
console.log("\nall passed");
