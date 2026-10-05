// 让位的守卫：一栏可以让开，但不能让到回不来。
//
// 布局在窄屏下会放弃列（viewport.ts 的 FOLDS）。放弃有两种做法，两种都行：
// 栏换个地方继续待着（右栏横过来占满一行），或者栏收起来、把手留在缝上。
// 不行的是第三种 —— 栏没了，把它叫回来的那个控件也没了。那时窗口上不再有任何
// 东西指向它，用户读到的就是「功能消失了」。
//
// 这正是 1200px 以下会话侧栏发生过的事：`[data-fold~="rail"] .gutter-l
// { display: none }` 把唯一的入口连同栏一起藏掉，而 keys.ts 里没有快捷键。
// 1920×1080 在 175% 缩放下逻辑宽度是 1097 —— 一台很普通的笔记本。
//
// 断言读结构，不读像素：栏自己在不在屏上，或者它的把手能不能被点到。两者有
// 一个成立就行，都不成立才是失败。
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

// 每一档折叠，以及它折掉的那一栏。宽度取阈值下方一点，落在这一档里。
const CASES = [
  { fold: "rail", width: 1100, col: ".rail", gutter: ".gutter-l", label: "会话侧栏" },
  { fold: "rail", width: 960, col: ".rail", gutter: ".gutter-l", label: "会话侧栏（200% 缩放档）" },
  { fold: "side", width: 800, col: ".side", gutter: ".gutter-r", label: "度量栏" },
];

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));

await page.goto(PAGE, { waitUntil: "networkidle" });
await page.waitForSelector(".app", { timeout: 20000 });
await page.waitForTimeout(500);

// reach 回答一栏在这个宽度下还够不够得着：它自己在屏上，或者它的把手能被点到。
// 把手用 elementFromPoint 判，而不是读样式 —— 一个盖在它上面的元素同样让它点
// 不到，而那对用户是同一回事。
const reach = (sel, gutterSel) =>
  page.evaluate(
    ([col, gut]) => {
      const vis = (el) => {
        if (!el) return false;
        const b = el.getBoundingClientRect();
        if (b.width < 1 || b.height < 1) return false;
        for (let n = el; n; n = n.parentElement) {
          const s = getComputedStyle(n);
          if (s.display === "none" || s.visibility === "hidden" || Number(s.opacity) === 0) return false;
        }
        return true;
      };
      const column = document.querySelector(col);
      const grip = document.querySelector(`${gut} .grip`);
      let hit = false;
      if (vis(grip)) {
        const b = grip.getBoundingClientRect();
        const at = document.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2);
        hit = !!at && (at === grip || grip.contains(at) || at.contains(grip));
      }
      return {
        onScreen: vis(column),
        gripHit: hit,
        fold: document.documentElement.dataset.fold ?? "",
      };
    },
    [sel, gutterSel],
  );

for (const c of CASES) {
  await page.setViewportSize({ width: c.width, height: 900 });
  await page.waitForTimeout(350);
  const r = await reach(c.col, c.gutter);
  check(
    `${c.width}px：这一档真的折了 ${c.fold}`,
    r.fold.split(" ").includes(c.fold),
    `data-fold="${r.fold}"`,
  );
  check(
    `${c.width}px：${c.label}还够得着`,
    r.onScreen || r.gripHit,
    `栏在屏上=${r.onScreen} 把手可点=${r.gripHit}`,
  );
}

// 够得着还不够 —— 点下去要真的回来。左栏是收起的那一种，所以这一条对它有意义。
await page.setViewportSize({ width: 1100, height: 900 });
await page.waitForTimeout(350);
const before = await page.evaluate(() => document.querySelector(".app")?.dataset.rail ?? "");
// 点不到就是这道守卫要抓的那件事本身，所以它得报成一条失败，而不是抛出去把
// 脚本打断 —— 一个崩掉的守卫说不清它究竟看到了什么。
let clicked = true;
try {
  await page.click(".gutter-l .grip", { timeout: 3000 });
} catch {
  clicked = false;
}
check("1100px：折叠后把手点得到", clicked);
await page.waitForTimeout(400);
const after = await page.evaluate(() => ({
  rail: document.querySelector(".app")?.dataset.rail ?? "",
  width: document.querySelector(".rail")?.getBoundingClientRect().width ?? 0,
}));
check("1100px：折叠后点把手，会话侧栏真的回来", before === "off" && after.rail === "on" && after.width > 1,
  `${before} → ${after.rail}，宽 ${Math.round(after.width)}px`);

await browser.close();
if (fails.length) {
  console.error(`\n${fails.length} 条不合格：${fails.join("、")}`);
  process.exit(1);
}
console.log("\n让位的每一栏都还回得来。");
