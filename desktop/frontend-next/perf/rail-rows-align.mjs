// With the icon rail hidden, the sidebar's rows line up and read alike: every row's icon sits on one vertical line and every
// label starts on another, from the new-session button down to the wallet and feedback rows at the foot.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=1&sess=1&turns=1";
const ROWS = [
  ["新建会话", ".studio-new-task"],
  ["搜索与快捷操作", ".studio-search"],
  ["文件", ".studio-quicknav > button:nth-child(1)"],
  ["工具与权限", ".studio-quicknav > button:nth-child(2)"],
  ["扩展", ".studio-quicknav > button:nth-child(3)"],
  ["记忆", ".studio-quicknav > button:nth-child(4)"],
  ["远程", ".studio-quicknav > button:nth-child(5)"],
  ["钱包与用量", ".studio-wallet:not(.studio-feedback)"],
  ["发送反馈", ".studio-feedback"],
];
const TOLERANCE = 1;
const spread0 = (rows, at) => rows.reduce((worst, r) => (Math.abs(r.icon - at) > Math.abs(worst) ? r.icon - at : worst), 0);
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    const ctx = await browser.newContext({ locale: "zh-CN", colorScheme: scheme, viewport: { width: 1280, height: 900 }, deviceScaleFactor: 4, reducedMotion: "reduce" });
    await ctx.addInitScript(() => { try { localStorage.setItem("rx-nav-rail", "off"); } catch {} });
    const page = await ctx.newPage();
    await page.goto(BASE, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    const seen = [];
    for (const [name, sel] of ROWS) {
      const at = await page.evaluate((s) => {
        const row = document.querySelector(s);
        if (!row) return null;
        const svg = row.querySelector("svg").getBoundingClientRect();
        const walk = document.createTreeWalker(row, NodeFilter.SHOW_TEXT);
        let text = null;
        for (let n = walk.nextNode(); n && !text; n = walk.nextNode()) {
          if (!n.textContent.trim()) continue;
          const range = document.createRange();
          range.selectNodeContents(n);
          text = range.getBoundingClientRect().x;
        }
        const label = [...row.querySelectorAll("*")].find((e) => e.children.length === 0 && e.textContent.trim() && !e.closest("kbd,small,.fbk-badge")) ?? row;
        const cs = getComputedStyle(label);
        return { icon: svg.x + svg.width / 2, text, look: `${cs.fontSize}/${cs.fontWeight}/${cs.color}`, fill: getComputedStyle(row).backgroundColor };
      }, sel);
      if (at) seen.push({ name, ...at });
    }
    const spread = (key) => Math.max(...seen.map((r) => r[key])) - Math.min(...seen.map((r) => r[key]));
    const show = (key) => seen.map((r) => `${r.name} ${r[key].toFixed(1)}`).join("; ");
    check(`${scheme}: all ${ROWS.length} rows are drawn`, seen.length === ROWS.length, `${seen.length}`);
    check(`${scheme}: the icons share one vertical line`, spread("icon") <= TOLERANCE, `spread ${spread("icon").toFixed(1)}px — ${show("icon")}`);
    check(`${scheme}: the labels start on one vertical line`, spread("text") <= TOLERANCE, `spread ${spread("text").toFixed(1)}px — ${show("text")}`);
    const folder = await page.evaluate(() => {
      const r = document.querySelector(".rail .wsfolder")?.getBoundingClientRect();
      return r ? r.x + r.width / 2 : null;
    });
    check(`${scheme}: the icons sit on the project folders' line`, folder !== null && Math.abs(spread0(seen, folder)) <= TOLERANCE, `folders ${folder}; rows ${show("icon")}`);
    const lockup = await page.evaluate(() => {
      const logo = document.querySelector(".studio-brand-logo").getBoundingClientRect();
      const first = document.querySelector(".studio-new-task").getBoundingClientRect();
      const rail = document.querySelector(".rail").getBoundingClientRect();
      return { above: logo.top - rail.top, below: first.top - logo.bottom };
    });
    check(`${scheme}: the logo has the same space above and below it`, Math.abs(lockup.above - lockup.below) <= TOLERANCE, `above ${lockup.above}, below ${lockup.below}`);
    const logo = await page.evaluate(() => {
      const r = document.querySelector(".studio-brand-logo").getBoundingClientRect();
      return { x: r.x, y: r.y, h: r.height };
    });
    const shot = await page.screenshot({ clip: { x: 0, y: logo.y, width: 120, height: logo.h } });
    const inkLeft = await page.evaluate(async (b64) => {
      const img = new Image();
      img.src = `data:image/png;base64,${b64}`;
      await img.decode();
      const c = document.createElement("canvas");
      c.width = img.width; c.height = img.height;
      const g = c.getContext("2d");
      g.drawImage(img, 0, 0);
      const d = g.getImageData(0, 0, c.width, c.height).data;
      const bg = [d[0], d[1], d[2]];
      for (let x = 0; x < c.width; x++) for (let y = 0; y < c.height; y++) {
        const i = (y * c.width + x) * 4;
        if (Math.abs(d[i] - bg[0]) + Math.abs(d[i + 1] - bg[1]) + Math.abs(d[i + 2] - bg[2]) > 60) return x / (c.width / 120);
      }
      return null;
    }, shot.toString("base64"));
    const labelLeft = seen[0].text;
    check(`${scheme}: the logo's first letter starts where the row labels start`, inkLeft !== null && Math.abs(inkLeft - labelLeft) <= TOLERANCE, `logo ink ${inkLeft}, labels ${labelLeft}`);
    const avatar = await page.evaluate(() => {
      const ini = document.querySelector(".studio-user-foot .ini");
      const row = ini?.closest(".acctrow");
      if (!ini || !row) return null;
      const a = ini.getBoundingClientRect();
      const r = row.getBoundingClientRect();
      return { same: Math.abs(a.width - r.width) <= 1 && Math.abs(a.height - r.height) <= 1, radius: getComputedStyle(ini).borderRadius, rowRadius: getComputedStyle(row).borderRadius };
    });
    check(`${scheme}: the account avatar is one rounded square, not a circle inside another shape`, avatar !== null && avatar.same && avatar.radius === avatar.rowRadius, JSON.stringify(avatar));
    const tabGapsAt = async () => {
      const boxes = await page.evaluate(() => [...document.querySelectorAll(".studio-session-segments button")].map((e) => {
        const r = e.getBoundingClientRect();
        return { x: r.x, y: r.y, width: r.width, height: r.height };
      }));
      const gaps = [];
      for (const box of boxes) {
        const shot = await page.screenshot({ clip: box });
        gaps.push(await page.evaluate(async (b64) => {
          const img = new Image();
          img.src = `data:image/png;base64,${b64}`;
          await img.decode();
          const c = document.createElement("canvas");
          c.width = img.width; c.height = img.height;
          const g = c.getContext("2d");
          g.drawImage(img, 0, 0);
          const d = g.getImageData(0, 0, c.width, c.height).data;
          const mid = (c.width >> 1) * 4;
          const bg = [d[mid], d[mid + 1], d[mid + 2]];
          let top = -1, bottom = -1;
          for (let y = 0; y < c.height; y++) {
            for (let x = Math.floor(c.width * 0.2); x < Math.floor(c.width * 0.8); x++) {
              const i = (y * c.width + x) * 4;
              if (Math.abs(d[i] - bg[0]) + Math.abs(d[i + 1] - bg[1]) + Math.abs(d[i + 2] - bg[2]) > 90) { if (top < 0) top = y; bottom = y; break; }
            }
          }
          return { above: top / 4, below: (c.height - 1 - bottom) / 4 };
        }, shot.toString("base64")));
      }
      return gaps.reduce((w, g) => (Math.abs(g.above - g.below) > Math.abs(w.above - w.below) ? g : w), gaps[0]);
    };
    const inkExtent = async () => {
      const box = await page.evaluate(() => {
        const r = document.querySelector(".studio-session-segments button").getBoundingClientRect();
        return { x: r.x, y: r.y, width: r.width, height: r.height };
      });
      const shot = await page.screenshot({ clip: box });
      return page.evaluate(async (b64) => {
        const img = new Image();
        img.src = `data:image/png;base64,${b64}`;
        await img.decode();
        const c = document.createElement("canvas");
        c.width = img.width; c.height = img.height;
        const g = c.getContext("2d");
        g.drawImage(img, 0, 0);
        const d = g.getImageData(0, 0, c.width, c.height).data;
        const mid = (c.width >> 1) * 4;
        const bg = [d[mid], d[mid + 1], d[mid + 2]];
        let top = -1, bottom = -1;
        for (let y = 0; y < c.height; y++) {
          for (let x = Math.floor(c.width * 0.2); x < Math.floor(c.width * 0.8); x++) {
            const i = (y * c.width + x) * 4;
            if (Math.abs(d[i] - bg[0]) + Math.abs(d[i + 1] - bg[1]) + Math.abs(d[i + 2] - bg[2]) > 90) { if (top < 0) top = y; bottom = y; break; }
          }
        }
        return { top: top / 4, bottom: bottom / 4 };
      }, shot.toString("base64"));
    };
    const clipped = await inkExtent();
    await page.addStyleTag({ content: ".studio-session-segments button > span { overflow: visible !important; }" });
    const open = await inkExtent();
    check(`${scheme}: the session tabs' glyphs are not cut by the label's clip box`, Math.abs(clipped.top - open.top) <= 0.25 && Math.abs(clipped.bottom - open.bottom) <= 0.25, `clipped ${JSON.stringify(clipped)} open ${JSON.stringify(open)}`);
    await page.evaluate(() => { document.head.lastElementChild.remove(); });
    for (const scale of [1]) {
      await page.evaluate((f) => document.documentElement.style.setProperty("--fs-scale", String(f)), scale);
      await page.waitForTimeout(120);
      const worst = await tabGapsAt();
      check(`${scheme}: at text size ${scale} each session tab's text has equal space above and below`, Math.abs(worst.above - worst.below) <= 1.5, JSON.stringify(worst));
    }
    await page.evaluate(() => document.documentElement.style.removeProperty("--fs-scale"));
    const pillY = await page.evaluate(() => document.querySelector(".studio-session-segments").getBoundingClientRect().y);
    check(`${scheme}: the session pill sits on a whole pixel, so its fill has equal edges above and below`, Number.isInteger(pillY), `y ${pillY}`);
    const looks = new Set(seen.map((r) => r.look));
    check(`${scheme}: every row's label has one size, weight and colour`, looks.size === 1, seen.map((r) => `${r.name} ${r.look}`).join("; "));
    const fill = seen.find((r) => r.name === "新建会话")?.fill ?? "";
    check(`${scheme}: New session has no fill at rest`, /rgba\(0, 0, 0, 0\)|transparent/.test(fill), fill);
    await ctx.close();
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
