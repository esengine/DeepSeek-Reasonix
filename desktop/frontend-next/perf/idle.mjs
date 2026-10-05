// 闲置的守卫：没人看得见的东西不许动，闲着的窗口不许烧 CPU。
//
// 一条动画不会因为看不见就停下。`.glowring` 的注释写着「停下就必须熄灭」，
// 熄灭的却只有 opacity —— 里面那个 150px 的径向渐变照旧沿着边框跑，每秒 60
// 次在一层 mask 里重新光栅化，从窗口打开的那一刻起，一直到关掉为止。报上来
// 的是「webview2 占 50% CPU，关掉软件立刻回到 1%」(#9481)。
//
// 两条断言，一条读结构一条读开销：看不见的元素上不许有动画在跑（机器再快也
// 是错的），以及动画在闲置窗口上的净开销要小（跟同一台机器关掉动画的自己比，
// 机器快慢自己抵消掉）。
import { chromium } from "playwright";

const URL = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const CPU = Number(process.env.PERF_CPU ?? 4);
const SECS = Number(process.env.PERF_SECS ?? 4);
const ROUNDS = Number(process.env.PERF_ROUNDS ?? 400);
// 动画在闲置窗口上可以占多少主线程。给的是净值 —— 同一台机器关掉动画再量一
// 遍，两者相减 —— 所以这个数不随机器快慢漂移。
const BUDGET = Number(process.env.PERF_IDLE_BUDGET ?? 10);

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const cdp = await page.context().newCDPSession(page);
await cdp.send("Performance.enable");
if (CPU > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU });
await page.goto(URL, { waitUntil: "networkidle" });
await page.waitForSelector(".app", { timeout: 15000 });
await page.waitForTimeout(700);

// 一条已经很长的记录：闲置的代价要在它身上量，空会话上什么都看不出来。
await page.evaluate(async (n) => {
  const yieldFrame = () => new Promise((r) => requestAnimationFrame(r));
  for (let i = 0; i < n; i++) {
    const id = `t${i}`;
    const args = JSON.stringify({ path: `internal/pkg/mod${i}/file${i}.go` });
    window.__feed({ kind: "turn_started" });
    window.__feed({ kind: "tool_dispatch", tool: { id, name: "bash", args } });
    window.__feed({ kind: "tool_result", tool: { id, name: "bash", args, output: `第 ${i} 次输出\n`.repeat(6), durationMs: 300 } });
    window.__feed({ kind: "text", text: `第 ${i} 段回答。\n\n- 要点一\n- 要点二\n\n` });
    window.__feed({ kind: "message" });
    window.__feed({ kind: "turn_done" });
    if (i % 20 === 19) await yieldFrame();
  }
  await yieldFrame();
}, ROUNDS);
await page.waitForTimeout(700);

// 在跑、而且没人看得见的动画。看不见的判据只取无可争议的三种：自己或祖先的
// opacity 是 0、visibility 不可见、盒子没有面积。滚出视口不算——那是内容多，
// 不是画了个没人要的东西。
const unseen = () =>
  page.evaluate(() => {
    const out = [];
    for (const a of document.getAnimations()) {
      if (a.playState !== "running") continue;
      const el = a.effect?.target;
      if (!(el instanceof Element)) continue;
      let why = "";
      for (let node = el; node instanceof Element; node = node.parentElement) {
        const cs = getComputedStyle(node);
        if (cs.visibility === "hidden" || cs.visibility === "collapse") why = `${node.tagName.toLowerCase()} visibility:${cs.visibility}`;
        else if (Number(cs.opacity) === 0) why = `${node.tagName.toLowerCase()}.${node.className?.toString().slice(0, 24)} opacity:0`;
        if (why) break;
      }
      // An SVG shape's box leaves out its stroke, so a drawn straight line has
      // no area of its own; whether it is seen is its drawing's question.
      const box = (el instanceof SVGGeometryElement && el.ownerSVGElement ? el.ownerSVGElement : el).getBoundingClientRect();
      if (!why && box.width * box.height === 0) why = "盒子没有面积";
      if (why) out.push(`${a.animationName ?? "?"} 在 ${el.tagName.toLowerCase()}.${el.className?.toString().slice(0, 24) || ""} 上（${why}）`);
    }
    return out;
  });

const read = async () => Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
async function busy() {
  await page.waitForTimeout(400);
  const before = await read();
  const t0 = Date.now();
  await page.waitForTimeout(SECS * 1000);
  const wall = Date.now() - t0;
  const after = await read();
  return (((after.TaskDuration ?? 0) - (before.TaskDuration ?? 0)) * 1000 * 100) / wall;
}

// 那道流光的两头都要看：只断言「看不见的不许动」，把它焊死成永远不动也是绿的。
// In studio the running signal is the run mark's R being traced (studio-r-run);
// the border glow is no longer part of the design.
const glow = () =>
  page.evaluate(() =>
    document.getAnimations().some((a) => a.animationName === "studio-r-run" && a.playState === "running"),
  );

const idleUnseen = await unseen();
check("闲置时没有动画画在看不见的东西上", idleUnseen.length === 0, idleUnseen.join("；") || "0 条");
check("闲置时那道流光是停的", !(await glow()));

await page.evaluate(() => window.__feed({ kind: "turn_started" }));
await page.waitForTimeout(900);
const runUnseen = await unseen();
check("跑着时也没有", runUnseen.length === 0, runUnseen.join("；") || "0 条");
check("跑起来时那道流光真的在走", await glow());
await page.evaluate(() => window.__feed({ kind: "turn_done" }));
await page.waitForTimeout(600);
check("停下后它也停下", !(await glow()));

const withAnim = await busy();
// 同一台机器、同一条记录，只是把动画关掉：两者之差就是动画自己的账，机器快慢
// 在相减里抵消，所以这个预算在开发机和 CI 上是同一个数。
await page.addStyleTag({ content: `*, *::before, *::after { animation: none !important }` });
const without = await busy();
await browser.close();

const net = withAnim - without;
check(
  `动画在闲置窗口上的净开销 ≤ ${BUDGET} 个百分点`,
  net <= BUDGET,
  `${withAnim.toFixed(1)}% − ${without.toFixed(1)}% = ${net.toFixed(1)} 点（降频 ${CPU}x）`,
);

console.log(fails.length ? `\n失败 ${fails.length} 项：\n- ${fails.join("\n- ")}` : "\n全部通过");
process.exit(fails.length ? 1 : 0);
