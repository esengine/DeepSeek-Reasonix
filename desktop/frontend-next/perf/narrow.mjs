// Under a high interface scale the layout's room is the window divided by the zoom, so a phone-width window can leave
// the shell a couple of hundred CSS pixels. Rows that cannot fit must give something up, never paint over each other.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = process.env.NARROW_SHOTS;
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const probe = (page) =>
  page.evaluate(() => {
    const live = (el) => {
      const r = el.getBoundingClientRect();
      return r.width > 0 && r.height > 0 && getComputedStyle(el).visibility !== "hidden" ? r : null;
    };
    const name = (el) => (el.className && String(el.className).split(/\s+/)[0]) || el.tagName;
    const overlaps = (parent) => {
      const boxes = [...parent.children].map((el) => [el, live(el)]).filter(([, r]) => r);
      const hits = [];
      for (let i = 0; i < boxes.length; i++)
        for (let j = i + 1; j < boxes.length; j++) {
          const [a, ra] = boxes[i];
          const [b, rb] = boxes[j];
          const x = Math.min(ra.right, rb.right) - Math.max(ra.left, rb.left);
          const y = Math.min(ra.bottom, rb.bottom) - Math.max(ra.top, rb.top);
          if (x > 1 && y > 1) hits.push(`${name(a)}/${name(b)}`);
        }
      return hits;
    };
    const outside = (parent) =>
      [...parent.children].filter((el) => live(el) && live(el).right > innerWidth + 1).map(name);
    const flatOverlaps = (els) => {
      const boxes = els.map((el) => [el, live(el)]).filter(([, r]) => r);
      const hits = [];
      for (let i = 0; i < boxes.length; i++)
        for (let j = i + 1; j < boxes.length; j++) {
          const x = Math.min(boxes[i][1].right, boxes[j][1].right) - Math.max(boxes[i][1].left, boxes[j][1].left);
          const y = Math.min(boxes[i][1].bottom, boxes[j][1].bottom) - Math.max(boxes[i][1].top, boxes[j][1].top);
          if (x > 1 && y > 1) hits.push(`${name(boxes[i][0])}/${name(boxes[j][0])}`);
        }
      return hits;
    };
    const bar = document.querySelector(".chrome");
    const row = document.querySelector(".compose .row");
    const tools = document.querySelector(".compose .turntools");
    const crumb = bar.querySelector(".crumb");
    const title = crumb.querySelector("b");
    const clip = crumb.getBoundingClientRect();
    const t = title.getBoundingClientRect();
    return {
      bar: { overlaps: overlaps(bar).concat(overlaps(bar.querySelector(".r"))), outside: outside(bar).concat(outside(bar.querySelector(".r"))) },
      row: { overlaps: overlaps(row), outside: outside(row) },
      viewSwitch: [...bar.querySelectorAll(".view-action")].map((el) => el.getAttribute("data-value")),
      sideAsk: (() => { const el = tools.querySelector('[data-action="side.toggle"]'); const r = el && live(el); return r ? { w: r.width, h: r.height } : null; })(),
      toolsOverlaps: flatOverlaps([...tools.children]),
      toolsOverflow: [...tools.children].filter((el) => live(el) && live(el).right > tools.getBoundingClientRect().right + 1).map(name),
      toolsOverlap: flatOverlaps([...tools.querySelectorAll("button")]),
      titleShown: t.width,
      titleOutsideCrumb: t.width > 0 && (t.left < clip.left - 1 || t.right > clip.right + 1),
      crumbWidth: clip.width,
      room: document.body.clientWidth,
    };
  });

const CONTROLS = [".phone-action", ".find-action", ".theme-toggle", ".view-action"];

// Opens the fold when there is one, then presses nothing: asks whether each control is on screen with nothing over it.
async function reachable(page) {
  const more = page.locator(".chrome .r-more");
  if (await more.isVisible()) {
    await more.click();
    await page.waitForTimeout(150);
  }
  return page.evaluate((controls) => {
    const out = [];
    for (const sel of controls) {
      const el = document.querySelector(`.chrome ${sel}`);
      if (!el) {
        out.push(`${sel} missing`);
        continue;
      }
      const r = el.getBoundingClientRect();
      const top = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      const inside = r.width > 0 && r.left >= 0 && r.right <= innerWidth + 1;
      if (!inside || !(el === top || el.contains(top))) out.push(`${sel}${inside ? " covered" : " off screen"}`);
    }
    return out;
  }, CONTROLS);
}

const browser = await chromium.launch();

