import { chromium } from "playwright";
import { fileURLToPath } from "node:url";
import { mkdirSync, readFileSync, readdirSync } from "node:fs";
import { join, dirname } from "node:path";
const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = fileURLToPath(new URL("shots", import.meta.url));
// PERF_SRC lets the script run from outside the repo (a scratch copy) and
// still read the fixtures it must exempt.
const SRC = process.env.PERF_SRC ?? join(dirname(fileURLToPath(import.meta.url)), "..", "src");
mkdirSync(SHOTS, { recursive: true });
const fails = [];
const check = (n, ok, d = "") => { console.log(`${ok ? "  ok" : "FAIL"}  ${n}${d ? "  — " + d : ""}`); if (!ok) fails.push(n); };

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));

// 先按英文启动
await page.addInitScript(() => localStorage.setItem("rx-lang", "en"));
await page.goto(`${PAGE}?pref=en`, { waitUntil: "networkidle" });
await page.waitForSelector(".app", { timeout: 20000 });
await page.waitForTimeout(800);

check("<html lang> 是 en", (await page.evaluate(() => document.documentElement.lang)) === "en");

await page.evaluate(async () => {
  const y = () => new Promise((r) => requestAnimationFrame(r));
  window.__feed({ kind: "turn_started" });
  window.__feed({ kind: "tool_dispatch", tool: { id: "t1", name: "edit_file", args: '{"path":"pkg/a.go"}' } });
  window.__feed({ kind: "tool_result", tool: { id: "t1", name: "edit_file", args: '{"path":"pkg/a.go"}', output: "done", durationMs: 210, added: 4, removed: 2 } });
  window.__feed({ kind: "text", text: "A short answer.\n\n" });
  window.__feed({ kind: "message" });
  await y();
});
await page.waitForTimeout(500);

const seen = (s) => page.locator(`text=${s}`).first().isVisible().catch(() => false);
// 固件替用户造的数据不该翻：技能描述、钩子名、记忆条目、代理地址、会话标题，
// 真机上都是用户自己写的内容。语言名同理：英文界面里「中文」就该是「中文」。
// 豁免表连内容带清单都从固件目录读出来 —— 曾经只有内容是读的、文件名是手写的
// 五个，固件后来拆成十二个 mock_*.ts，没列到的那几个造的数据就全被当成漏译。
const fixtureSrc = readdirSync(join(SRC, "port"))
  .filter((f) => f === "fixture.ts" || /^mock(_[a-z]+)?\.ts$/.test(f))
  .map((f) => { try { return readFileSync(join(SRC, "port", f), "utf8"); } catch { return ""; } })
  .join("\n");
