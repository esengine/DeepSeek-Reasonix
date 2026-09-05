// 真机验证外观设置：字号、界面缩放、字体、壁纸，都要真的作用到页面上。
import { chromium } from "playwright";
import { fileURLToPath } from "node:url";
import { mkdirSync } from "node:fs";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = fileURLToPath(new URL("shots", import.meta.url));
mkdirSync(SHOTS, { recursive: true });

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
// 判据里拿中文比 DOM，所以语种要钉住 —— 不钉就跟着运行环境走，在英文机器
// 上红的是台架没说自己要哪一种语言。locale.mjs 一直是这么开页面的。
const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));

await page.goto(PAGE, { waitUntil: "networkidle" });
await page.waitForSelector(".app", { timeout: 20000 });
await page.waitForTimeout(600);

// 先灌几轮，让转录里有真正的正文可以量。
await page.evaluate(async () => {
  const y = () => new Promise((r) => requestAnimationFrame(r));
  for (let i = 0; i < 4; i++) {
    window.__feed({ kind: "turn_started" });
    window.__feed({ kind: "tool_dispatch", tool: { id: `t${i}`, name: "bash", args: '{"cmd":"ls"}' } });
    window.__feed({ kind: "tool_result", tool: { id: `t${i}`, name: "bash", args: '{"cmd":"ls"}', output: "a.go\nb.go\n", durationMs: 12 } });
    window.__feed({ kind: "text", text: `第 ${i} 段回答，用来量正文的字号有没有跟着走。\n\n` });
    window.__feed({ kind: "message" });
    window.__feed({ kind: "turn_done" });
    await y();
  }
});
await page.waitForTimeout(400);

// 设置页有吸顶头，普通点击会被它挡住；这些是内容区里的控件，直接派发点击。
const clickIn = (group, label) =>
  page.evaluate(
    ([g, l]) => {
      const box = [...document.querySelectorAll('[role="group"]')].find((e) => e.getAttribute("aria-label") === g);
      [...(box?.querySelectorAll("button") ?? [])].find((b) => b.textContent.trim() === l)?.click();
    },
    [group, label],
  );

const readPx = () =>
  page.evaluate(() => {
    const el = document.querySelector("#flowScroll .out .txt");
    return el ? parseFloat(getComputedStyle(el).fontSize) : 0;
  });
