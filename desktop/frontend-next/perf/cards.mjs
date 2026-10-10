// Card quality is mostly a set of invariants: prose must stay in its column,
// machine output owns its scrollport, and decision actions remain reachable.
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=2&sess=2&turns=1";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, locale: "zh-CN", colorScheme: "dark" });
page.setDefaultTimeout(8000);
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));

await page.goto(PAGE, { waitUntil: "networkidle" });
await page.waitForSelector(".compose");
await page.evaluate(() => {
  const long = "https://private.example/" + "unpublished-deepseek-flash-vision-".repeat(45);
  window.__feed({ kind: "turn_started" });
  window.__feed({ kind: "text", text: long });
  window.__feed({ kind: "message" });
  window.__feed({ kind: "tool_dispatch", tool: { id: "read-ok", name: "read_file", args: '{"path":"a.ts"}', readOnly: true } });
  window.__feed({ kind: "tool_result", tool: { id: "read-ok", name: "read_file", args: '{"path":"a.ts"}', output: "1→ok", readOnly: true } });
  window.__feed({ kind: "tool_dispatch", tool: { id: "read-bad", name: "read_file", args: '{"path":"b.ts"}', readOnly: true } });
  window.__feed({ kind: "tool_result", tool: { id: "read-bad", name: "read_file", args: '{"path":"b.ts"}', err: "no such file", readOnly: true } });
  window.__feed({ kind: "tool_dispatch", tool: { id: "long-output", name: "bash", args: "run", readOnly: false } });
  window.__feed({ kind: "tool_result", tool: { id: "long-output", name: "bash", args: "run", output: "x".repeat(1800) + "\n" + "row\n".repeat(80), readOnly: false } });
  window.__feed({ kind: "approval_request", approval: { id: "approval-layout", tool: "bash", subject: long } });
  window.__feed({ kind: "ask_request", ask: { id: "ask-layout", questions: [
    { id: "q1", header: "一个很长的问题标题", prompt: "选择一种方案", options: [{ label: "方案 A" }, { label: "方案 B" }] },
    { id: "q2", header: "另一个同样很长的问题标题", prompt: "补充选择", options: [{ label: "继续" }, { label: "停止" }] },
    { id: "q3", header: "第三个问题标题", prompt: "最后一个选择", options: [{ label: "保留" }, { label: "删除" }] },
  ] } });
});

for (const { width, height } of [{ width: 1440, height: 900 }, { width: 640, height: 800 }, { width: 420, height: 640 }]) {
  await page.setViewportSize({ width, height });
  await page.waitForTimeout(500);
  await page.locator(".ask").scrollIntoViewIfNeeded();
  const layout = await page.evaluate(() => {
    const box = (selector) => document.querySelector(selector)?.getBoundingClientRect();
    const prose = document.querySelector('.call[data-k="say"] .out .txt');
    const flow = document.querySelector('[data-pane="flow"]');
    const ask = document.querySelector(".ask");
    const tabs = document.querySelector(".ask-tabs");
    const foot = document.querySelector(".ask-foot");
    const primary = foot?.querySelector(".btn");
    const secondary = foot?.querySelector(".dismiss");
    const apv = document.querySelector(".apv:not([data-sealed])");
    const actions = [...(apv?.querySelectorAll(".apv-ft .btn") ?? [])].map((el) => el.getBoundingClientRect());
    return {
      flowOverflow: flow ? flow.scrollWidth - flow.clientWidth : 999,
      proseOverflow: prose ? prose.scrollWidth - prose.clientWidth : 999,
      askOverflow: ask ? ask.scrollWidth - ask.clientWidth : 999,
      tabsOverflow: getComputedStyle(tabs).overflowX,
      primary: box(".ask-foot .btn"),
      secondary: box(".ask-foot .dismiss"),
      foot: box(".ask-foot"),
      free: (() => {
        const el = document.querySelector(".ask-pane[data-on] .other-wrap input");
        const r = el?.getBoundingClientRect();
        return r && r.width > 0 && r.height > 0 ? { top: r.top, bottom: r.bottom, left: r.left, right: r.right } : null;
      })(),
      view: { w: innerWidth, h: innerHeight },
      runLabel: document.querySelector(".studio-runlabel")?.textContent ?? "",
      approvalRows: [...new Set(actions.map((r) => Math.round(r.y)))].length,
      approvalMainWidth: actions[0]?.width ?? 0,
      approvalWidth: apv?.getBoundingClientRect().width ?? 0,
    };
  });
  check(`${width}px：正文不横向溢出`, layout.proseOverflow <= 1, `${Math.round(layout.proseOverflow)}px`);
  check(`${width}px：提问卡不撑宽`, layout.askOverflow <= 1, `${Math.round(layout.askOverflow)}px`);
  check(`${width}px：转录不横向溢出`, layout.flowOverflow <= 1, `${Math.round(layout.flowOverflow)}px`);
  check(`${width}px：只有选项的提问卡上，自由作答入口不点任何东西就可见`, !!layout.free);
  check(`${width}px：作答入口完整落在可视区内`, !!layout.free && layout.free.top >= 0 && layout.free.bottom <= layout.view.h && layout.free.left >= 0 && layout.free.right <= layout.view.w);
  check(`${width}px：状态行写明在等你回答`, layout.runLabel.includes("等待你回答"), layout.runLabel);
  check(`${width}px：问题标签可横向到达`, layout.tabsOverflow === "auto");
  if (width === 420) {
    check("420px：提问主次动作分行", layout.primary.bottom <= layout.secondary.top + 1);
    check("420px：提问动作都留在卡内", layout.secondary.right <= layout.foot.right + 1);
    check("420px：审批主操作独占首行", layout.approvalRows === 2 && layout.approvalMainWidth >= layout.approvalWidth * 0.8,
      `${layout.approvalRows} 行 / ${Math.round(layout.approvalMainWidth)}px`);
  }
}

