// Session messages are switched on from the settings page, not from a config
// file; a message held for approval can be let through for its project for good,
// and says where that is kept and undone; and a saved switch says when it applies.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};
const shotDir = process.env.SHOTS;

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  for (const width of [1280, 420]) {
    const tag = `${scheme} ${width}px`;
    const page = await browser.newPage({ viewport: { width, height: 900 }, locale: "zh-CN", colorScheme: scheme, reducedMotion: "reduce" });
    page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
    await page.goto(`${BASE}?pref=zh&turns=2&peers=1&peerset=1&pending=1&ws=3`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");

    await page.waitForSelector(".peerbar");
    const held = await page.evaluate(() => {
      const bar = document.querySelector(".peerbar");
      const btn = [...bar.querySelectorAll("button")].find((b) => b.textContent.includes("记住项目"));
      const note = bar.querySelector(".apv-note");
      const r = bar.getBoundingClientRect();
      const spill = [...bar.querySelectorAll("button, .apv-note")].some((e) => e.getBoundingClientRect().right > r.right + 1);
      return { btn: btn?.textContent ?? "", note: note?.textContent ?? "", described: btn?.getAttribute("aria-describedby") === note?.id, spill, scroll: bar.scrollWidth - bar.clientWidth };
    });
    check(`${tag}: the held message offers to remember its project`, /记住项目「[^」]+」/.test(held.btn), held.btn);
    check(`${tag}: it says where the choice is kept and undone`, held.note.includes("设置 → 会话间消息") && held.note.includes("/"), held.note.slice(0, 40));
    check(`${tag}: the note is tied to the button`, held.described);
    check(`${tag}: nothing in the held bar spills sideways`, !held.spill && held.scroll <= 1, `${held.scroll}px`);
    if (shotDir && width === 1280) await page.locator(".compose").screenshot({ path: `${shotDir}/${scheme}-held.png` });

    await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
    const section = (value) => page.evaluate((v) => document.querySelector(`[data-action="settings.section"][data-value="${v}"]`)?.click(), value);
    await page.waitForSelector('[data-action="settings.section"][data-value="tools"]');
    await section("session");
    await page.waitForTimeout(300);
    check(`${tag}: session messages have a settings entry of their own`, (await page.locator('[data-action="settings.page-index"][data-target="peers"]').count()) === 1);
    await section("tools");
    await page.waitForTimeout(300);
    check(`${tag}: and no longer sit at the foot of tools and permissions`, (await page.locator('[role="switch"][aria-label="启用会话间消息"]').count()) === 0);
    await section("session");
    await page.locator('[data-action="settings.page-index"][data-target="peers"]').click();
    await page.waitForSelector('[role="switch"][aria-label="启用会话间消息"]', { timeout: 8000 });
    const s = await page.evaluate(() => {
      const sw = (n) => document.querySelector(`[role="switch"][aria-label="${n}"]`);
      const list = document.querySelector(".peerreleased");
      const box = sw("启用会话间消息").closest(".box");
      const r = box.getBoundingClientRect();
      return {
        enabled: sw("启用会话间消息").getAttribute("aria-checked"),
        dispatchOff: sw("允许模型把任务交给别的会话").disabled,
        released: [...(list?.querySelectorAll("li") ?? [])].map((li) => li.textContent),
        revoke: [...(list?.querySelectorAll("button") ?? [])].map((b) => b.getAttribute("aria-label")),
        pending: document.body.textContent.includes("下个回合起生效"),
        immediate: /立即生效/.test(box.textContent),
        path: box.textContent.includes("/Users/you/.reasonix/config.toml"),
        spill: box.scrollWidth - box.clientWidth,
        within: [...box.querySelectorAll("li, .lrow, .peerhead")].every((e) => e.getBoundingClientRect().right <= r.right + 1),
      };
    });
    check(`${tag}: the switch is on the page`, s.enabled === "true");
    check(`${tag}: the hand-off switch is live while messages are on`, s.dispatchOff === false);
    check(`${tag}: released projects are listed with a revoke each`, s.released.length === 2 && s.revoke.length === 2 && s.revoke.every((l) => l.startsWith("撤销放行")), s.revoke.join("|"));
    check(`${tag}: a saved switch says the next turn, never at once`, s.pending && !s.immediate);
    check(`${tag}: the file the switches live in is named`, s.path);
    check(`${tag}: nothing spills out of the box`, s.within && s.spill <= 1, `${s.spill}px`);
    const pick = page.getByRole("combobox", { name: "放行这个项目" });
    const options = await pick.locator("option").count();
    check(`${tag}: a project can be chosen from those already added`, options >= 1, `${options} choices`);
    await page.getByRole("button", { name: "放行", exact: true }).click();
    await page.waitForFunction(() => document.querySelectorAll(".peerreleased li").length === 3);
    check(`${tag}: allowing it through adds it to the list`, true);
    if (shotDir && width === 1280) {
      await page.addStyleTag({ content: ".cfgbad { display: none !important; }" });
      await page.setViewportSize({ width, height: 1100 });
      await page.waitForTimeout(250);
      await page.locator("#set-peers").screenshot({ path: `${shotDir}/${scheme}-settings.png` });
      await page.locator("#set-peer-projects").screenshot({ path: `${shotDir}/${scheme}-projects.png` });
    }
    await page.close();
  }
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