const FIXTURE_TEXT = new Set(
  [...fixtureSrc.matchAll(/(["'`])((?:[^"'`\\]|\\.)*?)\1/g)].map((m) => m[2]).filter((x) => /[一-鿿]/.test(x)),
);
const isFixture = (s) => s === "中文" || [...FIXTURE_TEXT].some((f) => f.includes(s) || s.includes(f));
// 判据认的是右栏各块自己的标题。原来认的三个（Metrics / Pending changes /
// Agents and tools）在 src/ui 里已经一处都不剩，只活在词表里 —— 面板改过名，
// 而判据没跟，于是这几条对着不存在的字红了很久。
check("侧栏译成英文", await seen("Prefix cache"));
check("标签页译成英文", await seen("Activity"));
check("会话树译成英文", await seen("Workspaces"));
check("改动面板译成英文", await seen("Worktree changes"));
check("外部服务面板译成英文", await seen("External services"));
await page.screenshot({ path: `${SHOTS}/lang-en.png` });

// 设置页：逐个分区看有没有漏译
await page.keyboard.press("Meta+Comma");
await page.waitForTimeout(500);
// 分区从导航栏自己数出来，不手写：手写的那份停在十个，而设置页早就有十三个 ——
// 远程、用量、存储三整块面板的译文从来没有被看过一眼。
const sections = await page.evaluate(() =>
  [...document.querySelectorAll('.prefs-nav [id^="prefs-"]')].map((e) => e.id.slice("prefs-".length)),
);
// 数不出分区同样是坏了：清单空着，下面那个循环一次都不跑，而断言照样成立。
if (sections.length < 8) fails.push(`只数出 ${sections.length} 个设置分区，导航栏没读到`);
const leftover = {};
for (const sec of sections) {
  await page.evaluate((id) => document.getElementById(`prefs-${id}`)?.click(), sec);
  await page.waitForTimeout(320);
  const zh = await page.evaluate(() => {
    const cjk = /[一-鿿]/;
    const out = [];
    const walk = (n) => {
      if (n.nodeType === 3) { const s = n.textContent.trim(); if (cjk.test(s)) out.push(s); return; }
      if (n.nodeType !== 1 || n.tagName === "SCRIPT") return;
      for (const c of n.childNodes) walk(c);
    };
    for (const sel of [".prefs-main", ".prefs-nav"]) {
      const el = document.querySelector(sel);
      if (el) walk(el);
    }
    return [...new Set(out)];
  });
  // 固件造的「用户数据」不该翻：技能与钩子的名字、记忆条目、代理地址、
  // 会话标题，真机上都是用户自己写的。语言名同理 —— 英文界面里「中文」
  // 就该是「中文」。
  const own = zh.filter((x) => !isFixture(x));
  if (own.length) leftover[sec] = own.map((x) => x.slice(0, 34));
}
await page.screenshot({ path: `${SHOTS}/lang-en-settings.png` });
const secBad = Object.keys(leftover).length;
// 数目由扫到的分区数说，不写死：写死的那个「10」和写死的分区清单互相印证，
// 于是清单少了三个的时候，读数看着仍然是自洽的。
check(
  "设置各分区已译",
  secBad === 0,
  secBad
    ? Object.entries(leftover).map(([k, v]) => `${k}: ${v.slice(0, 3).join("/")}`).join("  |  ")
    : `${sections.length} 个分区都干净`,
);
await page.keyboard.press("Escape");
await page.waitForTimeout(300);

// 界面里不该再有中文（fixture 造的会话标题除外）
const stray = await page.evaluate(() => {
  const cjk = /[一-鿿]/;
  const skip = new Set(["SCRIPT", "STYLE"]);
  const out = [];
  const walk = (n) => {
    if (n.nodeType === 3) { const s = n.textContent.trim(); if (cjk.test(s)) out.push(s); return; }
    if (n.nodeType !== 1 || skip.has(n.tagName)) return;
    for (const c of n.childNodes) walk(c);
  };
  walk(document.querySelector(".app"));
  return [...new Set(out)];
});
const strayOwn = stray.filter((x) => !isFixture(x));
check("主界面无残留中文", strayOwn.length === 0, strayOwn.length ? strayOwn.slice(0, 6).map((x) => x.slice(0, 30)).join(" / ") : `${stray.length} 处全是固件数据`);

// 切回中文：另开一页，否则上面那条 initScript 每次导航都会把语言写回 en
const zh = await browser.newPage({ viewport: { width: 1440, height: 900 } });
await zh.addInitScript(() => localStorage.setItem("rx-lang", "zh"));
await zh.goto(`${PAGE}?pref=zh`, { waitUntil: "networkidle" });
await zh.waitForSelector(".app", { timeout: 20000 });
await zh.waitForTimeout(700);
check("切回中文", await zh.locator("text=前缀缓存").first().isVisible().catch(() => false));

// 上下文构成那五行是这条守卫最初红在的地方：表在模块作用域里就把 t() 求了值，
// 而模块体先于 boot() 跑，于是标签把源语言冻住了，界面换了它们不换。翻的是标签，
// 不是数据 —— 所以两边都读，一边要求全变，一边要求逐位不变。
const legend = (pg) =>
  pg.evaluate(() =>
    [...document.querySelectorAll('[data-b="ctx"] .ctxlg .r')].map((r) => ({
      label: r.querySelector(".t")?.textContent?.trim() ?? "",
      value: r.querySelector("b")?.textContent?.trim() ?? "",
    })),
  );
const enParts = await legend(page);
const zhParts = await legend(zh);
check("上下文构成读得到", enParts.length > 0 && enParts.length === zhParts.length,
  `en ${enParts.length} 行 / zh ${zhParts.length} 行`);
const han = /[一-鿿]/;
check("英文窗口里这几行没有中文", enParts.every((p) => !han.test(p.label)),
  enParts.map((p) => p.label).join(" / "));
check("中文窗口里这几行是中文", zhParts.every((p) => han.test(p.label)),
  zhParts.map((p) => p.label).join(" / "));
check("换语言换的是标签，不是数据",
  enParts.every((p, i) => p.value === zhParts[i]?.value),
  enParts.map((p, i) => `${p.value}|${zhParts[i]?.value}`).join(" "));
check("<html lang> 回到 zh-CN", (await zh.evaluate(() => document.documentElement.lang)) === "zh-CN");
await zh.screenshot({ path: `${SHOTS}/lang-zh.png` });

await browser.close();
console.log(fails.length ? `\n失败 ${fails.length} 项：\n- ${fails.join("\n- ")}` : "\n全部通过");
// 退出码是这个脚本唯一被机器读的部分。少了这一行，它印着「失败 3 项」而
// 退出 0 —— 任何按返回码判断的地方都会当它通过。
process.exit(fails.length ? 1 : 0);
