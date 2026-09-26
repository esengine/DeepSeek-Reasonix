// A name is whatever the folder was called. Nothing bounds it, and the fixture's
// names are all short — so the widths in this window are only ever exercised by
// the polite case unless something supplies the impolite one.
//
// What is asserted is where content lands, never how it was clipped: an ellipsis
// and a scroller are both correct answers, and an element whose scrollWidth
// exceeds its box may simply be holding a tooltip nobody has hovered. The
// failures worth catching are a window pushed wider than itself, a title bar
// whose text runs under the controls at its right, and anything painted past the
// edge without a scroller to reach it.
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?ws=2&sess=6";
const WIDTHS = [1512, 1180, 900, 760];
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

// Two shapes, because they break differently: CJK wraps almost anywhere, a long
// Latin identifier has no break opportunity at all.
const CJK = "这是一个非常长的名字".repeat(12);
const LATIN = "averyveryverylongunbrokenidentifierwithoutspaces".repeat(4);
const PATH = "internal/provider/openai/streaming/".repeat(6) + "chunk_decoder.go";

const browser = await chromium.launch();
for (const width of WIDTHS) {
  const page = await browser.newPage({ viewport: { width, height: 880 } });
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".chrome");
  await page.waitForTimeout(900);

  await page.evaluate(({ cjk, latin, path }) => {
    const fill = (sel, text) => {
      for (const el of document.querySelectorAll(sel)) if (el.textContent?.trim()) el.textContent = text;
    };
    fill(".crumb-proj", latin);
    fill(".crumb > b", latin);
    fill(".sesstitle", cjk);
    fill(".wsname, .wsrow .nm", latin);
    fill(".hl .nm", latin);
    fill(".hl .arg", path);
  }, { cjk: CJK, latin: LATIN, path: PATH });
  await page.waitForTimeout(400);

  const seen = await page.evaluate(() => {
    const scrolled = (el) => {
      for (let p = el; p; p = p.parentElement) {
        const o = getComputedStyle(p);
        if (/auto|scroll/.test(o.overflowX) || /auto|scroll/.test(o.overflowY)) return true;
      }
      return false;
    };
    const chrome = document.querySelector(".chrome");
    const right = chrome?.querySelector(".r");
    const crumb = chrome?.querySelector(".crumb");
    // The rightmost edge anything in the breadcrumb actually paints to — the
    // container's own box says nothing, because its children may overflow it.
    let painted = 0;
    for (const el of crumb?.querySelectorAll("*") ?? []) {
      const r = el.getBoundingClientRect();
      if (r.width > 0 && r.right > painted) painted = r.right;
    }
    const past = [];
    for (const el of document.querySelectorAll("body *")) {
      // A collapsed column keeps its boxes but is inert and unpainted; what it
      // lays out past the edge is neither seen nor reachable.
      if (el.closest("[inert]") || !el.checkVisibility({ visibilityProperty: true })) continue;
      const r = el.getBoundingClientRect();
      if (r.width < 12 || r.height < 6) continue;
      if (r.right > innerWidth + 2 && !scrolled(el)) {
        past.push(`${el.tagName.toLowerCase()}.${el.className.toString().split(/\s+/)[0]}`);
      }
    }
    return {
      docWidth: document.documentElement.scrollWidth,
      win: innerWidth,
      painted: Math.round(painted),
      controls: right ? Math.round(right.getBoundingClientRect().left) : Infinity,
      past: [...new Set(past)].slice(0, 6),
    };
  });

  console.log(`\n${width}px`);
  check("长名字没有把窗口撑宽", seen.docWidth <= seen.win, seen.docWidth > seen.win ? `多出 ${seen.docWidth - seen.win}px` : "");
  check("面包屑没有画到右侧控件底下", seen.painted <= seen.controls,
    seen.painted > seen.controls ? `压进去 ${seen.painted - seen.controls}px` : "");
  check("没有元素画到窗口之外", seen.past.length === 0, seen.past.join(", "));
  await page.close();
}
// A switcher row in a service's detail: the hint either takes the width left
// beside the switch or drops below it. The detail column is ~470px even in a
// wide window, so a hint that only shrinks becomes one word per line.
for (const width of [1400, 640]) {
  const page = await browser.newPage({ viewport: { width, height: 900 } });
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".app");
  await page.keyboard.press("Meta+Comma");
  await page.waitForTimeout(500);
  await page.evaluate(() => document.getElementById("prefs-providers")?.click());
  await page.waitForTimeout(700);
  const hints = await page.evaluate(() =>
    [...document.querySelectorAll(".pdetail-options .vway")].map((row) => {
      const why = row.querySelector(".why");
      const range = document.createRange();
      range.selectNodeContents(why);
      const s = getComputedStyle(row);
      return {
        lb: (row.querySelector(".lb")?.textContent ?? "").trim(),
        width: Math.round(why.getBoundingClientRect().width),
        room: Math.round(row.clientWidth - parseFloat(s.paddingLeft) - parseFloat(s.paddingRight)),
        lines: new Set([...range.getClientRects()].map((r) => Math.round(r.top))).size,
      };
    }),
  );
  console.log(`\n${width}px 服务详情`);
  check("有带说明的开关行", hints.length > 0, `${hints.length} 行`);
  for (const h of hints) {
    check(`「${h.lb}」的说明没有被开关挤成竖条`, h.width >= Math.min(200, h.room * 0.6) && h.lines <= 4,
      `宽 ${h.width}px / 可用 ${h.room}px，${h.lines} 行`);
  }
  await page.close();
}
await browser.close();
console.log(fails.length ? `\n${fails.length} 项未通过` : "\n全部通过");
process.exit(fails.length ? 1 : 0);
