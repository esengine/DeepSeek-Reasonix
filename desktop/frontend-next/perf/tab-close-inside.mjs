// A pane tab's close button has to stay inside its own tab whatever the tab
// carries: past the tab's edge it sits on the next tab, and a press closes
// nothing. The folder and host labels share the row with the title, so a long
// one is what pushes it out. Only the browser lays this out.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const LONG = "一个很长很长很长很长很长很长很长很长很长的会话标题";
const cases = [
  ["a long title", { title: LONG }],
  ["a long title in a long folder", { title: LONG, ws: "streamhub-02-pipelines-smc-workspace" }],
  ["a long folder and a long host", { title: LONG, ws: "streamhub-02-pipelines-smc-workspace", host: "build-runner-eu-west-long-hostname" }],
];

const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1200, height: 760 } });
  await page.goto(BASE, { waitUntil: "networkidle" });
  await page.waitForSelector(".app");
  for (const [label, tab] of cases) {
    const got = await page.evaluate(({ title, ws, host }) => {
      document.querySelector(".probe-tabs")?.remove();
      const bar = document.createElement("div");
      bar.className = "panetabs probe-tabs";
      bar.style.cssText = "position:fixed;left:0;top:0;right:0;z-index:2147483647";
      const one = (t) => `<div class="ptab" aria-selected="${t === title}"><i class="pip"></i><span class="ptab-nm">${t}</span>`
        + (ws ? `<span class="ptab-ws">${ws}</span>` : "") + (host ? `<span class="ptab-host">${host}</span>` : "")
        + `<button class="ptab-x">×</button></div>`;
      bar.innerHTML = one(title) + one("hi");
      document.body.appendChild(bar);
      const first = bar.querySelector(".ptab");
      const box = first.getBoundingClientRect();
      const x = first.querySelector(".ptab-x").getBoundingClientRect();
      const hit = document.elementFromPoint(x.left + x.width / 2, x.top + x.height / 2);
      return {
        inside: x.left >= box.left - 0.5 && x.right <= box.right + 0.5,
        reached: hit === first.querySelector(".ptab-x"),
        title: Math.round(first.querySelector(".ptab-nm").getBoundingClientRect().width),
        detail: `tab ${Math.round(box.left)}–${Math.round(box.right)}, close ${Math.round(x.left)}–${Math.round(x.right)}`,
      };
    }, tab);
    check(`${label}: the close button stays inside its tab`, got.inside, got.detail);
    check(`${label}: a press on the close button reaches it`, got.reached);
    check(`${label}: the title keeps room to be read`, got.title >= 48, `${got.title}px`);
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
console.log("\nall passed");
