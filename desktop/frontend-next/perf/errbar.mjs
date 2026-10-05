// The window's error bar floats above the composer of the pane in front: it
// may not move the conversation, and it may not cover the send button.
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&turns=4";
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

const geometry = () =>
  page.evaluate(() => {
    const rect = (el) => el?.getBoundingClientRect() ?? null;
    const bar = document.querySelector(".errbar");
    const send = document.querySelector('[data-action="session.send"]');
    const at = (el) => {
      const b = rect(el);
      if (!b) return false;
      const hit = document.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2);
      return !!hit && (hit === el || el.contains(hit));
    };
    return { bar: rect(bar), compose: rect(document.querySelector(".compose")), sendHit: at(send), barHit: at(bar) };
  });

for (const { width, height } of [
  { width: 1440, height: 900 },
  { width: 640, height: 900 },
]) {
  await page.setViewportSize({ width, height });
  await page.waitForTimeout(300);
  // Send is only live with something to send, which is when covering it counts.
  await page.fill('textarea[role="combobox"]', "继续检查");
  const before = await geometry();
  await page.evaluate(() => window.__refuse("open", "基准拒绝：新建会话"));
  // A new session in the open pane's own folder only focuses that blank pane,
  // so the request that fails is the first one asked of another folder.
  const asks = page.locator('[data-action="session.new"]');
  for (let i = 0; i < (await asks.count()) && (await page.locator(".errbar").count()) === 0; i++) {
    await asks.nth(i).dispatchEvent("click");
    await page.waitForTimeout(150);
  }
  await page.waitForSelector(".errbar");
  await page.waitForTimeout(250);
  const after = await geometry();
  const where = `${width}×${height}`;
  check(`${where}：报错条出现且可点`, after.bar !== null && after.barHit);
  check(`${where}：报错条在编辑器上沿之上`, after.bar && after.bar.bottom <= after.compose.top + 1, `bar.bottom=${Math.round(after.bar?.bottom)} compose.top=${Math.round(after.compose.top)}`);
  check(`${where}：发送按钮没被盖住`, before.sendHit && after.sendHit);
  check(`${where}：编辑器没被顶动`, Math.abs(after.compose.top - before.compose.top) < 1, `${Math.round(before.compose.top)} → ${Math.round(after.compose.top)}`);
  await page.locator(".errbar button").click();
  await page.waitForSelector(".errbar", { state: "detached" });
}

await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
