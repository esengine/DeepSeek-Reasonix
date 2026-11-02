// The team block with 30 agents and 10 groups: rows mount still, a reorder
// moves only the rows that changed place and keeps every row's element, and a
// sidebar scrolled between two reorders, or a reorder landing mid-slide, moves
// nothing that did not move. Geometry and Web Animations are browser questions.
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=1&sess=3&team=30&groups=10";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForFunction(() => document.querySelectorAll(".team-rail .team-row").length >= 41);
  await page.waitForTimeout(400);

  const edges = await page.evaluate(() =>
    [...document.querySelectorAll(".team-rail .team-row-name")].map((n) => Math.round(n.getBoundingClientRect().left)));
  check("every row's name starts at one edge, agents and groups alike", new Set(edges).size === 1, `left edges ${[...new Set(edges)].join(", ")}`);

  const sliding = () => page.evaluate(() =>
    [...document.querySelectorAll(".team-rail .team-row")].filter((r) => r.getAnimations().some((a) => a.playState === "running")).map((r) => r.dataset.key));
  check("rows mount still", (await sliding()).length === 0, `${(await sliding()).length} sliding after load`);

  await page.evaluate(() => {
    window.__rows = new WeakSet(document.querySelectorAll(".team-rail .team-row"));
    const rail = document.querySelector(".team-rail");
    let el = rail.parentElement;
    while (el && !(el.scrollHeight > el.clientHeight + 1)) el = el.parentElement;
    window.__scroller = el;
  });
  const scrolls = await page.evaluate(() => {
    const s = window.__scroller;
    return !!s && /(auto|scroll)/.test(getComputedStyle(s).overflowY) && !!s.querySelector(".projects-header");
  });
  check("a long team scrolls with the projects, not out of reach", scrolls);
  const order = () => page.evaluate(() => [...document.querySelectorAll(".team-rail .team-row")].map((r) => r.dataset.key));
  const keep = () => page.evaluate(() => [...document.querySelectorAll(".team-rail .team-row")].every((r) => window.__rows.has(r)));

  // A reorder with the sidebar scrolled since the last one.
  const before = await order();
  const scrolled = await page.evaluate(() => {
    const s = window.__scroller;
    if (!s) return 0;
    s.scrollTop += 240;
    return s.scrollTop;
  });
  const mover = before[25];
  await page.evaluate((id) => window.__team.touch(id, Date.now()), mover);
  await page.waitForFunction((id) => document.querySelector(".team-rail .team-row:nth-child(2)")?.dataset.key === id || [...document.querySelectorAll(".team-rail .team-row")].findIndex((r) => r.dataset.key === id) < 3, mover);
  const after = await order();
  const moved = new Set(after.filter((k, i) => before.indexOf(k) !== i));
  const slid = await sliding();
  const stray = slid.filter((k) => !moved.has(k));
  check("the sidebar was scrolled before the reorder", scrolled > 0, `scrollTop ${scrolled}`);
  check("a reorder slides only rows that changed place", stray.length === 0 && slid.includes(mover), `${slid.length} sliding, ${stray.length} of them stayed in place: ${stray.slice(0, 5).join(", ")}`);
  check("a reorder keeps every row's element", await keep());

  // A second reorder landing while the first is still sliding.
  await page.waitForTimeout(600);
  const second = (await order())[30];
  const third = (await order())[34];
  const { trace, fired } = await page.evaluate(async ([a, b]) => {
    const tops = [];
    const row = () => document.querySelector(`.team-rail .team-row[data-key="${a}"]`);
    const rail = () => document.querySelector(".team-rail").getBoundingClientRect().top;
    window.__team.touch(a, Date.now());
    const start = performance.now();
    let fired = -1;
    while (performance.now() < start + 700) {
      await new Promise((r) => requestAnimationFrame(r));
      const el = row();
      if (el) tops.push(el.getBoundingClientRect().top - rail());
      if (fired < 0 && performance.now() - start >= 90) {
        window.__team.touch(b, Date.now() + 1);
        fired = tops.length - 1;
      }
    }
    return { trace: tops, fired };
  }, [second, third]);
  if (process.env.TRACE) console.log(trace.map((y) => y.toFixed(0)).join(" "));
  // The row slides up, and the second reorder puts it one row lower: moving
  // down by more than that is a jump back toward where the slide began.
  const back = trace.slice(1).reduce((sum, y, i) => sum + Math.max(0, y - trace[i]), 0);
  const rowHeight = await page.evaluate(() => document.querySelector(".team-rail .team-row").getBoundingClientRect().height);
  const short = fired >= 0 ? Math.abs(trace[fired] - trace.at(-1)) : 0;
  check("the second reorder landed mid-slide", short > rowHeight, `${short.toFixed(0)}px short of rest when it landed`);
  check("a reorder landing mid-slide continues from where the row is", back <= rowHeight * 1.5, `moved back ${back.toFixed(1)}px, a row is ${rowHeight.toFixed(1)}px`);

  await page.waitForTimeout(700);
  const resting = await page.evaluate(() => [...document.querySelectorAll(".team-rail .team-row")].filter((r) => getComputedStyle(r).transform !== "none").length);
  check("every row comes to rest", resting === 0 && (await sliding()).length === 0, `${resting} still offset`);
  check("the latest activity leads", (await order()).indexOf(third) <= 2, `at ${(await order()).indexOf(third)}`);
  check("no row was remounted across the reorders", await keep());
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