const output = await page.evaluate(() => {
  const body = document.querySelector('[data-call="long-output"] .out-body');
  if (!body) return null;
  body.scrollLeft = body.scrollWidth;
  return { overflow: getComputedStyle(body).overflowX, left: body.scrollLeft, extra: body.scrollWidth - body.clientWidth };
});
check("长工具输出在折叠态即可横向阅读", output?.overflow === "auto" && output.left > 0 && output.extra > 0,
  output ? `${Math.round(output.extra)}px 可滚` : "没有输出视口");
check("合并读取仍显示失败", await page.locator(".call .fail", { hasText: "项失败" }).count() === 1);
check("回执没有重复根容器", await page.locator(".rc > .rc").count() === 0);

// The clean receipt shares one row between its label and the evidence behind
// it. The evidence is a command of any length; the label is CJK, which may break
// between any two glyphs, so it is the one that must not give up its width.
const rc = await browser.newPage({ viewport: { width: 1440, height: 900 }, locale: "zh-CN", colorScheme: "dark" });
rc.on("pageerror", (e) => fails.push("页面异常: " + e.message));
await rc.goto(PAGE, { waitUntil: "networkidle" });
await rc.waitForSelector(".compose");
await rc.evaluate(() => {
  const command = "test -s AGENTS.md && grep -q 'reasonix' AGENTS.md && ".repeat(8) + "go test ./...";
  window.__feed({ kind: "turn_started" });
  window.__feed({ kind: "text", text: "已经更新 AGENTS.md，并运行了验证命令。" });
  window.__feed({ kind: "message" });
  window.__feed({ kind: "turn_done", receipt: {
    verdict: "verified", saysSomething: true,
    changes: [{ path: "AGENTS.md", reviewed: true }],
    verifications: [{ command, passed: true }],
  } });
});
await rc.waitForSelector(".rc-ok .rc-t");
for (const width of [1440, 640, 420]) {
  await rc.setViewportSize({ width, height: 800 });
  await rc.waitForTimeout(300);
  const row = await rc.evaluate(() => {
    const label = document.querySelector(".rc-ok .rc-t");
    const src = document.querySelector(".rc-ok .rc-src");
    const lh = parseFloat(getComputedStyle(label).lineHeight);
    const tick = document.querySelector(".rc-ok .rc-tick").getBoundingClientRect();
    const rc = document.querySelector(".rc-ok").getBoundingClientRect();
    const say = [...document.querySelectorAll('.call[data-k="say"] .out .txt')].pop().getBoundingClientRect();
    return {
      lines: Math.round(label.getBoundingClientRect().height / lh), srcClipped: src.scrollWidth > src.clientWidth,
      dLeft: Math.round(tick.left - say.left), dRight: Math.round(rc.right - say.right),
    };
  });
  check(`${width}px：回执标签不被长命令挤成竖排`, row.lines === 1, `${row.lines} 行`);
  check(`${width}px：长命令在回执行内截断`, row.srcClipped);
  check(`${width}px：回执与回答左对齐`, Math.abs(row.dLeft) <= 1, `偏 ${row.dLeft}px`);
  check(`${width}px：回执不超出回答栏宽`, row.dRight <= 1, `超出 ${row.dRight}px`);
}