const rootVar = (name) => page.evaluate((n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim(), name);

const base = await readPx();
// 默认字号按界面语言分档（readDefault(): 中文 14.5、西文 13.5）—— 写死一个常数
// 的时候，这条在中文界面上一直是红的，而红的是断言不是产品。
const wantRead = (await page.evaluate(() => document.documentElement.lang)).startsWith("zh") ? 14.5 : 13.5;
check("默认正文字号", Math.abs(base - wantRead) < 0.6, `${base}px（该是 ${wantRead}px）`);

// 打开设置 → 外观
await page.keyboard.press("Meta+Comma");
await page.waitForTimeout(500);
await page.evaluate(() => document.getElementById("prefs-appearance")?.click());
await page.waitForTimeout(400);
await page.screenshot({ path: `${SHOTS}/look-1-外观页.png` });

// 1) 正文字号
await clickIn("正文字号", "更大");
await page.waitForTimeout(500);
const bigger = await readPx();
check("正文调大生效", bigger > base + 2, `${base}px → ${bigger}px`);

// 2) 界面缩放。缩放后输入框还得在屏幕里 —— vh 不跟着除回去的话，
//    100vh 会溢出一个 zoom 倍，底部的输入框直接被顶出可视区。
const composerFits = () =>
  page.evaluate(() => {
    const el = document.querySelector(".compose");
    if (!el) return { ok: false, bottom: 0, vh: innerHeight };
    const r = el.getBoundingClientRect();
    return { ok: r.bottom <= innerHeight + 2 && r.top < innerHeight, bottom: Math.round(r.bottom), vh: innerHeight };
  });
for (const [label, want] of [["宽松", "1.15"], ["更大", "1.3"], ["紧凑", "0.9"]]) {
  await clickIn("界面大小", label);
  await page.waitForTimeout(500);
  const z = await page.evaluate(() => document.documentElement.style.zoom);
  check(`${label}缩放生效`, z === want, `zoom=${z || "(空)"}`);
  const fit = await composerFits();
  check(`${label}下输入框仍在屏幕里`, fit.ok, `底边 ${fit.bottom} / 视口 ${fit.vh}`);
}
await clickIn("界面大小", "宽松");
await page.waitForTimeout(400);
const zoom = await page.evaluate(() => document.documentElement.style.zoom);
await page.screenshot({ path: `${SHOTS}/look-2-放大后.png` });

// 3) 字体：写一个名字进去
await page.locator(".fontrow .fontown").first().fill("Georgia");
await page.waitForTimeout(600);
const ui = await rootVar("--ui");
check("自定义字体生效", ui.startsWith("Georgia,"), ui.slice(0, 46));
check("字体带兜底", ui.includes("sans-serif"), "写错名字不会把界面弄花");

// 3b) 等宽是另一个槽位，要单独验 —— 之前就是这里没测出来
await page.locator(".fontrow .fontown").nth(1).fill("Menlo");
await page.waitForTimeout(700);
const mono = await rootVar("--mono");
check("等宽字体生效", mono.startsWith("Menlo,"), mono.slice(0, 42));
const termFont = await page.evaluate(() => {
  const el = document.querySelector(".term");
  return el ? getComputedStyle(el).fontFamily : "";
});
check("终端块真的换了字", termFont.startsWith("Menlo"), termFont.slice(0, 42) || "(没有终端块)");

// 壁纸能不能被看见，和它有没有挂上是两回事。前面三条验的是管线：变量挂上了、
// 层开了、浓度不为零 —— 窗口里每一层都自己上色的时候，一个像素都透不出来，这
// 三条照样全绿，这个功能就是这么悄悄坏掉的。
//
// 所以这里量像素：同一屏在浓度 0.95 和 0 之间各截一张，让浏览器自己解码，求平均
// 色差。推理图层是靠不住的 —— elementFromPoint 只给最上面那个元素，画在它下面的
// 兄弟层根本不在祖先链上，照那样算会把「被挡住」算成「透得出来」。
async function wallpaperReaches(page, browser) {
  const setAlpha = (v) => page.evaluate((a) => document.documentElement.style.setProperty("--bg-alpha", a), String(v));
  const keep = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--bg-alpha").trim());
  await setAlpha(0.95);
  await page.waitForTimeout(500);
  const on = (await page.screenshot({ type: "png" })).toString("base64");
  await setAlpha(0);
  await page.waitForTimeout(500);
  const off = (await page.screenshot({ type: "png" })).toString("base64");
  await setAlpha(keep);
  await page.waitForTimeout(300);
  return avgDiff(browser, on, off);
}

// Mean channel difference between two same-sized shots, 0..255. Decoded by the
// browser, which is both simpler than pulling a decoder into node and the same
// decoder that drew the thing under test.
async function avgDiff(browser, on, off) {
  const scratch = await browser.newPage();
  const diff = await scratch.evaluate(async ([a, b]) => {
    const load = (d) =>
      new Promise((r) => {
        const i = new Image();
        i.onload = () => r(i);
        i.src = "data:image/png;base64," + d;
      });
    const [ia, ib] = await Promise.all([load(a), load(b)]);
    const c = document.createElement("canvas");
    c.width = ia.width;
    c.height = ia.height;
    const g = c.getContext("2d", { willReadFrequently: true });
    g.drawImage(ia, 0, 0);
    const da = g.getImageData(0, 0, c.width, c.height).data;
    g.clearRect(0, 0, c.width, c.height);
    g.drawImage(ib, 0, 0);
    const db = g.getImageData(0, 0, c.width, c.height).data;
    let sum = 0, n = 0;
    for (let k = 0; k < da.length; k += 4) {
      sum += Math.abs(da[k] - db[k]) + Math.abs(da[k + 1] - db[k + 1]) + Math.abs(da[k + 2] - db[k + 2]);
      n += 3;
    }
    return Math.round((sum / n) * 10) / 10;
  }, [on, off]);
  await scratch.close();
  return diff;
}

// 4) 壁纸：塞一张真图片进去
// 限定在壁纸那一组里 —— 输入框自己也有一个文件输入，不限定的话选择器命中两个，
// 这一整段就在这里抛异常退出，下面所有断言从来没有跑过。
const shot = page.locator('.grp:has(.paperpick) input[type="file"]');
await shot.setInputFiles({
  name: "paper.png",
  mimeType: "image/png",
  // 一张 2x2 的 PNG，够验证整条上传—落盘—回读的链路
  buffer: Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAFUlEQVR4nGP8z8Dwn4GBgYGJAQaAAAAA//8DAAKrAaXLmt0hAAAAAElFTkSuQmCC",
    "base64",
  ),
});
await page.waitForTimeout(900);
const bg = await rootVar("--bg-image");
check("壁纸挂上了", bg.startsWith("url("), bg.slice(0, 40));
check("背景层已开", (await page.evaluate(() => document.documentElement.dataset.bg)) === "on");
const alpha = await rootVar("--bg-alpha");
check("壁纸有可见的浓度", Number(alpha) > 0, `alpha=${alpha}`);

