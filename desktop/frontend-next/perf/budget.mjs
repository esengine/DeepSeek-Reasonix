// 预算的守卫：输入框上下的东西可以有很多，但转录不能因此没有。
//
// 这是从一张用户截图倒推出来的一类缺陷，而不是一个实例。当时 `.qitems` 没有
// 高度上限，一次失败的构建排了 41 条续接，`.compose` 是 flex: none —— 它要多高
// 有多高，900px 的窗格里转录量到 0px，输入框自己也被顶出屏幕。
//
// 修一个孩子不算修完：队列、扩展视图、错误条、运行通知都堆在同一处，其中只有
// slotrail 当时限了高。所以断言不问「队列多高」，问的是**转录还剩多少**，以及
// 输入框在不在屏上 —— 换成别的孩子撑爆，同一条断言照样会红。
//
// 断言读几何，不读类名：一个把包装层删掉、把 max-height 挪走、或者给 .compose
// 换个布局的改动，都应该在这里红。
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

// 转录至少要占窗格的这一份。定成 0.4 不是产品真理，是「别再掉到 0」的下限：
// 修好那天的实测是 900px 窗格里 466px，也就是 0.52，留了余量给以后加的东西。
const FLOOR = 0.4;

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));

// 队列排满到内核自己的上限。够不够满不由这个脚本判断 —— 64 是 sessioninbox 的
// maxItems，超过它内核会拒收，所以这就是布局要能承受的最坏情况。
await page.goto(`${PAGE}?queue=64&turns=40`, { waitUntil: "networkidle" });
await page.waitForSelector(".app", { timeout: 20000 });
await page.waitForSelector(".qi", { timeout: 10000 });
await page.waitForTimeout(500);

// 运行通知没有条数上限，也没人替它们记着限高：每条都要用户自己点「知道了」。
// 塞进去的是不同的 code，否则内核那条「同一件事不叠加」的规则会把它们折成一条，
// 而这里要量的正是它们不折的时候。
const NOTICES = 12;
await page.evaluate((n) => {
  for (let i = 0; i < n; i++) {
    window.__feed({
      kind: "notice",
      level: i % 3 === 0 ? "warn" : "error",
      audience: "operator",
      code: `probe_${i}`,
      text: `运行时报告第 ${i} 条`,
      detail: "一段够长的细节，长到足以让这一条自己占掉不止一行的高度。",
    });
  }
}, NOTICES);
await page.waitForTimeout(400);

const geom = () =>
  page.evaluate(() => {
    const box = (sel) => {
      const el = document.querySelector(sel);
      return el ? el.getBoundingClientRect() : null;
    };
    const pane = box(".pane");
    const flow = box('.scroll[data-pane="flow"]');
    const compose = box(".compose");
    const aux = box(".composeaux");
    const notes = box(".composenotes");
    const ta = box(".compose textarea");
    const rows = document.querySelectorAll(".qi").length;
    const bars = document.querySelectorAll(".rtbar").length;
    const scrolls = (sel) => {
      const el = document.querySelector(sel);
      return !!el && el.scrollHeight > el.clientHeight + 1;
    };
    return {
      pane: pane?.height ?? 0,
      flow: flow?.height ?? 0,
      compose: compose?.height ?? 0,
      aux: aux?.height ?? 0,
      notes: notes?.height ?? 0,
      rows,
      bars,
      qScrolls: scrolls(".qitems"),
      // 输入框整个在窗口里，不只是「存在」。被顶出去的那次它也还在 DOM 里。
      taInView: !!ta && ta.top >= 0 && ta.bottom <= innerHeight + 1,
      viewport: innerHeight,
    };
  });

for (const [w, h] of [
  [1440, 900],
  [1440, 700],
  [1100, 640],
]) {
  await page.setViewportSize({ width: w, height: h });
  await page.waitForTimeout(400);
  const g = await geom();
  const share = g.pane > 0 ? g.flow / g.pane : 0;
  check(
    `${w}×${h}：${g.rows} 条队列 + ${g.bars} 条运行通知下，转录仍占窗格 ${(share * 100).toFixed(0)}%`,
    share >= FLOOR,
    `转录 ${Math.round(g.flow)}px / 窗格 ${Math.round(g.pane)}px`,
  );
  check(`${w}×${h}：输入框整个在屏上`, g.taInView, `视口 ${g.viewport}px`);
  check(
    `${w}×${h}：让开的是辅助区自己，不是转录`,
    g.qScrolls,
    `队列行区可滚=${g.qScrolls}，辅助区 ${Math.round(g.aux)}px，通知区 ${Math.round(g.notes)}px`,
  );
}

// 矮窗口要真的报出 short，否则上面那三档只是碰巧没超预算。
await page.setViewportSize({ width: 1440, height: 700 });
await page.waitForTimeout(350);
const fold = await page.evaluate(() => document.documentElement.dataset.fold ?? "");
check("700px 高：这一档真的折了 short", fold.split(" ").includes("short"), `data-fold="${fold}"`);

// 长 preview 的完整内容仍然看得见，但看它的那块东西自己也有边界 —— 换掉原生
// title 的理由就是它没有。所以这里问的是「浮层受不受管」，跟上面几条是同一件事。
await page.setViewportSize({ width: 1440, height: 900 });
await page.waitForTimeout(400);

const hover = async (nth) => {
  await page.hover(`.qi:nth-child(${nth}) .pv`);
  // 打开有延迟，是为了让读一列行时不会逐行弹。等过它。
  await page.waitForTimeout(700);
  return page.evaluate(() => {
    const el = document.querySelector(".ovf");
    if (!el) return null;
    const b = el.getBoundingClientRect();
    return {
      w: Math.round(b.width),
      h: Math.round(b.height),
      inView: b.left >= 0 && b.top >= 0 && b.right <= innerWidth + 1 && b.bottom <= innerHeight + 1,
      scrolls: el.scrollHeight > el.clientHeight,
      selectable: getComputedStyle(el).userSelect !== "none",
    };
  });
};

const away = async () => {
  await page.mouse.move(4, 4);
  await page.waitForTimeout(400);
};

const short = await hover(1);
check("没被截断的那一行不弹任何东西", short === null, short ? `弹了 ${short.w}×${short.h}` : "");

await away();
const long = await hover(3);
check("被截断的那一行看得见全文", !!long, long ? `${long.w}×${long.h}` : "没弹出来");
if (long) {
  check("浮层整个在视口内", long.inView, `${long.w}×${long.h}`);
  check("浮层里的字能选", long.selectable);
}

// 天花板得有人证明它在。队列的 preview 被内核截到 120 runes，到不了这个高度 ——
// 所以台架另放一条长的：断言的是 .ovf 这个通用件，不是队列碰巧多长。
await away();
const huge = await hover(2);
check("再长也撞得到天花板", !!huge && huge.h <= 224, huge ? `${huge.h}px` : "没弹出来");
check("撞到之后是它自己滚，不是溢出去", !!huge && huge.scrolls && huge.inView, huge ? `可滚=${huge.scrolls} 在视口内=${huge.inView}` : "");

await browser.close();
if (fails.length) {
  console.error(`\n${fails.length} 条不合格：${fails.join("、")}`);
  process.exit(1);
}
console.log("\n输入框上下堆什么，转录都还在。");
