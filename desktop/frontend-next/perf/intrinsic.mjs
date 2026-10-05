// The transcript skips layout for off-screen cards with content-visibility,
// and each one holds an estimated height until it is first laid out. The
// estimate need not be accurate, but its error must fall on both sides: below
// the shortest card every unread card is underestimated, the error accumulates
// in one direction, and the scroller lands short and then corrects — which is
// the lurch. This measures the real distribution and checks the stylesheet's
// number against it.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const HERE = dirname(fileURLToPath(import.meta.url));
const SRC = process.env.PERF_SRC ?? join(HERE, "..", "src");
const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?ws=1&sess=1&turns=40&pref=zh";

const css = readFileSync(join(SRC, "styles", "app.css"), "utf8");
const declared = Number(css.match(/contain-intrinsic-size:\s*auto\s+(\d+(?:\.\d+)?)px/)?.[1] ?? NaN);
if (!Number.isFinite(declared)) {
  console.log("未在样式表中找到 contain-intrinsic-size 的兜底值：该检查将始终通过。");
  process.exit(1);
}

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: "dark" });
await page.goto(PAGE, { waitUntil: "networkidle" });
await page.waitForSelector(".compose");
await page.waitForTimeout(1000);

// Walk the whole transcript so every card lays out at least once: only
// then is the distribution the one a reader meets.
const stats = await page.evaluate(async () => {
  const sc = [...document.querySelectorAll("*")]
    .filter((el) => {
      const s = getComputedStyle(el);
      return /auto|scroll/.test(s.overflowY) && el.scrollHeight > el.clientHeight + 4 && el.querySelector(".call");
    })
    .pop();
  if (sc) {
    for (let y = 0; y <= sc.scrollHeight; y += 400) {
      sc.scrollTop = y;
      await new Promise((r) => requestAnimationFrame(r));
    }
  }
  const hs = [...document.querySelectorAll(".chunk > .enterbox > .call")]
    .map((c) => c.offsetHeight)
    .filter((x) => x > 0)
    .sort((a, b) => a - b);
  if (!hs.length) return null;
  const q = (p) => hs[Math.floor((hs.length - 1) * p)];
  return {
    n: hs.length,
    min: hs[0],
    median: q(0.5),
    mean: Math.round(hs.reduce((a, b) => a + b, 0) / hs.length),
    max: hs[hs.length - 1],
  };
});

if (!stats) {
  console.log("未量到卡片：该检查将始终通过，请先确认页面存在转录内容。");
  process.exit(1);
}

console.log(`样式表兜底值 ${declared}px   实测 n=${stats.n} 最小 ${stats.min} 中位数 ${stats.median} 均值 ${stats.mean} 最大 ${stats.max}`);
check(
  "占位估算不低于最小卡片高度",
  declared >= stats.min,
  declared < stats.min ? `${declared} < ${stats.min}：每张未读卡片均被低估` : "",
);
check(
  "占位估算处于实测分布区间内",
  declared <= stats.max,
  declared > stats.max ? `${declared} > ${stats.max}：误差将反向累加` : "",
);

await browser.close();
console.log(fails.length ? `\n${fails.length} 项未通过` : "\n全部通过");
process.exit(fails.length ? 1 : 0);