// 上面三条即使一个像素都没透出来也会全过 —— 它们验的是管线：变量挂上了、层开了、
// 浓度不为零。图看不看得见是另一回事：窗口里每一层都自己上色的时候，八个采样点
// 一个都透不出来，而这三条照样绿。所以这条验的是结果，从最上面那层往下把每一层
// 的不透明度乘掉，剩下多少就是壁纸能贡献多少。
await page.screenshot({ path: `${SHOTS}/look-3-壁纸.png` });

// 5) 浓度滑块。限定在壁纸那一组：字体「微调」也是 .slider，而且排在它前面 ——
//    .first() 一直拉的是那一根，这条断言从来没碰过浓度。
const sliders = page.locator(".grp:has(.paperpick) .slider");
await sliders.first().fill("0.9");
await page.waitForTimeout(600);
check("浓度可调", Number(await rootVar("--bg-alpha")) > 0.8, `alpha=${await rootVar("--bg-alpha")}`);

// 5a) Focus. Broken silently for as long as the preview was a fixed 116px strip
//     (about 5:1) standing in for a 16:10 window: `cover` only leaves a focal
//     point room on the axis it overflows on, so horizontal focus moved nothing
//     at all and read as a dead slider. Measured in shape and pixels rather than
//     in variables, which stayed green through all of it.
await page.locator(".paperview").scrollIntoViewIfNeeded();
await page.waitForTimeout(300);
const shape = await page.evaluate(() => {
  const el = document.querySelector(".paperview");
  const r = el.getBoundingClientRect();
  return { view: r.width / r.height, win: innerWidth / innerHeight };
});
check(
  "预览是窗口的形状",
  Math.abs(shape.view - shape.win) / shape.win < 0.02,
  `预览 ${shape.view.toFixed(2)} : 窗口 ${shape.win.toFixed(2)}`,
);

// A 16:9 four-colour picture: wider than the 16:10 window, so it overflows
// horizontally and fits exactly top to bottom — both outcomes on one image.
await shot.setInputFiles({
  name: "quad.png",
  mimeType: "image/png",
  buffer: Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAACAAAAASCAIAAAC1qksFAAAAKUlEQVR42u3NsQkAQBCEQPtv2q9hg0sewVQGYQqPCwj4EVgPZSogIEAeyi7NTwArCz4AAAAASUVORK5CYII=",
    "base64",
  ),
});
await page.waitForTimeout(900);
const paperview = page.locator(".paperview");
const focusX = page.locator(".grp:has(.paperpick) .prow", { hasText: "横向焦点" }).locator(".slider");
const focusY = page.locator(".grp:has(.paperpick) .prow", { hasText: "纵向焦点" }).locator(".slider");
// Ask before dragging: let the preview go back to a strip and horizontal is the
// axis with no room, so fill() would time out here and take every assertion
// below it down with the exception — which is the thing this section guards.
const canX = !(await focusX.isDisabled());
check("有余量的那条轴是可调的", canX, "16:9 的图比 16:10 的窗口宽，左右一定裁得掉");
check("没有余量的那条轴是停用的", await focusY.isDisabled(), "16:9 的图在 16:10 的窗口里，上下正好填满");
let moved = 0;
if (canX) {
  await focusX.fill("0");
  await page.waitForTimeout(500);
  const left = (await paperview.screenshot({ type: "png" })).toString("base64");
  await focusX.fill("1");
  await page.waitForTimeout(500);
  const right = (await paperview.screenshot({ type: "png" })).toString("base64");
  moved = await avgDiff(browser, left, right);
}
check("横向焦点真的移动了画面", moved > 2, `0 与 1 两端的平均色差 ${moved}/255`);
const why = await page.locator(".grp:has(.paperpick) .note").innerText().catch(() => "");
check("并且说得出为什么", why.includes("上下"), why || "(旁边什么也没说)");
if (canX) await focusX.fill("0.5");
await page.waitForTimeout(400);
await page.screenshot({ path: `${SHOTS}/look-6-焦点.png` });

