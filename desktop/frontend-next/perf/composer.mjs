// Composer geometry is interaction design: a control may exist in the DOM and
// still be unusable because a name pushed it away or a menu covers another one.
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&turns=4";
const BOX = 'textarea[role="combobox"]';
const fails = [];

const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: "dark", reducedMotion: "reduce" });
page.setDefaultTimeout(8000);
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));

await page.goto(PAGE, { waitUntil: "networkidle" });
await page.waitForSelector(".compose");
await page.evaluate(() => document.fonts.ready);

const frame = () => page.evaluate(() => new Promise((done) => requestAnimationFrame(() => requestAnimationFrame(done))));
const geometry = () => page.evaluate(() => {
  const box = document.querySelector('textarea[role="combobox"]');
  const compose = document.querySelector(".compose");
  const send = document.querySelector('[data-action="session.send"]');
  const rect = (el) => el?.getBoundingClientRect();
  const hit = (el) => {
    const b = rect(el);
    if (!b) return false;
    const at = document.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2);
    return !!at && (at === el || el.contains(at));
  };
  // The design may give the empty box a floor taller than one line; growth past
  // that floor is what a wrapping placeholder or a stale height would cause.
  const floor = box ? parseFloat(getComputedStyle(box).minHeight) || 0 : 0;
  return { box: rect(box), floor, compose: rect(compose), send: rect(send), sendHit: hit(send), fold: document.documentElement.dataset.fold ?? "" };
});

for (const { width, height, composeMax } of [
  { width: 1440, height: 900, composeMax: 150 },
  { width: 640, height: 900, composeMax: 150 },
  { width: 420, height: 520, composeMax: 220 },
]) {
  await page.setViewportSize({ width, height });
  await page.waitForTimeout(450);
  await page.fill(BOX, "");
  await frame();
  const g = await geometry();
  check(`${width}×${height}：空输入不超过一行或设计下限`, g.box.height <= Math.max(32, g.floor) + 0.5, `输入 ${Math.round(g.box.height)}px，下限 ${g.floor}px`);
  check(`${width}×${height}：编辑器不过度占高`, g.compose.height <= composeMax, `编辑器 ${Math.round(g.compose.height)}px`);
  await page.fill(BOX, "继续检查");
  await frame();
  const ready = await geometry();
  check(`${width}×${height}：主动作可见且可点`, ready.sendHit && ready.send.right <= width && ready.send.bottom <= height, `fold=${ready.fold}`);
}

// A private gateway may accept an unpublished name much longer than anything
// in the public catalogue. It may be truncated, but may not cover its peers.
await page.setViewportSize({ width: 420, height: 520 });
await page.evaluate(() => {
  const name = document.querySelector(".studio-model-control > button .nm");
  if (name) name.textContent = "vendor/internal/deepseek-flash-experimental-vision-20260910";
});
await frame();
const model = await page.evaluate(() => {
  const pick = document.querySelector(".studio-model-control");
  const button = document.querySelector(".studio-model-control > button");
  if (!pick || !button) return null;
  const a = button.getBoundingClientRect();
  // Every other control on the composer's toolbar, wherever the layout puts it.
  const peers = [...document.querySelectorAll(".compose button")].filter((el) => {
    const r = el.getBoundingClientRect();
    return !pick.contains(el) && r.width > 0 && r.height > 0 && el.checkVisibility({ visibilityProperty: true });
  });
  const covered = peers.filter((el) => {
    const r = el.getBoundingClientRect();
    return r.left < a.right - 1 && r.right > a.left + 1 && r.top < a.bottom - 1 && r.bottom > a.top + 1;
  }).map((el) => el.getAttribute("data-action") || el.className.toString().split(" ")[0]);
  return { width: a.width, slot: pick.getBoundingClientRect().width, covered };
});
check("模型按钮在场", !!model);
check("长模型名留在模型按钮内", !!model && model.width <= model.slot + 1, model ? `按钮 ${Math.round(model.width)} / 槽 ${Math.round(model.slot)}` : "");
check("长模型名不覆盖相邻控件", !!model && model.covered.length === 0, model?.covered.join(" / ") || "");

// Moving from a completion to a toolbar menu is one layer change, not two
// translucent menus competing for the same text and pointer.
await page.setViewportSize({ width: 1010, height: 800 });
await page.goto(PAGE, { waitUntil: "networkidle" });
await page.waitForSelector(".compose");
await page.fill(BOX, "@");
await page.waitForSelector(".slashmenu");
await page.click(".studio-model-control > button");
await page.waitForSelector(".slashmenu", { state: "detached" });
// Ask the control whether it opened, not where the menu was drawn: the menu is
// portalled to body, and aria-expanded is state the control itself declares.
await page.waitForSelector('.studio-model-control > button[aria-expanded="true"]');
check("补全与模型菜单互斥", await page.locator(".menu:not([hidden])").count() === 1);

