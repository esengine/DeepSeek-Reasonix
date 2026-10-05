// A running step has to say so on its own row, and a row's parts sit where
// they are read: the status mark, the tool's name, then what the call did.
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=2&sess=2&turns=1";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();

async function open(width, colorScheme, reducedMotion = "no-preference") {
  const page = await browser.newPage({ viewport: { width, height: 800 }, locale: "zh-CN", colorScheme, reducedMotion });
  page.setDefaultTimeout(8000);
  page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".compose");
  await page.evaluate(() => {
    const args = JSON.stringify({ command: '$f="C:\\Users\\me\\AppData\\Local\\reasonix-studio-host.exe"; Get-Item $f' });
    const shell = { kind: "shell", shell: "powershell" };
    window.__feed({ kind: "turn_started" });
    window.__feed({ kind: "text", text: "先看一下宿主进程。" });
    window.__feed({ kind: "tool_dispatch", tool: { id: "sh-done", name: "bash", args, readOnly: false, execution: shell } });
    window.__feed({ kind: "tool_result", tool: { id: "sh-done", name: "bash", args, output: "ok", durationMs: 44000, readOnly: false, execution: shell } });
    window.__feed({ kind: "tool_dispatch", tool: { id: "sh-run", name: "bash", args, readOnly: false, execution: shell } });
  });
  await page.waitForSelector('[data-call="sh-run"] .tool-state[data-state="running"]');
  await page.waitForTimeout(400);
  return page;
}

const row = (page, id) =>
  page.evaluate((id) => {
    const hl = document.querySelector(`[data-call="${id}"] .hl`);
    const box = (sel) => hl.querySelector(sel).getBoundingClientRect();
    // Where the glyphs end, not the box: a fixed-width box ends where it was
    // told to, and the blank between its text and its edge is the defect.
    const ink = (sel) => {
      const range = document.createRange();
      range.selectNodeContents(hl.querySelector(sel));
      return range.getBoundingClientRect();
    };
    const icon = hl.querySelector(".tool-state .studio-icon");
    return {
      animation: getComputedStyle(icon).animationName,
      icon: box(".tool-state"), nm: ink(".nm"), arg: ink(".arg"),
      clipped: hl.querySelector(".nm").scrollWidth > hl.querySelector(".nm").clientWidth,
    };
  }, id);

for (const colorScheme of ["dark", "light"]) {
  for (const width of [1440, 640]) {
    const page = await open(width, colorScheme);
    const run = await row(page, "sh-run");
    const done = await row(page, "sh-done");
    const tag = `${colorScheme} ${width}px`;
    check(`${tag}：运行中的步骤图标在动`, run.animation !== "none", run.animation);
    check(`${tag}：已完成的步骤图标不动`, done.animation === "none", done.animation);
    // One fact moves in one place: the step's own mark while the group is open,
    // the group's mark once it is folded and the step can no longer be seen.
    const group = () => page.evaluate(() => {
      const g = document.querySelector('.activity-group:has([data-call="sh-run"])');
      const spin = (el) => getComputedStyle(el).animationName;
      return {
        head: spin(g.querySelector(".activity-status-icon")),
        step: spin(g.querySelector('[data-call="sh-run"] .tool-state .studio-icon')),
        says: g.querySelector("summary .activity-running")?.textContent ?? "",
      };
    });
    const opened = await group();
    check(`${tag}：执行过程的标题说出有步骤在跑`, opened.says.includes("1"), opened.says);
    check(`${tag}：展开时只有步骤自己的图标在动`, opened.head === "none" && opened.step !== "none", `${opened.head} / ${opened.step}`);
    await page.locator('.activity-group:has([data-call="sh-run"]) > summary').click();
    await page.waitForTimeout(250);
    const folded = await group();
    check(`${tag}：收起后由标题的图标接着动`, folded.head !== "none" && folded.step === "none", `${folded.head} / ${folded.step}`);
    for (const [id, r] of [["运行中", run], ["已完成", done]]) {
      const lead = Math.round(r.nm.left - r.icon.right);
      const gap = Math.round(r.arg.left - r.nm.right);
      check(`${tag}：${id}行名字紧跟状态图标`, lead >= 0 && lead <= 12, `${lead}px`);
      check(`${tag}：${id}行命令紧跟名字，没有空着的一栏`, gap >= 0 && gap <= 12, `${gap}px`);
      check(`${tag}：${id}行的名字没被命令挤掉`, !r.clipped);
    }
    check(`${tag}：运行中与已完成两行的图标对齐`, Math.abs(run.icon.left - done.icon.left) <= 1,
      `${Math.round(run.icon.left - done.icon.left)}px`);
    await page.close();
  }
}

const still = await open(1440, "dark", "reduce");
const calm = await row(still, "sh-run");
check("减弱动态：运行中的图标不转", calm.animation === "none", calm.animation);
const color = await still.evaluate(() => getComputedStyle(document.querySelector('[data-call="sh-run"] .tool-state')).backgroundColor);
check("减弱动态：运行中仍有底色说明它在跑", color !== "rgba(0, 0, 0, 0)", color);
await still.close();

await browser.close();
if (fails.length) {
  console.error(`\n${fails.length} 项不合格：\n  ` + fails.join("\n  "));
  process.exit(1);
}
console.log("\n运行中的步骤看得见，步骤行没有空栏。");