// 5c) A drag is a stream of values, not a stream of decisions. One save per
//     pointer tick is the kernel's whole config file rewritten twenty times,
//     and two answers landing out of order put the thumb back two frames. The
//     fixture answers instantly, where coalesced and not look the same — so
//     give the save a round trip first, then count what actually went out.
const LAG = 400;
await page.evaluate((ms) => window.__saveLag(ms), LAG);
const dim = page.locator(".grp:has(.paperpick) .prow", { hasText: "压暗" }).locator(".slider");
await dim.scrollIntoViewIfNeeded();
await page.waitForTimeout(200);
await dim.evaluate((el) => {
  window.__ticks = 0;
  el.addEventListener("input", () => window.__ticks++);
});
const track = await dim.boundingBox();
const began = Date.now();
await page.mouse.move(track.x + track.width * 0.08, track.y + track.height / 2);
await page.mouse.down();
await page.mouse.move(track.x + track.width * 0.95, track.y + track.height / 2, { steps: 24 });
await page.mouse.up();
const drag = Date.now() - began;
const ticks = await page.evaluate(() => window.__ticks);
await page.waitForTimeout(LAG * 4);
const wrote = await page.evaluate(() => window.__saves());
// Writes are paced by the round trip, not by the pointer: one in flight plus
// one per lag the drag lasted, and a couple over for scheduling. Ticks is what
// it would be without any of that, so the bound has to stay well under it.
const ceiling = Math.ceil(drag / LAG) + 2;
check(
  "拖动按往返节流，不是按格子",
  ticks >= 8 && wrote.count <= ceiling,
  `${ticks} 格 · 拖了 ${drag}ms → ${wrote.count} 次写（上限 ${ceiling}）`,
);
check("同时只有一次写在飞", wrote.peak === 1, `峰值 ${wrote.peak} 次`);
check(
  "写进去的是松手时的那个值",
  Math.abs((wrote.last?.wallpaper?.dim ?? -1) - Number(await dim.inputValue())) < 1e-6,
  `落盘 ${wrote.last?.wallpaper?.dim} / 滑块 ${await dim.inputValue()}`,
);
await page.evaluate(() => window.__saveLag(0));

// 5b) 图真的到了屏幕上。设置页是全屏覆盖层，量之前要先把它收起来 —— 用户看的
//     是那一屏，不是设置页。
// 收起设置页。快捷键是个开关，按下去是开还是关取决于此刻的状态 —— 直接等它
// 真的从 DOM 里消失，不确定就再按一次 Escape，否则这条断言量的是设置页的底色。
await page.keyboard.press("Meta+Comma");
await page.waitForSelector(".prefs", { state: "detached", timeout: 3000 }).catch(async () => {
  await page.keyboard.press("Escape");
  await page.waitForSelector(".prefs", { state: "detached", timeout: 3000 }).catch(() => {});
});
await page.waitForTimeout(600);
check("设置页已收起", (await page.locator(".prefs").count()) === 0);
const reach = await wallpaperReaches(page, browser);
check("壁纸真的到了屏幕上", reach > 3, `开图与关图两屏的平均色差 ${reach}/255`);
await page.screenshot({ path: `${SHOTS}/look-5-窗口上的壁纸.png` });

// 6) 移除壁纸
await page.keyboard.press("Meta+Comma");
await page.waitForTimeout(600);
await page.evaluate(() => document.getElementById("prefs-appearance")?.click());
await page.waitForTimeout(500);
await page.evaluate(() => [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === "移除")?.click());
await page.waitForTimeout(700);
check("壁纸可移除", (await rootVar("--bg-image")) === "", `--bg-image="${await rootVar("--bg-image")}"`);
await page.screenshot({ path: `${SHOTS}/look-4-移除后.png` });

// 7) 重开页面：设置要还在（真的落了盘）。fixture 只存在内存里，所以这里
//    只验证没有落盘时不会残留脏状态；真机的持久化由 serve 的 config 负责。
await page.reload({ waitUntil: "networkidle" });
await page.waitForSelector(".app", { timeout: 20000 });
await page.waitForTimeout(700);
check("重开不残留壁纸", (await rootVar("--bg-image")) === "");

await browser.close();
console.log(fails.length ? `\n失败 ${fails.length} 项：\n- ${fails.join("\n- ")}` : "\n全部通过");
process.exit(fails.length ? 1 : 0);
