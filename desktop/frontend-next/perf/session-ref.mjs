// A past conversation is referenced from the message box: "#" lists this
// workspace's conversations, a pick becomes a removable chip, a search hit in
// the rail can be referenced too, and the size limit is a real control.
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
    await page.goto(`${BASE}?pref=zh&turns=2&search=1`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");

    const box = page.getByRole("combobox", { name: "任务输入" });
    await box.click();
    await box.pressSequentially("接着上次的讨论 #");
    await page.waitForSelector(".slashmenu .mi");
    const menu = await page.evaluate(() => {
      const m = document.querySelector(".slashmenu");
      const r = m.getBoundingClientRect();
      const rows = [...m.querySelectorAll(".mi")];
      return {
        rows: rows.length,
        kinds: [...new Set(rows.map((x) => x.querySelector(".rt")?.textContent))],
        spill: r.left < -1 || r.right > innerWidth + 1,
        hints: rows.every((x) => !!x.querySelector(".ds")),
      };
    });
    check(`${tag}: # lists past conversations marked as conversations`, menu.rows >= 2 && menu.kinds.length === 1 && menu.kinds[0] === "会话", `${menu.rows} rows ${menu.kinds}`);
    check(`${tag}: each row says when it was`, menu.hints);
    check(`${tag}: the menu stays on screen`, !menu.spill);
    if (shotDir && width === 1280) {
      const area = await page.evaluate(() => {
        const a = document.querySelector(".compose").getBoundingClientRect(), m = document.querySelector(".slashmenu").getBoundingClientRect();
        const x = Math.min(a.left, m.left), y = Math.min(a.top, m.top);
        return { x, y, width: Math.max(a.right, m.right) - x, height: Math.max(a.bottom, m.bottom) - y };
      });
      await page.screenshot({ path: `${shotDir}/${scheme}-menu.png`, clip: area });
    }

    await page.keyboard.press("Enter");
    await page.waitForSelector(".shots .shot");
    const chip = await page.evaluate(() => {
      const c = document.querySelector(".shots .shot");
      const ta = document.querySelector('[aria-label="任务输入"]');
      const r = c.getBoundingClientRect();
      return { title: c.querySelector(".nm")?.textContent, sub: c.querySelector(".sz")?.textContent, text: ta.value, spill: r.right > innerWidth + 1, remove: !!c.querySelector(".x") };
    });
    check(`${tag}: picking one makes a chip titled with it and takes the # word out of the box`, !!chip.title && chip.text === "接着上次的讨论 " && chip.sub === "历史会话 · 只读资料", `${chip.title} | "${chip.text}"`);
    check(`${tag}: the chip can be removed and does not spill`, chip.remove && !chip.spill);
    if (shotDir && width === 1280) await page.locator(".compose").screenshot({ path: `${shotDir}/${scheme}-chip.png` });

    if (width === 1280) {
      await page.getByRole("searchbox").fill("轮换");
      await page.waitForSelector(".content-matches li", { timeout: 8000 });
      const hit = await page.evaluate(() => {
        const li = document.querySelector(".content-matches li");
        const [open, ref] = li.querySelectorAll("button");
        const o = open.getBoundingClientRect(), r = ref.getBoundingClientRect();
        return { has: !!ref, size: Math.round(r.width) + "x" + Math.round(r.height), label: ref?.getAttribute("aria-label") ?? "", beside: r.left >= o.right - 1 && Math.abs(r.top - o.top) < 2, tall: r.height >= 28 };
      });
      check(`${tag}: a search hit offers to reference it, beside the open button`, hit.has && hit.beside && /引用「.+」到输入框/.test(hit.label), `${hit.size} ${hit.label}`);
      check(`${tag}: that button is at least 28px tall`, hit.tall, hit.size);
      if (shotDir) await page.locator(".content-matches").screenshot({ path: `${shotDir}/${scheme}-hit.png` });
      const refButton = page.locator(".content-matches li button[data-action='session.reference-match']").first();
      await refButton.focus();
      await page.waitForTimeout(200);
      const refBox = await refButton.boundingBox();
      await page.mouse.click(refBox.x + refBox.width / 2, refBox.y + refBox.height / 2);
      await page.waitForFunction(() => document.querySelectorAll(".shots .shot").length === 2);
      check(`${tag}: referencing a hit adds a second chip to the message box`, true);
    }

    await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
    await page.waitForSelector(".prefs-nav button");
    await page.evaluate(() => [...document.querySelectorAll(".prefs-nav button")].find((b) => b.textContent.trim().startsWith("会话"))?.click());
    await page.waitForSelector("#set-session-ref", { timeout: 8000 });
    const s = await page.evaluate(() => {
      const g = document.getElementById("set-session-ref");
      const input = g.querySelector("input");
      const r = g.getBoundingClientRect();
      return { value: input?.value, unit: g.querySelector("i")?.textContent, restore: g.querySelector('[data-action="session-ref.reset"]')?.disabled, spill: r.right > innerWidth + 1 || g.scrollWidth > g.clientWidth + 1 };
    });
    check(`${tag}: the size limit is a number field at its default, with nothing to restore`, s.value === "8000" && ["token", "令牌"].includes(s.unit) && s.restore === true, `${s.value} ${s.unit}`);
    check(`${tag}: the setting does not spill sideways`, !s.spill);
    if (shotDir) await page.locator("#set-session-ref").screenshot({ path: `${shotDir}/${scheme}-${width}-setting.png` });
    await page.close();
  }
}
await browser.close();
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
console.log("\nall passed");