// The toolbar shares its line with the send cluster, whose width depends on
// whether a turn runs. Every column width has to hold both: controls inside the
// composer, none under another, and the model name still readable.
const LONG_MODEL = "mimo-v2.6-pro-reasoning-preview";
const toolbar = () => page.evaluate((name) => {
  const nm = document.querySelector(".studio-model-control .nm");
  if (nm) nm.textContent = name;
  const edge = document.querySelector(".compose").getBoundingClientRect();
  const shown = [...document.querySelectorAll(".compose .row button")]
    .filter((el) => el.getClientRects().length && el.checkVisibility({ visibilityProperty: true }));
  const id = (el) => el.getAttribute("data-action") || el.getAttribute("aria-label") || el.className.toString();
  const boxes = shown.map((el) => ({ el, id: id(el), r: el.getBoundingClientRect() }));
  const outside = boxes.filter(({ r }) => r.left < edge.left - 0.5 || r.right > edge.right + 0.5 || r.bottom > edge.bottom + 0.5).map((b) => b.id);
  const overlaps = [];
  for (let i = 0; i < boxes.length; i++) for (let j = i + 1; j < boxes.length; j++) {
    const a = boxes[i], b = boxes[j];
    if (a.el.contains(b.el) || b.el.contains(a.el)) continue;
    if (a.r.left < b.r.right - 1 && a.r.right > b.r.left + 1 && a.r.top < b.r.bottom - 1 && a.r.bottom > b.r.top + 1) overlaps.push(`${a.id} × ${b.id}`);
  }
  return {
    pane: Math.round(document.querySelector(".pane").getBoundingClientRect().width),
    outside, overlaps, name: nm ? Math.round(nm.getBoundingClientRect().width) : 0,
  };
}, LONG_MODEL);

const swept = new Set();
for (const state of ["idle", "running"]) {
  await page.setViewportSize({ width: 1010, height: 800 });
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".compose");
  if (state === "running") {
    await page.evaluate(() => window.__feed({ kind: "turn_started" }));
    await page.waitForSelector('[data-action="session.stop"]');
  }
  const bad = [];
  for (let width = 340; width <= 760; width += 10) {
    await page.setViewportSize({ width, height: 800 });
    await frame();
    const g = await toolbar();
    swept.add(g.pane);
    if (g.outside.length) bad.push(`${g.pane}px 越界: ${g.outside.join(" / ")}`);
    if (g.overlaps.length) bad.push(`${g.pane}px 重叠: ${g.overlaps.join(" / ")}`);
    if (g.name < 40) bad.push(`${g.pane}px 模型名只剩 ${g.name}px`);
  }
  check(`${state}：各宽度下控件都在编辑器内、互不覆盖、模型名可读`, bad.length === 0, bad.slice(0, 4).join("；"));
}
check("扫到的对话列覆盖 360–700px", [...swept].some((w) => w <= 360) && [...swept].some((w) => w >= 700), [...swept].sort((a, b) => a - b).slice(0, 1).concat([...swept].sort((a, b) => b - a).slice(0, 1)).join("–"));

// An untouched phone composer leaves room to read; focus or a draft restores
// the editing controls without replacing the textarea or its caret.
const phone = () => page.evaluate(() => {
  const box = document.querySelector('textarea[role="combobox"]');
  const shown = (selector) => document.querySelector(selector)?.getBoundingClientRect().height > 0;
  const tools = [...document.querySelectorAll(".turntools > *")]
    .filter((el) => el.getBoundingClientRect().height > 0);
  const rows = new Set(tools.map((el) => {
    const r = el.getBoundingClientRect();
    return Math.round((r.top + r.height / 2) / 2);
  }));
  return { height: box.getBoundingClientRect().height, line: parseFloat(getComputedStyle(box).lineHeight),
    refine: shown(".studio-refine"), branch: shown(".studio-branch-pop"), rows: rows.size };
});
await page.setViewportSize({ width: 390, height: 760 });
await page.goto(PAGE, { waitUntil: "networkidle" });
await page.waitForSelector(BOX);
await page.click('[role="tab"][aria-selected="true"]');
await frame();
const quiet = await phone();
check("手机：空置输入只占一行", quiet.height <= quiet.line + 1);
check("手机：空置时收起优化与分支提示", !quiet.refine && !quiet.branch);
check("手机：空置工具栏只占一行", quiet.rows === 1);
await page.click(BOX);
await frame();
const focused = await phone();
check("手机：聚焦恢复编辑空间与优化入口", focused.height > quiet.height && focused.refine);
await page.fill(BOX, "保留草稿\n第二行\n第三行");
await page.click('[role="tab"][aria-selected="true"]');
await frame();
const draft = await phone();
check("手机：未聚焦草稿仍展开", draft.height >= draft.line * 3 - 1 && draft.refine);
check("手机：展开后草稿原样保留", await page.inputValue(BOX) === "保留草稿\n第二行\n第三行");
await page.fill(BOX, "");
await page.getByRole("button", { name: "引用到输入框", exact: true }).last().click();
await page.waitForSelector(".shots");
await page.click('[role="tab"][aria-selected="true"]');
await frame();
const quoted = await phone();
check("手机：仅有引用附件也保留编辑空间", quoted.height > quiet.height && quoted.refine);

await browser.close();
if (fails.length) {
  console.error(`\n${fails.length} 项不合格：\n  ` + fails.join("\n  "));
  process.exit(1);
}
console.log("\n编辑器关键几何与浮层互斥全部通过。");
