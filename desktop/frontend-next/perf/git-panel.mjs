// The Git panel in the workbench: a workspace that is one repository shows the changes as a folder tree to stage,
// discard and commit, a push that is previewed before it is confirmed, and a history whose commits open into their
// files; a folder of projects shows one section per repository, each acting on its own.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const shotDir = process.env.SHOTS;
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    for (const width of [1400, 700]) {
      const tag = `${scheme} ${width}px`;
      const open = async (query) => {
        const page = await browser.newPage({ colorScheme: scheme, viewport: { width, height: 1000 }, locale: "zh-CN", reducedMotion: "reduce" });
        page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
        await page.goto(`${BASE}?pref=zh&ws=2&sess=3&turns=2${query}`, { waitUntil: "networkidle" });
        await page.waitForSelector(".app");
        await page.locator('.navbtn[aria-label="文件"]').first().click();
        await page.waitForSelector(".workbench-changes");
        return page;
      };
      const shot = async (page, name) => {
        if (!shotDir) return;
        await page.mouse.move(width - 3, 995);
        await page.waitForTimeout(250);
        const box = await page.locator(".workbench-explorer").first().boundingBox();
        // At phone width the composer's toolbar is drawn over the foot of the column; the panel's own picture stops above it.
        await page.screenshot({ path: `${shotDir}/${scheme}-${width}-${name}.png`, clip: { x: box.x, y: box.y, width: box.width, height: box.height - (width <= 700 ? 150 : 0) } });
      };

      const one = await open("");
      const tree = await one.evaluate(() => ({
        folders: [...document.querySelectorAll(".workbench-change-folder")].length,
        rows: [...document.querySelectorAll('.workbench-change-row[data-action-click="workbench.diff"]')].length,
        overflow: document.querySelector(".workbench-explorer").scrollWidth > document.querySelector(".workbench-explorer").clientWidth + 1,
      }));
      check(`${tag}: the changes are a folder tree`, tree.folders >= 2 && tree.rows === 4, JSON.stringify(tree));
      check(`${tag}: the panel does not push sideways`, !tree.overflow);
      await shot(one, "changes");
      await one.locator('[data-action="workbench.discard"]').first().focus();
      await one.locator('[data-action="workbench.discard"]').first().click();
      await one.waitForSelector(".workbench-discard-ask");
      const ask = await one.evaluate(() => document.querySelector(".workbench-discard-ask").textContent);
      check(`${tag}: a discard asks first and says it cannot be undone`, ask.includes("无法恢复"));
      await shot(one, "discard");
      await one.getByRole("button", { name: "取消", exact: true }).first().click();
      await one.getByRole("button", { name: "推送…" }).click();
      await one.waitForSelector(".push-card");
      const push = await one.evaluate(() => document.querySelector(".push-card").textContent);
      check(`${tag}: the push says where it goes before it goes`, push.includes("github.com") && push.includes("main → origin/main") && push.includes("从不强制推送"), push.slice(0, 120));
      await shot(one, "push");
      await one.getByRole("button", { name: "推送到 github.com", exact: false }).last().click();
      await one.waitForSelector('.push-card[data-phase="done"]');
      check(`${tag}: a confirmed push reports it`, (await one.locator(".push-card").textContent()).includes("已把 2 个提交推送到 github.com"));
      await shot(one, "pushed");
      await one.getByText("提交历史").click();
      await one.getByRole("button", { name: /show the commit graph/ }).click();
      await one.getByRole("button", { name: /retry\.go/ }).last().click();
      await one.waitForSelector(".gh-diff");
      await shot(one, "history");
      await one.close();

      const many = await open("&git=multi");
      const heads = await many.evaluate(() => [...document.querySelectorAll(".workbench-repo-head")].map((h) => h.textContent));
      check(`${tag}: a folder of projects shows a section per repository`, heads.length === 3 && heads[0].includes("api") && heads[1].includes("feature/login"), heads.join(" | "));
      await shot(many, "repos");
      await many.locator('.workbench-repo[data-repo="web"] [data-action="workbench.stage"]').first().click();
      await many.waitForFunction(() => document.querySelector('.workbench-repo[data-repo="web"] [data-action="workbench.stage"]')?.getAttribute("aria-checked") === "true");
      check(`${tag}: staging in one repository leaves the others as they were`, (await many.locator('.workbench-repo[data-repo="api"] [data-action="workbench.stage"][aria-checked="true"]').count()) === 1);
      await many.close();
    }
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
console.log("\nall passed");
