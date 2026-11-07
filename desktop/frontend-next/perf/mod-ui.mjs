// Claude Code mods in the window: the band a mod draws above the prompt, a question a mod asks, the settings
// block (switch, time limit, each mod's state by code) and the install preview's full-trust listing.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const shotDir = process.env.SHOTS;
const SHOWN = process.env.MODS_SHOWN === "1";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const band = { kind: "extension_surface", extension: { pluginId: "statusline-clock", surfaceId: "mod-above-prompt", kind: "view", view: { slot: "above_prompt", body: [
  { kind: "row", children: [{ kind: "pip", tone: "ok" }, { kind: "text", value: "statusline-clock", tone: "strong" }, { kind: "text", value: "12:30 · 今天还剩 3 个任务", tone: "dim" }, { kind: "button", label: "稍后提醒", actionId: "snooze:5" }] },
] } } };
const ask = { kind: "extension_surface", extension: { pluginId: "git-guard", surfaceId: "mod-ask", kind: "form", sessionId: "s", generation: 1, form: { title: "git-guard asks", message: "要删除已经合并的分支 old-spike 吗？", fields: [{ key: "answer", label: "回答", kind: "input" }] } } };

// The pane attaches to its stream a moment after the page is up, and an event fed earlier is lost; feed again until it lands.
async function feedUntil(page, ev, seen) {
  for (let i = 0; i < 20; i++) {
    await page.evaluate((e) => window.__feed(e), ev);
    if (await seen().then(() => true, () => false)) return;
  }
  await seen();
}

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  for (const width of [1280, 420]) {
    const tag = `${scheme} ${width}px`;
    const page = await browser.newPage({ viewport: { width, height: 1300 }, locale: "zh-CN", colorScheme: scheme, reducedMotion: "reduce" });
    page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
    await page.goto(`${BASE}?pref=zh&turns=2`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");

    await feedUntil(page, band, () => page.getByRole("button", { name: "稍后提醒" }).waitFor({ timeout: 500 }));
    const at = await page.evaluate(() => {
      const s = document.querySelector(".slotted").getBoundingClientRect();
      const c = document.querySelector('[aria-label="任务输入"]').getBoundingClientRect();
      return { above: s.bottom <= c.top + 4, spill: s.right > innerWidth + 1 || document.documentElement.scrollWidth > innerWidth + 1 };
    });
    check(`${tag}: the band stands above the text the person types in`, at.above);
    check(`${tag}: the band does not push the page sideways`, !at.spill);
    await page.getByRole("button", { name: "稍后提醒" }).focus();
    check(`${tag}: its button can be reached from the keyboard`, await page.evaluate(() => document.activeElement?.textContent === "稍后提醒"));
    const ring = await page.evaluate(() => {
      const b = document.activeElement, rail = document.querySelector(".slotrail");
      const cs = getComputedStyle(b);
      const room = Math.min(b.getBoundingClientRect().left - rail.getBoundingClientRect().left, rail.getBoundingClientRect().right - b.getBoundingClientRect().right);
      return { ring: parseFloat(cs.outlineWidth) + Math.max(0, parseFloat(cs.outlineOffset)), room };
    });
    check(`${tag}: the focus ring of the band's button is not cut off by the strip it sits in`, ring.room >= ring.ring - 0.5, `${ring.room}px room for a ${ring.ring}px ring`);
    await page.evaluate(() => document.activeElement?.blur());
    if (shotDir) { await page.mouse.move(width - 5, 895); await page.waitForTimeout(300); await page.screenshot({ path: `${shotDir}/${scheme}-${width}-band.png`, clip: { x: 0, y: 760, width, height: 540 } }); }

    await feedUntil(page, ask, () => page.waitForSelector('.call[data-k="extension"] .extform', { timeout: 500 }));
    const card = await page.evaluate(() => {
      const form = document.querySelector(".extform");
      return { note: form.querySelector(".why")?.textContent ?? "", tag: document.querySelector('.call[data-k="extension"] .tag')?.textContent ?? "" };
    });
    check(`${tag}: a mod's question says whose it is and that answering approves nothing`, card.note.includes("git-guard") && card.note.includes("不会批准任何工具调用或权限") && card.tag.includes("git-guard"), card.note);
    if (shotDir && width === 1280) await page.locator('.call[data-k="extension"]').last().screenshot({ path: `${shotDir}/${scheme}-ask.png` });

    await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
    await page.waitForSelector(".prefs-nav button");
    await page.evaluate(() => [...document.querySelectorAll(".prefs-nav button")].find((b) => b.textContent.trim().startsWith("扩展"))?.click());
    await page.waitForSelector('[data-action="extensions.add"]');
    if (!SHOWN) {
      check(`${tag}: the mod settings block is not drawn while the host is hidden`, (await page.locator("#set-mods").count()) === 0);
      await page.locator('input[aria-label="搜索设置"]').fill("Claude Code mod");
      await page.waitForTimeout(400);
      const found = await page.evaluate(() => [...document.querySelectorAll(".prefs-nav *, .prefs-results *")].some((e) => e.children.length === 0 && /Claude Code mod/.test(e.textContent)));
      check(`${tag}: settings search does not lead to it either`, !found);
      await page.locator('input[aria-label="搜索设置"]').fill("");
    } else {
      await page.waitForSelector("#set-mods");
      const off = await page.evaluate(() => {
        const g = document.getElementById("set-mods");
        return { checked: g.querySelector('[role="switch"]').getAttribute("aria-checked"), rows: g.querySelectorAll(".modlist li").length, why: [...g.querySelectorAll(".modlist .ds")].filter((e) => e.textContent.includes("没有打开")).length };
      });
      check(`${tag}: the switch ships off and every mod says why it is not running`, off.checked === "false" && off.rows === 4 && off.why === 4, `${off.rows} mods, ${off.why} reasons`);
      await page.locator('#set-mods [role="switch"]').click();
      await page.waitForFunction(() => document.querySelectorAll("#set-mods .modlist li[data-state='running']").length === 1);
      const on = await page.evaluate(() => {
        const g = document.getElementById("set-mods");
        const r = g.getBoundingClientRect();
        const states = [...g.querySelectorAll(".modlist li")].map((li) => li.getAttribute("data-state"));
        return { states, spill: r.right > innerWidth + 1 || g.scrollWidth > g.clientWidth + 1, marks: [...g.querySelectorAll(".modstate")].every((e) => /^\S+\s+\S+/.test(e.textContent.trim())) };
      });
      check(`${tag}: each state shows running, disabled, restarting or not running, with a mark as well as a word`, on.states.join() === "running,disabled,restarting,inactive" && on.marks, on.states.join());
      check(`${tag}: the block does not spill sideways`, !on.spill);
      if (shotDir) await page.locator("#set-mods").screenshot({ path: `${shotDir}/${scheme}-${width}-settings.png` });
    }

    if (width === 1280) {
      await page.locator('.ext-add-btn[data-action="extensions.add"]').first().click();
      await page.locator('[role="menuitem"][data-value="source"]').click();
      const source = page.locator("textarea, input").filter({ hasNot: page.locator("[aria-label]") }).last();
      await page.locator('textarea[placeholder*="github.com"], input[placeholder*="github.com"]').first().fill("https://github.com/acme/claude-code mod git-guard");
      await page.locator('[data-action="extensions.inspect"]').first().click();
      await page.waitForSelector(".modpreview", { timeout: 10000 });
      const prev = await page.evaluate(() => {
        const m = document.querySelector(".modpreview");
        const text = m.textContent;
        return { trust: text.includes("完全信任"), static: text.includes("不是保证"), imports: text.includes("看不见"), unserved: text.includes("本应用不会触发的事件"), hash: !!m.querySelector("[title]") };
      });
      check(`${tag}: the install preview states full trust, that it is a static listing, hidden imports and unserved events`, prev.trust && prev.static && prev.imports && prev.unserved && prev.hash, JSON.stringify(prev));
      if (shotDir) await page.locator(".cand").first().screenshot({ path: `${shotDir}/${scheme}-preview.png` });
    }
    await page.close();
  }
}
await browser.close();
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
console.log("\nall passed");
