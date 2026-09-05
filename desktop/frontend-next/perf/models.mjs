// 连接面板的模型列表：网关报出一百多个名字，这一屏还得能用。
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const PANEL = ".addp[data-edit]";

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
// 判据里拿中文比 DOM，所以语种要钉住 —— 不钉就跟着运行环境走，在英文机器
// 上红的是台架没说自己要哪一种语言。locale.mjs 一直是这么开页面的。
const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1400, height: 950 } });
const page = await ctx.newPage();
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));

// 等不到也是这条守卫的结论，不是它退出的方式。抛出去的话退出码归 node，报的是
// 一段栈 —— 而「这一屏不再长这样」正是它该说清楚的那一件事。
const appear = async (sel, ms, what) => {
  try {
    await page.waitForSelector(sel, { timeout: ms });
    return true;
  } catch {
    check(`${what}还在这条守卫认识的位置`, false, `等不到 ${sel}（${ms}ms）`);
    return false;
  }
};

const giveUp = async () => {
  await browser.close();
  console.log(`\n${fails.length} 项不合格:\n  ` + fails.join("\n  ") + "\n  这一屏的结构变了，先更新判据再谈通过");
  process.exit(1);
};

await page.goto(PAGE, { waitUntil: "networkidle" });
if (!(await appear(".app", 20000, "工作台"))) await giveUp();

await page.keyboard.press("Meta+Comma");
await page.waitForTimeout(500);
await page.evaluate(() => document.getElementById("prefs-model")?.click());
await page.waitForTimeout(600);

// relay.example.com 是固件里那个网关级的来源，重新问一次才拿到全量列表。
await page.evaluate(() => {
  const row = [...document.querySelectorAll(".vrow")].find((r) => r.textContent.includes("relay.example.com"));
  const edit = [...(row?.querySelectorAll("button") ?? [])].find((b) => b.textContent.trim() === "编辑");
  edit?.scrollIntoView({ block: "center" });
  edit?.click();
});
if (!(await appear(PANEL, 5000, "连接的编辑面板"))) await giveUp();
await page.evaluate(() => {
  [...document.querySelectorAll(".addp[data-edit] button")]
    .find((b) => b.textContent.trim() === "重新问一次有哪些模型")?.click();
});
await page.waitForTimeout(900);

const read = () =>
  page.evaluate(() => {
    const box = document.querySelector(".addp[data-edit]");
    const rows = [...box.querySelectorAll(".mline")];
    const list = box.querySelector(".mrows");
    return {
      rows: rows.length,
      on: rows.filter((r) => r.querySelector('.tick[aria-checked="true"]')).length,
      // 勾上的必须排在前面，否则"我选了什么"要靠翻。
      firstOff: rows.findIndex((r) => r.hasAttribute("data-off")),
      lastOn: rows.map((r) => !r.hasAttribute("data-off")).lastIndexOf(true),
      scrolls: list.scrollHeight > list.clientHeight + 1,
      height: list.clientHeight,
      more: (box.querySelector(".mmore")?.textContent ?? "").trim(),
      names: rows.map((r) => r.querySelector(".nm").textContent),
      newRow: (box.querySelector(".mnew .lb")?.textContent ?? "").trim(),
      label: (box.querySelector(".mlb")?.textContent ?? "").trim(),
      // 整块编辑面板的高度。没有上限时一百多行会把保存按钮顶到几屏以外，
      // 而保存正是这一屏要做的事。
      panel: Math.round(box.getBoundingClientRect().height),
    };
  });

const type = async (s) => {
  const box = page.locator(`${PANEL} .mfind input`);
  await box.click();
  await box.fill("");
  if (s) await box.type(s, { delay: 10 });
  await page.waitForTimeout(300);
  return read();
};

let s = await read();
const all = Number(s.label.match(/\/(\d+)/)?.[1] ?? 0);
check("端点报出的是网关规模", all > 100, `${all} 个`);
check("一行一个 DOM 节点的列表有上限", s.rows < all, `列出 ${s.rows} / ${all}`);
check("砍掉的说出来，不是悄悄少", s.more !== "" && s.more.includes(String(all - s.rows)), s.more);
check("列表自己滚，不撑开面板", s.scrolls && s.height <= 320, `${s.height}px`);
check("整块面板还在一屏之内", s.panel < 900, `${s.panel}px`);
check("勾上的排在前面", s.lastOn < s.firstOff, `最后一个勾在 ${s.lastOn}，第一个没勾在 ${s.firstOff}`);

s = await type("omni");
check("搜索真的收窄", s.rows > 0 && s.rows < 20, `${s.rows} 行`);
check("搜出来的都带这个词", s.names.every((n) => n.toLowerCase().includes("omni")));
check("有匹配时，添加的入口排在结果后面", s.newRow.includes("omni"), s.newRow);

s = await type("qwen3-max");
check("完全同名时不提添加", s.newRow === "", s.newRow);

s = await type("zzz-not-on-the-list");
check("一个都不匹配时只剩添加", s.rows === 0 && s.newRow.includes("zzz-not-on-the-list"), s.newRow);

await page.locator(`${PANEL} .mnew`).click();
await page.waitForTimeout(400);
s = await read();
check(
  "填进去的落在最前面，而且是勾上的",
  s.names[0] === "zzz-not-on-the-list" && s.firstOff !== 0,
  `${s.names[0]} · 第一个没勾的在第 ${s.firstOff} 行`,
);
check("总数跟着加一", s.label.includes(`/${all + 1}`), s.label);

await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} 项不合格:\n  ` + fails.join("\n  "));
  process.exit(1);
}
console.log("\n全部通过");
