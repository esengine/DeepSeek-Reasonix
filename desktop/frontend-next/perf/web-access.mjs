// Web search and fetch are set from the settings page: each kind is an ordered
// list with the paid backend last, consent to be billed is its own switch that
// is never ticked for the person and is inert without a DeepSeek key, and
// nothing spills at a phone's width.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};
const shotDir = process.env.SHOTS;

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  for (const [width, ready] of [[1280, "ready"], [420, "ready"], [1280, "noready"]]) {
    const tag = `${scheme} ${width}px${ready === "noready" ? " without a key" : ""}`;
    const page = await browser.newPage({ viewport: { width, height: 900 }, locale: "zh-CN", colorScheme: scheme, reducedMotion: "reduce" });
    page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
    await page.goto(`${BASE}?pref=zh&turns=2&webaccess=${ready}`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
    await page.waitForSelector(".prefs-nav button");
    await page.evaluate(() => [...document.querySelectorAll(".prefs-nav button")].find((b) => b.textContent.includes("工具与权限"))?.click());
    await page.evaluate(() => document.getElementById("prefs-tools-web")?.click());
    await page.waitForSelector("#set-web-access");
    const r = await page.evaluate(() => {
      const g = document.querySelector("#set-web-access");
      const box = g.querySelector(".box").getBoundingClientRect();
      const lists = [...g.querySelectorAll(".wa-sec")].map((s) => [s.getAttribute("aria-label"), [...s.querySelectorAll(".wa-list > li > .tx .lb")].map((e) => e.textContent)]);
      const consent = [...g.querySelectorAll('[role="switch"]')].filter((b) => /同意/.test(b.getAttribute("aria-label")));
      const wide = [...g.querySelectorAll(".box *")].filter((e) => e.getBoundingClientRect().right > box.right + 1).map((e) => e.className || e.tagName);
      return {
        lists,
        video: g.querySelector('[data-action="web-access.video-url"]')?.getAttribute("aria-checked"),
        consent: consent.map((b) => [b.getAttribute("aria-checked"), b.disabled]),
        hint: g.textContent.includes("需要一个带密钥的 DeepSeek 提供方"),
        applies: g.textContent.includes("之后新开的窗格"),
        path: g.textContent.includes("/Users/you/.reasonix/config.toml"),
        wide: wide.slice(0, 3),
        scroll: g.querySelector(".box").scrollWidth - g.querySelector(".box").clientWidth,
      };
    });
    check(`${tag}: search and fetch are two ordered lists, the paid backend after the free one`, r.lists.length === 3 && r.lists[0][1][0] === "SearXNG" && r.lists[0][1][1] === "DeepSeek 官方搜索" && r.lists[1][1][0] === "本机抓取" && r.lists[1][1][1] === "DeepSeek 官方抓取", JSON.stringify(r.lists));
    check(`${tag}: the video address list is its own third list with one switch, off until the person turns it on`, r.lists[2]?.[0] === "视频地址" && r.lists[2][1].length === 1 && r.lists[2][1][0] === "把视频地址交给模型厂商" && r.video === "false", JSON.stringify([r.lists[2], r.video]));
    check(`${tag}: no consent is ticked for the person`, r.consent.length === 2 && r.consent.every(([on]) => on === "false"), JSON.stringify(r.consent));
    check(`${tag}: the consent is ${ready === "noready" ? "inert and says why" : "live"}`, ready === "noready" ? r.consent.every(([, off]) => off) && r.hint : r.consent.every(([, off]) => !off) && !r.hint);
    check(`${tag}: it says when a change applies and where it is kept`, r.applies && r.path);
    check(`${tag}: nothing spills out of the box`, r.wide.length === 0 && r.scroll <= 1, `${r.wide.join(",")} ${r.scroll}px`);
    if (shotDir && width === 1280 && ready === "ready") {
      await page.addStyleTag({ content: ".cfgbad { display: none !important; }" });
      await page.setViewportSize({ width, height: 1200 });
      await page.waitForTimeout(250);
      await page.locator("#set-web-access").screenshot({ path: `${shotDir}/${scheme}-web.png` });
    }
    await page.close();
  }
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
