// The MCP rows under the context meter: how long a server's own name is, we do
// not decide; how wide the card is, we do.
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const CARD = ".studio-context-anchor .chrome-context-card";
const ROW = `${CARD} .chrome-mcp-row`;

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1500, height: 900 } });
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));

await page.goto(PAGE, { waitUntil: "networkidle" });
// Not finding the card is this guard's finding, not how it exits: a thrown
// timeout says nothing about what it saw.
const appear = async (sel, ms) => {
  try {
    await page.waitForSelector(sel, { timeout: ms });
    return true;
  } catch {
    check(`上下文卡片还是这条守卫认识的形状（等不到 ${sel}）`, false, `${ms}ms`);
    return false;
  }
};

let ready = await appear(".compose", 20000);
if (ready) {
  // Opened by its own control, so the card stays up without the pointer on it.
  await page.locator('[data-action="metrics.details"][data-value="context"]').click();
  ready = await appear(`${ROW} span`, 10000);
}
if (!ready) {
  await browser.close();
  console.log(`\n${fails.length} 项不合格:\n  ` + fails.join("\n  ") + "\n  卡片结构变了，先更新判据再谈通过");
  process.exit(1);
}
await page.waitForTimeout(300);

const read = () =>
  page.evaluate(([card, row]) => {
    const box = document.querySelector(card);
    const first = document.querySelector(row);
    const nm = first?.querySelector("span");
    const state = first?.querySelector("small");
    const manage = box?.querySelector('[data-action="settings.section"][data-value="ext"]');
    if (!box || !nm || !state || !manage) return { missing: true };
    const at = (el) => {
      const b = el.getBoundingClientRect();
      const hit = document.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2);
      return !!hit && (hit === el || el.contains(hit));
    };
    return {
      // The card is a fixed width; once it scrolls sideways, something forced it.
      overflow: box.scrollWidth - box.clientWidth,
      nmW: Math.round(nm.getBoundingClientRect().width),
      nmCut: nm.scrollWidth > nm.clientWidth,
      nmChars: nm.textContent.length,
      rowH: Math.round(first.getBoundingClientRect().height),
      stateW: Math.round(state.getBoundingClientRect().width),
      manageHit: at(manage),
    };
  }, [CARD, ROW]);

let s = await read();
if (s.missing) {
  check("上下文卡片还是这条守卫认识的形状（缺名字、状态或管理入口）", false);
  await browser.close();
  process.exit(1);
}
const restH = s.rowH;
check("卡片没被撑开", s.overflow === 0, `横向溢出 ${s.overflow}px`);
check("名字看得见", s.nmW > 0, `${s.nmW}px`);
check("管理入口按得到", s.manageHit);

// A server often reports a whole PATH or a URL with a token, not one space in it.
await page.evaluate((row) => {
  const nm = document.querySelector(`${row} span`);
  if (nm) nm.textContent = "https://example.com/" + "a".repeat(260) + "?token=deadbeef";
}, ROW);
await page.waitForTimeout(250);
s = await read();
check("一整串没有空格的也没撑开卡片", s.overflow === 0, `横向溢出 ${s.overflow}px`);
check("再长也没把状态挤没", s.stateW > 0, `状态 ${s.stateW}px`);
check("确实是被截断而不是换行撑开", s.nmChars > 200 && s.nmCut && s.rowH === restH, `${s.nmChars} 字 / 行高 ${s.rowH}px`);

await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} 项不合格:\n  ` + fails.join("\n  "));
  process.exit(1);
}
console.log("\n全部通过");
