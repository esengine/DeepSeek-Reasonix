// 待送达那一条「改」：正文读不回来时，编辑器不许打开。
//
// 空字符串是一个真实的正文状态,不是「我不知道」。把读取失败当成空正文,用户
// 会在空框里重打一句他从没看见过的话,而保存是整条替换 —— 这一条守的是那个。
//
// ?queue= 播出来的行,固件里没有对应的正文,读回去必然被拒。这正是屏幕上真会
// 出现的那个状态:行还在,而它的条目内核已经不认了。
//
// 这条守卫不覆盖「读得回来时编辑器打得开」:台架的 BenchPort 自带订阅,固件内部
// 的 inbox_changed 到不了界面,所以走 steer 真排一条队的路在这里不通。缺的是台
// 架的一条边,不是判据 —— 记在这里,而不是让它看起来被测过了。
import { chromium } from "playwright";
import { fileURLToPath } from "node:url";
import { mkdirSync } from "node:fs";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = fileURLToPath(new URL("shots", import.meta.url));
mkdirSync(SHOTS, { recursive: true });

const browser = await chromium.launch();
// 判据比的是界面上那几个词,所以语种得钉住 —— verify.mjs 早就是这么开页面的。
const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
page.on("console", (m) => m.type() === "error" && fails.push("控制台错误: " + m.text()));

await page.goto(`${PAGE}?queue=3`, { waitUntil: "networkidle" });
await page.waitForSelector(".app", { timeout: 15000 });
await page.waitForTimeout(800);

const rows = page.locator(".queue .qi");
check("播出来的行在", (await rows.count()) === 3, `${await rows.count()} 行`);

const first = rows.nth(0);
const wasFirst = await first.locator(".pv").innerText();
await first.locator('button:has-text("改")').click();
await page.waitForTimeout(500);

const box = await page.locator(".queue .qedit").count();
check("读不回就不打开编辑器", box === 0, box ? "开了一个空框" : "");
check("原来那一行原样留着", (await first.locator(".pv").innerText()) === wasFirst);
const why = first.locator(".qwhy[data-err]");
check("这一行说了为什么", await why.isVisible().catch(() => false), (await why.innerText().catch(() => "")).slice(0, 60));

// 说明是这一行的,不是面板的:换一行再点,上一行的说明得跟着走。一条粘住不动的
// 提示会让下一次失败看起来像上一次的余音。
const second = rows.nth(1);
await second.locator('button:has-text("改")').click();
await page.waitForTimeout(500);
check("说明跟着被点的那一行走", (await first.locator(".qwhy[data-err]").count()) === 0);
check("新的那一行才带着说明", await second.locator(".qwhy[data-err]").isVisible().catch(() => false));
check("换了一行也还是没开编辑器", (await page.locator(".queue .qedit").count()) === 0);

await page.screenshot({ path: `${SHOTS}/待送达-读不回.png` });
console.log(fails.length ? `\n失败 ${fails.length} 条` : "\n全部通过");
await browser.close();
process.exit(fails.length ? 1 : 0);
