// 右栏（度量面板）：端点自己写的话有多长不由我们定，那一栏有多宽由我们定。
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1500, height: 900 } });
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));

await page.goto(PAGE, { waitUntil: "networkidle" });
// 等不到也是这条守卫的结论，不是它退出的方式。抛出去的话退出码归 node，报的
// 是一段栈 —— 而「面板不再长这样」正是它该说清楚的那一件事。
const appear = async (sel, ms) => {
  try {
    await page.waitForSelector(sel, { timeout: ms });
    return true;
  } catch {
    check(`右栏还是这条守卫认识的形状（等不到 ${sel}）`, false, `${ms}ms`);
    return false;
  }
};

const ready = (await appear(".app", 20000)) && (await appear('[data-b="mcp"] .srvrow', 10000));
if (!ready) {
  await browser.close();
  console.log(`\n${fails.length} 项不合格:\n  ` + fails.join("\n  ") + "\n  面板结构变了，先更新判据再谈通过");
  process.exit(1);
}
await page.waitForTimeout(400);

// 目标不在，是这条守卫要报的事，不是它崩掉的理由：一个抛出去的守卫说不清
// 它究竟看到了什么，而「面板换了形状」正是它该拦住的那一类改动。
const read = () =>
  page.evaluate(() => {
    const need = (sel, from = document) => from.querySelector(sel);
    const scroll = need(".side .scroll");
    const row = need('[data-b="mcp"] .srvrow');
    if (!scroll || !row) return { missing: !scroll ? ".side .scroll" : '[data-b="mcp"] .srvrow' };
    const nm = need(".nm", row);
    const fix = need(".fix", row);
    if (!nm || !fix) return { missing: !nm ? ".srvrow .nm" : ".srvrow .fix" };
    const rs = getComputedStyle(row);
    return {
      // 栏是固定宽的，一旦横向能滚，就是有东西把它撑开了 —— 整栏的每一块都跟着错位。
      overflow: scroll.scrollWidth - scroll.clientWidth,
      tag: row.tagName,
      chrome: `${rs.borderTopStyle} ${rs.backgroundColor}`,
      nmW: Math.round(nm.getBoundingClientRect().width),
      nmMono: getComputedStyle(nm).fontFamily.toLowerCase().includes("mono"),
      // 名字先被截掉，出路就没了；出路是这一行唯一不是状态的东西。
      fixW: Math.round(fix.getBoundingClientRect().width),
      nmChars: nm.textContent.length,
    };
  });

// 端点自己写的名字有多长不由我们定 —— 一整条路径、一个带 token 的 URL，中间
// 一个空格都没有。栏有多宽由我们定。
const stretch = (text) =>
  page.evaluate((t) => {
    const nm = document.querySelector('[data-b="mcp"] .srvrow .nm');
    if (nm) nm.textContent = t;
  }, text);

let s = await read();
if (s.missing) {
  check(`右栏还是这条守卫认识的形状（缺 ${s.missing}）`, false);
  await browser.close();
  console.log(`\n1 项不合格:\n  面板结构变了，先更新判据再谈通过`);
  process.exit(1);
}

check("这一栏没被撑开", s.overflow === 0, `横向溢出 ${s.overflow}px`);
check("名字看得见", s.nmW > 0, `${s.nmW}px`);
// 借 .job 的样式时这里是 <button> 套着系统那圈 outset 边框和灰底，
// 而栏里其它每一行都是无边框的。
check("不是一枚系统按钮", s.tag === "BUTTON" && s.chrome === "none rgba(0, 0, 0, 0)", s.chrome);
// 名字是机器事实，跟栏里其它行一样等宽。
check("名字等宽", s.nmMono, `nm mono=${s.nmMono}`);

// 端点常把一整条 PATH 或 URL 塞进来，中间一个空格都没有。
await stretch("https://example.com/" + "a".repeat(260) + "?token=deadbeef");
await page.waitForTimeout(250);
s = await read();
check("一整串没有空格的也没撑开这一栏", s.overflow === 0, `横向溢出 ${s.overflow}px`);
// 名字先被截掉，出路就没了 —— 而出路是这一行唯一能按的东西。
check("再长也没把出路挤没", s.fixW > 0, `「去修复」${s.fixW}px`);
check("确实是被截断而不是换行撑开", s.nmChars > 200 && s.nmW > 0, `${s.nmChars} 字 / ${s.nmW}px`);

await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} 项不合格:\n  ` + fails.join("\n  "));
  process.exit(1);
}
console.log("\n全部通过");