// In the reading measure the transcript and the composer are one column: every
// card's edges are the composer's. The full measure widens the transcript only.
const col = await browser.newPage({ viewport: { width: 1440, height: 900 }, locale: "zh-CN", colorScheme: "dark" });
col.on("pageerror", (e) => fails.push("页面异常: " + e.message));
await col.goto(PAGE, { waitUntil: "networkidle" });
await col.waitForSelector(".compose");
await col.evaluate(() => {
  window.__feed({ kind: "turn_started" });
  window.__feed({ kind: "text", text: "先读文件再构建。" });
  window.__feed({ kind: "message" });
  window.__feed({ kind: "tool_dispatch", tool: { id: "col-read", name: "read_file", args: '{"path":"a.ts"}', readOnly: true } });
  window.__feed({ kind: "tool_result", tool: { id: "col-read", name: "read_file", args: '{"path":"a.ts"}', output: "1→ok", readOnly: true } });
  window.__feed({ kind: "ask_request", ask: { id: "col-ask", questions: [
    { id: "q1", header: "方案", prompt: "选择一种方案", options: [{ label: "方案 A" }, { label: "方案 B" }] },
  ] } });
});
await col.waitForSelector('.call[data-k="ask"]');
const edges = () => col.evaluate(() => {
  const r = (el) => { const b = el.getBoundingClientRect(); return { l: Math.round(b.left), r: Math.round(b.right) }; };
  const all = (sel) => [...document.querySelectorAll(sel)].map(r);
  return {
    compose: r(document.querySelector(".compose")),
    flow: r(document.querySelector(".flow")),
    boxed: [...all(".flow .activity-group"), ...all('.flow .call[data-k="ask"]')],
    replies: all('.flow .call[data-k="say"] > .c'),
    bubbles: all('.flow .call[data-k="me"] .out .txt'),
  };
});
const setMeasure = (full) => col.evaluate((f) => {
  if (f) document.documentElement.dataset.measure = "full";
  else delete document.documentElement.dataset.measure;
}, full);
const composeAt = {};
for (const width of [1440, 1000]) {
  await col.setViewportSize({ width, height: 900 });
  await setMeasure(false);
  await col.waitForTimeout(300);
  const e = await edges();
  composeAt[width] = e.compose;
  const off = (xs, sides) => xs.filter((x) => sides.some((s) => Math.abs(x[s] - e.compose[s]) > 1));
  check(`${width}px 适宜阅读：找到了要比的卡片`, e.boxed.length >= 2 && e.replies.length >= 1 && e.bubbles.length >= 1,
    `${e.boxed.length} 张卡 / ${e.replies.length} 段回复 / ${e.bubbles.length} 个气泡`);
  check(`${width}px 适宜阅读：卡片左右边与输入框对齐`, off(e.boxed, ["l", "r"]).length === 0,
    `输入框 ${e.compose.l}–${e.compose.r}，卡片 ${JSON.stringify(off(e.boxed, ["l", "r"]))}`);
  check(`${width}px 适宜阅读：回复与输入框同宽`, off(e.replies, ["l", "r"]).length === 0,
    `输入框 ${e.compose.l}–${e.compose.r}，回复 ${JSON.stringify(off(e.replies, ["l", "r"]))}`);
  check(`${width}px 适宜阅读：用户气泡右边与输入框对齐`, off(e.bubbles, ["r"]).length === 0,
    `输入框右 ${e.compose.r}，气泡 ${JSON.stringify(off(e.bubbles, ["r"]))}`);
}
await col.setViewportSize({ width: 1440, height: 900 });
await setMeasure(true);
await col.waitForTimeout(300);
const full = await edges();
await setMeasure(false);
check("全宽：输入框宽度不跟着变", full.compose.l === composeAt[1440].l && full.compose.r === composeAt[1440].r,
  `适宜阅读 ${composeAt[1440].l}–${composeAt[1440].r}，全宽 ${full.compose.l}–${full.compose.r}`);
check("全宽：转录比输入框宽", full.flow.r - full.flow.l > full.compose.r - full.compose.l + 100,
  `转录 ${full.flow.r - full.flow.l}px，输入框 ${full.compose.r - full.compose.l}px`);
check("全宽：卡片跟着转录到边", full.boxed.every((x) => Math.abs(x.l - full.flow.l) <= 1 && Math.abs(x.r - full.flow.r) <= 1),
  `转录 ${full.flow.l}–${full.flow.r}，卡片 ${JSON.stringify(full.boxed)}`);

await browser.close();
if (fails.length) {
  console.error(`\n${fails.length} 项不合格：\n  ` + fails.join("\n  "));
  process.exit(1);
}
console.log("\n卡片层级、状态和窄栏几何全部通过。");
