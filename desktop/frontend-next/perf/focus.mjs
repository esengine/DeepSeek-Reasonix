// 专注的守卫：临时拿走外围，退出后世界跟进来前完全一致。
//
// 这个功能只有一件事会做错，而做错的方式只有一种：把临时状态写回用户的偏好。
// 所以断言不看「栏收起来了没有」这种表象，看的是三层所有权有没有串味 ——
//
//   wanted（用户的选择）   allowed（当前视口）   focus（临时观看状态）
//   effective = wanted && allowed && !focus
//
// focus 只保存一个 bool，其余永远从各自的主人重新求值。凡是「进专注时记下当时
// 的样子、退出时照着摆回去」的实现，都会在这几条里露出来。
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));

const open = async (url = PAGE) => {
  await page.goto(url, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 20000 });
  await page.waitForTimeout(500);
};

const shell = () =>
  page.evaluate(() => {
    const app = document.querySelector(".app");
    const wide = (sel) => Math.round(document.querySelector(sel)?.getBoundingClientRect().width ?? -1);
    return {
      focus: app?.dataset.focus ?? "off",
      rail: app?.dataset.rail ?? "",
      side: app?.dataset.side ?? "",
      fold: document.documentElement.dataset.fold ?? "",
      railW: wide(".rail"),
      sideW: wide(".side"),
      flowW: wide('.scroll[data-pane="flow"]'),
      // 用户的选择存在盘上，是这条守卫唯一认的 wanted。
      wantedSide: localStorage.getItem("rx-side"),
    };
  });

// 按钮就是进出的那一枚，不另外发明快捷键。
const toggle = async () => {
  await page.click('.chrome .thbtn[aria-pressed]');
  await page.waitForTimeout(450);
};

await open();

// ── 1：用户本来就收起了右栏，进出专注不该把它改成别的 ──────────────────
await page.keyboard.press("Control+Shift+\\");
await page.waitForTimeout(400);
let s = await shell();
const closedByUser = s.side === "off" && s.wantedSide === "0";
check("先把右栏收起来（这一步是前提，不是断言）", closedByUser, `data-side=${s.side} rx-side=${s.wantedSide}`);
await toggle();
await toggle();
s = await shell();
check("进出专注后，用户收起右栏的选择原样还在", s.wantedSide === "0" && s.side === "off", `rx-side=${s.wantedSide} data-side=${s.side}`);

// ── 2：两栏都要，专注时都不见，退出全回来 ────────────────────────────
await page.keyboard.press("Control+Shift+\\");
await page.waitForTimeout(400);
const before = await shell();
check("两栏都开着（前提）", before.rail === "on" && before.side === "on" && before.railW > 1 && before.sideW > 1,
  `rail=${before.railW}px side=${before.sideW}px`);
await toggle();
const during = await shell();
check("专注时两栏都收走了", during.railW <= 1 && during.sideW <= 1, `rail=${during.railW}px side=${during.sideW}px`);
check("转录因此变宽", during.flowW > before.flowW, `${before.flowW}px → ${during.flowW}px`);

// ── 6：看不见就够不着。只做视觉隐藏的话，这一条最容易假绿 ──────────────
const reachable = await page.evaluate(() => {
  const hit = [];
  for (const col of [".rail", ".side"]) {
    for (const el of document.querySelectorAll(`${col} button, ${col} a, ${col} input, ${col} [tabindex]`)) {
      el.focus();
      if (document.activeElement && document.activeElement !== document.body && el.contains(document.activeElement)) {
        hit.push(col);
        break;
      }
    }
  }
  return hit;
});
check("专注时两栏都进不去键盘焦点", reachable.length === 0, reachable.join("、") || "一个都进不去");

// ── 5：专注不是阅读器，会话仍然可操作 ────────────────────────────────
// 一轮真在跑的时候：状态看得见，那一枚是「停下」而不是「发送」。
await page.evaluate(() => window.__feed({ kind: "turn_started" }));
await page.waitForTimeout(500);
const running = await page.evaluate(() => ({
  run: document.querySelector(".app")?.dataset.run ?? "",
  action: document.querySelector(".compose .go .send span:last-child")?.textContent?.trim() ?? "",
  composer: !!document.querySelector(".compose textarea"),
}));
check("专注时仍然看得出这一轮在跑", running.run === "running", `data-run=${running.run}`);
check("专注时那一枚是「停下」", running.action === "停下", running.action || "没有这个按钮");
check("专注时输入框还在", running.composer);

// 等你批准的一轮不是在跑的一轮 —— data-run 该是 halt，那是产品自己的说法，
// 这里断言的是「卡还在、按得到」，不是它把状态叫成什么。
await page.evaluate(() =>
  window.__feed({ kind: "approval_request", approval: { id: "f1", tool: "bash", subject: "rm -rf build/", risk: "high" } }),
);
await page.waitForTimeout(500);
check("专注时审批卡还在", await page.evaluate(() => !!document.querySelector(".apv-ft .btn")));
const sealed = await page.evaluate(async () => {
  document.querySelector(".apv-ft .btn")?.click();
  await new Promise((r) => setTimeout(r, 400));
  return document.querySelector(".apv")?.dataset.sealed ?? "";
});
check("专注时审批真的答得掉", sealed !== "", `data-sealed=${sealed}`);

// ── 3：专注中改窗口大小，responsive 不停算，也不写回 wanted ────────────
await page.setViewportSize({ width: 800, height: 900 });
await page.waitForTimeout(500);
const narrow = await shell();
check("专注中窄下来，responsive 照常算", narrow.fold.includes("side"), `data-fold="${narrow.fold}"`);
check("窄下来没有写回用户的选择", narrow.wantedSide === "1", `rx-side=${narrow.wantedSide}`);
await page.setViewportSize({ width: 1440, height: 900 });
await page.waitForTimeout(500);
await toggle();
const after = await shell();
check("退出专注后，两栏按当前视口重新求值而不是照旧摆回", after.rail === "on" && after.side === "on" && after.railW > 1 && after.sideW > 1,
  `rail=${after.railW}px side=${after.sideW}px`);
check("退出后几何回到进入之前", after.flowW === before.flowW, `${before.flowW}px → ${after.flowW}px`);
check("用户的选择全程没被动过", after.wantedSide === "1", `rx-side=${after.wantedSide}`);

// ── 4：它不是 workspace 偏好，重开一次就不该还在 ──────────────────────
await toggle();
check("重开前确实在专注里（前提）", (await shell()).focus === "true");
await open();
check("重开之后不在专注里", (await shell()).focus === "off");

await browser.close();
if (fails.length) {
  console.error(`\n${fails.length} 条不合格：${fails.join("、")}`);
  process.exit(1);
}
console.log("\n拿走的都还回来了，没动过用户的选择。");
