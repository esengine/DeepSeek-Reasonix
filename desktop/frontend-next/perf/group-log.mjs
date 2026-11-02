// A group chat 2000 lines long: it opens on its latest page at the bottom, a
// page paged in above keeps the lines on screen where they were, a line that
// arrives while the person reads older ones leaves them there, and one that
// arrives at the bottom is followed. Scroll geometry is a browser question.
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=1&sess=2&team=2&groups=1&grouplog=2000";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".team-rail .team-row-open[data-action-click='team.group-open']");
  const started = Date.now();
  await page.click(".team-rail .team-row-open[data-action-click='team.group-open']");
  await page.waitForFunction(() => [...document.querySelectorAll(".team-stage-log .team-line")].some((l) => l.textContent.includes("第 2000 行")));
  const opened = Date.now() - started;
  await page.waitForTimeout(300);

  const state = () => page.evaluate(() => {
    const log = document.querySelector(".team-stage-log");
    const lines = [...log.querySelectorAll(".team-line")];
    const box = log.getBoundingClientRect();
    const seen = lines.find((l) => l.getBoundingClientRect().bottom > box.top + 1);
    return {
      count: lines.length,
      fromEnd: Math.round(log.scrollHeight - log.scrollTop - log.clientHeight),
      first: lines[0]?.textContent.match(/第 (\d+) 行/)?.[1],
      seen: seen?.textContent.match(/第 (\d+) 行/)?.[1],
      seenTop: seen ? seen.getBoundingClientRect().top : 0,
      more: !!log.querySelector("[data-action='team.group-older']"),
    };
  });

  const open = await state();
  console.log(`  ..  opened in ${opened}ms with ${open.count} lines drawn`);
  check("a long log opens on its latest page", open.count === 200 && open.first === "1801", `${open.count} lines from ${open.first}`);
  check("it opens at the bottom", open.fromEnd <= 2, `${open.fromEnd}px from the end`);

  await page.evaluate(() => { document.querySelector(".team-stage-log").scrollTop = 0; });
  const top = await state();
  await page.click("[data-action='team.group-older']");
  await page.waitForFunction(() => document.querySelectorAll(".team-stage-log .team-line").length === 400);
  const paged = await page.evaluate((n) => {
    const log = document.querySelector(".team-stage-log");
    const line = [...log.querySelectorAll(".team-line")].find((l) => l.textContent.includes(`第 ${n} 行`));
    return line.getBoundingClientRect().top;
  }, top.seen);
  check("a page paged in above keeps the lines on screen where they were", Math.abs(paged - top.seenTop) <= 2, `line ${top.seen} moved ${(paged - top.seenTop).toFixed(1)}px`);

  const reading = await state();
  await page.evaluate(() => window.__team.addLine("g-3", { from: "a-1", kind: "post", text: "第 2001 行：读着旧消息时到的" }));
  await page.waitForFunction(() => [...document.querySelectorAll(".team-stage-log .team-line")].some((l) => l.textContent.includes("第 2001 行")));
  await page.waitForTimeout(200);
  const after = await state();
  check("a line arriving while older ones are read leaves the reader there", after.seen === reading.seen && Math.abs(after.seenTop - reading.seenTop) <= 2, `reading line ${reading.seen} at ${reading.seenTop.toFixed(0)}, now line ${after.seen} at ${after.seenTop.toFixed(0)}`);

  await page.evaluate(() => { const log = document.querySelector(".team-stage-log"); log.scrollTop = log.scrollHeight; });
  await page.waitForTimeout(100);
  await page.evaluate(() => window.__team.addLine("g-3", { from: "a-2", kind: "post", text: "第 2002 行：在底部时到的" }));
  await page.waitForFunction(() => [...document.querySelectorAll(".team-stage-log .team-line")].some((l) => l.textContent.includes("第 2002 行")));
  await page.waitForTimeout(300);
  check("a line arriving at the bottom is followed", (await state()).fromEnd <= 2, `${(await state()).fromEnd}px from the end`);

  for (let i = 0; i < 12 && (await state()).more; i++) {
    const before = (await state()).count;
    await page.click("[data-action='team.group-older']");
    await page.waitForFunction((n) => document.querySelectorAll(".team-stage-log .team-line").length > n, before);
  }
  const all = await state();
  check("paging reaches the first line and stops offering more", all.first === "1" && !all.more && all.count === 2002, `${all.count} lines from ${all.first}, more ${all.more}`);
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
