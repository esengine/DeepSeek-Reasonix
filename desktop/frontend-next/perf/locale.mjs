// 没选过语言时，界面跟机器走：中文的各种写法都是中文，其余一律英文。
// 浏览器的 locale 是真设进去的，不是往 navigator 上打补丁。
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";

// [系统语言, 期望, 说明]
const CASES = [
  ["zh-CN", "zh", "简体·中国大陆"],
  ["zh-TW", "zh", "繁体·台湾"],
  ["zh-HK", "zh", "繁体·香港"],
  ["zh-Hans", "zh", "简体·未写地区"],
  ["zh-Hant", "zh", "繁体·未写地区"],
  ["zh-SG", "zh", "简体·新加坡"],
  ["zh", "zh", "只写 zh"],
  ["en-US", "en", "英语·美国"],
  ["en-GB", "en", "英语·英国"],
  ["ja-JP", "en", "日语 → 英语"],
  ["ko-KR", "en", "韩语 → 英语"],
  ["fr-FR", "en", "法语 → 英语"],
  ["de-DE", "en", "德语 → 英语"],
  ["ru-RU", "en", "俄语 → 英语"],
  ["ar-SA", "en", "阿拉伯语 → 英语"],
  ["pt-BR", "en", "葡萄牙语 → 英语"],
];

const fails = [];
const browser = await chromium.launch();

for (const [locale, want, note] of CASES) {
  const ctx = await browser.newContext({ locale, viewport: { width: 1200, height: 800 } });
  const page = await ctx.newPage();
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 20000 });
  await page.waitForTimeout(350);
  const got = await page.evaluate(() => document.documentElement.lang);
  const lang = got.startsWith("zh") ? "zh" : "en";
  const ok = lang === want;
  console.log(`${ok ? "  ok" : "FAIL"}  ${locale.padEnd(9)} → ${lang}   ${note}`);
  if (!ok) fails.push(`${locale} 应为 ${want}，实为 ${lang}`);
  await ctx.close();
}

// 明确选过的语言压过机器：一台中文机器上选了英文，就该是英文。
// 两侧摆一致（?pref= 是内核记着的那份），否则 adopt 会判定本地缓存过期。
for (const [locale, pick, want] of [
  ["zh-CN", "en", "en"],
  ["en-US", "zh", "zh"],
  ["ja-JP", "zh", "zh"],
  ["zh-TW", "", "zh"],
  ["fr-FR", "", "en"],
]) {
  const ctx = await browser.newContext({ locale, viewport: { width: 1200, height: 800 } });
  const page = await ctx.newPage();
  await page.addInitScript((p) => localStorage.setItem("rx-lang", p), pick);
  await page.goto(`${PAGE}?pref=${pick}`, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 20000 });
  await page.waitForTimeout(400);
  const got = (await page.evaluate(() => document.documentElement.lang)).startsWith("zh") ? "zh" : "en";
  const ok = got === want;
  const label = pick === "" ? "跟随系统" : `选了 ${pick}`;
  console.log(`${ok ? "  ok" : "FAIL"}  ${locale.padEnd(9)} + ${label.padEnd(10)} → ${got}`);
  if (!ok) fails.push(`${locale}+${pick || "auto"} 应为 ${want}，实为 ${got}`);
  await ctx.close();
}

// 内核记着的语言压过本地缓存，而且只重载一次就停 —— 换机器、清缓存都走这条路。
{
  const ctx = await browser.newContext({ locale: "ja-JP", viewport: { width: 1200, height: 800 } });
  const page = await ctx.newPage();
  let loads = 0;
  page.on("load", () => loads++);
  // 本地缓存说英文，内核记的是中文：应当采纳内核的，并且收敛。
  await page.addInitScript(() => {
    if (!sessionStorage.getItem("seeded")) {
      sessionStorage.setItem("seeded", "1");
      localStorage.setItem("rx-lang", "en");
    }
  });
  await page.goto(`${PAGE}?pref=zh`, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 20000 });
  await page.waitForTimeout(1200);
  const got = (await page.evaluate(() => document.documentElement.lang)).startsWith("zh") ? "zh" : "en";
  const ok = got === "zh" && loads <= 2;
  console.log(`${ok ? "  ok" : "FAIL"}  缓存过期时采纳内核的语言 → ${got}，加载 ${loads} 次`);
  if (!ok) fails.push(`采纳内核语言：得到 ${got}，加载 ${loads} 次`);
  await ctx.close();
}

await browser.close();
console.log(fails.length ? `\n失败 ${fails.length} 项：\n- ${fails.join("\n- ")}` : "\n全部通过");
process.exit(fails.length ? 1 : 0);
