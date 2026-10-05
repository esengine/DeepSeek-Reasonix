// 对话内查找：快捷键打开、计数、跳到没挂载的那一条、当前处真的落在眼前。
//
// 记录只挂几十张卡，浏览器自带的查找看不见屏幕外的消息；这里要证的是应用内
// 的查找能把一条从未挂载过的消息带到眼前，并且高亮落在读者看得见的地方 ——
// jsdom 没有布局，这几条只能在真浏览器里问。
import { chromium } from "playwright";
import { fileURLToPath } from "node:url";
import { mkdirSync } from "node:fs";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = process.env.SHOTS ?? fileURLToPath(new URL("shots", import.meta.url));
mkdirSync(SHOTS, { recursive: true });

const browser = await chromium.launch();
const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
page.on("console", (m) => m.type() === "error" && fails.push("控制台错误: " + m.text()));

// 当前处那一段高亮，量它在滚动容器里的位置；没有就说没有。
const current = () =>
  page.evaluate(() => {
    const now = CSS.highlights.get("rx-find-now");
    const range = now && [...now][0];
    const flow = document.querySelector('[data-pane="flow"]');
    if (!range || !flow) return null;
    const box = range.getBoundingClientRect();
    const view = flow.getBoundingClientRect();
    // 落在视口里还不够：长输出在自己的框里滚，框外的那一截同样看不见。
    const host = range.startContainer.parentElement;
    const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
    const seen = !!hit && (hit === host || host.contains(hit));
    return { text: range.toString(), top: box.top, bottom: box.bottom, h: box.height, viewTop: view.top, viewBottom: view.bottom, seen };
  });
const inView = (c) => !!c && c.h > 0 && c.seen && c.top >= c.viewTop + 40 && c.bottom <= c.viewBottom;
// 落地要等块挂上、位置稳住，给它一段时间，而不是赌一个固定的等待。
async function settled() {
  let c = null;
  for (let i = 0; i < 30; i++) {
    await page.waitForTimeout(50);
    c = await current();
    if (inView(c)) return c;
  }
  return c;
}

async function suite(scheme) {
  await page.emulateMedia({ colorScheme: scheme });
  await page.goto(`${PAGE}?turns=200`, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 15000 });
  await page.waitForTimeout(1200);
  await page.evaluate((s) => document.documentElement.setAttribute("data-theme", s), scheme);
  const tag = scheme;

  const mountedFirst = await page.locator('[data-item] :text("第 3 个问题")').count();
  check(`${tag} 第 3 问原本没挂载`, mountedFirst === 0);

  // 焦点在输入框里：它没有自己的查找，快捷键要能穿过它。
  await page.locator("textarea").first().focus();
  await page.keyboard.press("ControlOrMeta+f");
  await page.waitForTimeout(250);
  const box = page.locator(".tfind input");
  check(`${tag} 快捷键从输入框里打开查找条`, (await box.count()) === 1);
  check(`${tag} 焦点在查找框`, await box.evaluate((el) => el === document.activeElement).catch(() => false));
  await page.screenshot({ path: `${SHOTS}/${tag}-1-打开查找条.png` });

  await box.pressSequentially("第 3 个问题", { delay: 20 });
  const c1 = await settled();
  const n = await page.locator(".tfind .n").innerText();
  check(`${tag} 计数`, n === "1 / 1", n);
  check(`${tag} 屏幕外那条被带到眼前`, inView(c1), JSON.stringify(c1));
  check(`${tag} 高亮的是查的字`, c1?.text === "第 3 个问题", c1?.text);
  await page.screenshot({ path: `${SHOTS}/${tag}-2-跳到屏幕外的匹配.png` });

  // 一条工具输出里有二十处：每按一次 Enter，当前处要往下走，而且留在眼前。
  await box.fill("第 120 次输出");
  await page.waitForTimeout(700);
  const total = await page.locator(".tfind .n").innerText();
  check(`${tag} 同一行里的多处都算进去`, total === "1 / 20", total);
  const tops = [];
  for (let i = 0; i < 20; i++) {
    if (i > 0) await box.press("Enter");
    const c = await settled();
    check(`${tag} 第 ${i + 1} 处在眼前`, inView(c), JSON.stringify(c));
    tops.push(c?.top ?? NaN);
  }
  const moved = new Set(tops.map((t) => Math.round(t))).size;
  check(`${tag} 当前处随步进移动`, moved > 1, `${moved} 个不同位置`);
  await page.screenshot({ path: `${SHOTS}/${tag}-3-同一行里的第20处.png` });

  await box.press("Shift+Enter");
  await page.waitForTimeout(400);
  check(`${tag} Shift+Enter 往回`, (await page.locator(".tfind .n").innerText()) === "19 / 20");
  await box.press("ArrowUp");
  await page.waitForTimeout(400);
  check(`${tag} 上箭头往回`, (await page.locator(".tfind .n").innerText()) === "18 / 20");

  await box.press("Escape");
  await page.waitForTimeout(200);
  check(`${tag} Esc 关掉`, (await page.locator(".tfind").count()) === 0);
  check(`${tag} 关掉后高亮清空`, await page.evaluate(() => !CSS.highlights.has("rx-find") && !CSS.highlights.has("rx-find-now")));
}

for (const scheme of ["light", "dark"]) await suite(scheme);

// 窄屏：没有键盘，入口得是看得见的按钮。
await page.setViewportSize({ width: 390, height: 844 });
await page.emulateMedia({ colorScheme: "light" });
await page.goto(`${PAGE}?turns=200`, { waitUntil: "networkidle" });
await page.waitForSelector(".app", { timeout: 15000 });
await page.waitForTimeout(1200);
const button = page.locator(".chrome .find-action");
check("窄屏 顶栏有查找按钮", await button.isVisible().catch(() => false));
if (await button.isVisible().catch(() => false)) {
  await button.click();
  await page.waitForTimeout(250);
  await page.locator(".tfind input").pressSequentially("第 5 个问题", { delay: 20 });
  check("窄屏 按钮打开的查找条能跳过去", inView(await settled()));
}
await page.screenshot({ path: `${SHOTS}/narrow-查找.png` });

await browser.close();
console.log(fails.length ? `\n失败 ${fails.length} 条:\n  ${fails.join("\n  ")}` : "\n全部通过");
process.exit(fails.length ? 1 : 0);