// The fold's threshold is derived, not chosen: wherever the bar is not folded the title keeps its floor.
{
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  const bad = [];
  for (let room = 190; room <= 460; room += 10) {
    await page.setViewportSize({ width: room, height: 800 });
    await page.goto(`${BASE}?pref=en&turns=2`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    await page.waitForTimeout(200);
    const w = await page.evaluate(() => document.querySelector(".chrome .crumb").getBoundingClientRect().width);
    if (w < 48) bad.push(`${room}:${Math.round(w)}`);
  }
  check("at every room from 190 to 460 the title keeps its 4em floor", bad.length === 0, bad.join(" "));
  await page.close();
}
// A running turn swaps Send for Stop, which is wider; the toolbar must still sit on one line at every column width.
{
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  await page.goto(`${BASE}?pref=zh&turns=2`, { waitUntil: "networkidle" });
  await page.waitForSelector(".compose");
  await page.fill('textarea[role="combobox"]', "hello");
  await page.keyboard.press("Enter");
  await page.waitForSelector(".btn.stop");
  const bad = [];
  for (let room = 420; room <= 760; room += 20) {
    await page.setViewportSize({ width: room, height: 800 });
    await page.waitForTimeout(150);
    const lines = await page.evaluate(() => {
      const seen = [...document.querySelectorAll(".compose .row button")].filter((el) => el.getBoundingClientRect().width > 0 && el.checkVisibility());
      const mids = seen.map((el) => { const r = el.getBoundingClientRect(); return r.top + r.height / 2; }).sort((a, b) => a - b);
      return mids.filter((m, i) => i === 0 || m - mids[i - 1] > 6).length;
    });
    if (lines !== 1) bad.push(`${room}:${lines}`);
  }
  check("while a turn runs, the composer toolbar stays on one line from 420 to 760", bad.length === 0, bad.join(" "));
  await page.close();
}
for (const scheme of ["light", "dark"]) {
  for (const [width, zoom] of [[390, 1.8], [390, 1.3], [390, 1], [320, 1.8], [320, 1], [1280, 1]]) {
    const page = await browser.newPage({ viewport: { width, height: 800 }, colorScheme: scheme, reducedMotion: "reduce" });
    page.setDefaultTimeout(8000);
    page.on("pageerror", (e) => fails.push("page error: " + e.message));
    await page.goto(`${BASE}?pref=en&turns=2&zoom=${zoom}`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    await page.evaluate(() => document.fonts.ready);
    await page.waitForTimeout(500);
    const tag = `${scheme} ${width}px zoom ${zoom}`;
    const m = await probe(page);
    check(`${tag}: title bar controls do not overlap`, m.bar.overlaps.length === 0, m.bar.overlaps.join(", "));
    check(`${tag}: title bar stays inside the window`, m.bar.outside.length === 0, m.bar.outside.join(", "));
    check(`${tag}: the workbench switch is on the bar`, m.viewSwitch.includes("browser"), JSON.stringify(m.viewSwitch));
    check(`${tag}: composer row does not overlap`, m.row.overlaps.length === 0, m.row.overlaps.join(", "));
    check(`${tag}: the side question icon is drawn in the toolbar`, !!m.sideAsk && m.sideAsk.w > 0 && m.sideAsk.h > 0, JSON.stringify(m.sideAsk));
    check(`${tag}: composer toolbar controls do not overlap`, m.toolsOverlaps.length === 0, m.toolsOverlaps.join(", "));
    check(`${tag}: composer toolbar controls stay in the toolbar`, m.toolsOverflow.length === 0, m.toolsOverflow.join(", "));
    check(`${tag}: composer toolbar buttons do not overlap`, m.toolsOverlap.length === 0, m.toolsOverlap.join(", "));
    check(`${tag}: the title keeps its 4em floor inside its own box`, m.crumbWidth >= 48 && !m.titleOutsideCrumb, `crumb ${Math.round(m.crumbWidth)}px`);
    if (SHOTS) await page.screenshot({ path: `${SHOTS}/${process.env.NARROW_TAG ?? "shot"}-${scheme}-${width}-z${zoom}.png` });
    const hint = await page.evaluate(() => {
      const el = document.querySelector(".compose textarea");
      const cs = getComputedStyle(el);
      const ctx = document.createElement("canvas").getContext("2d");
      ctx.font = `${cs.fontStyle} ${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`;
      const room = el.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
      return { text: el.placeholder, width: ctx.measureText(el.placeholder).width, room };
    });
    check(`${tag}: the composer's placeholder is on one line and not empty`, hint.text.length > 0 && hint.width <= hint.room + 1, JSON.stringify(hint));
    const reach = await reachable(page);
    check(`${tag}: every title-bar control can be reached, folded or not`, reach.length === 0, reach.join(", "));
    if (SHOTS) await page.screenshot({ path: `${SHOTS}/${process.env.NARROW_TAG ?? "shot"}-open-${scheme}-${width}-z${zoom}.png` });
    await page.close();
  }
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
console.log("\nall ok");
