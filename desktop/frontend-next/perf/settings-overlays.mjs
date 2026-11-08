import assert from "node:assert/strict";
import { chromium } from "playwright";

const url = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&turns=2";
const browser = await chromium.launch();
const failures = [];
const check = async (name, run) => {
  try { await run(); console.log(`PASS ${name}`); }
  catch (error) { failures.push(`${name}: ${error.message}`); }
};

async function contained(page, selector) {
  const boxes = await page.locator(selector).evaluateAll((elements) => elements.map((el) => {
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, right: r.right, bottom: r.bottom, width: r.width, height: r.height, vw: innerWidth, vh: innerHeight };
  }));
  assert.ok(boxes.length > 0, `${selector} exists`);
  for (const r of boxes) {
    assert.ok(r.width > 0 && r.height > 0, `${selector} is visible`);
    assert.ok(r.x >= 0 && r.right <= r.vw + 1, `${selector} fits horizontally: ${JSON.stringify(r)}`);
    assert.ok(r.y >= 0 && r.bottom <= r.vh + 1, `${selector} fits vertically: ${JSON.stringify(r)}`);
  }
}

async function header(page) {
  await contained(page, '.prefs-sheet');
  await contained(page, '.prefs-actions button');
  assert.equal(await page.locator('.prefs-sheet').evaluate(el => el.scrollTop), 0, "focus only scrolls the page body");
}

async function scopeRows(page) {
  const rows = await page.locator('.scope-pick .pk:has(.ct)').evaluateAll(elements => elements.map(el => {
    const name = el.querySelector('.nm').getBoundingClientRect();
    const path = el.querySelector('.pt').getBoundingClientRect();
    const badge = el.querySelector('.ct').getBoundingClientRect();
    return { name: { left: name.left, right: name.right, bottom: name.bottom }, path: { left: path.left, top: path.top }, badge: { left: badge.left } };
  }));
  assert.ok(rows.length > 0);
  for (const row of rows) {
    assert.ok(row.name.right <= row.badge.left && Math.abs(row.path.left - row.name.left) < 1 && row.path.top >= row.name.bottom, JSON.stringify(row));
  }
}

async function open(page, name) {
  const drawer = page.getByRole("button", { name: "会话列表", exact: true });
  const expand = page.getByRole("button", { name: "展开工作区栏", exact: true });
  if (await drawer.isVisible() && await drawer.getAttribute("aria-pressed") !== "true") await drawer.click();
  else if (await expand.isVisible()) await expand.click();
  await page.getByRole("toolbar", { name: "侧栏入口" }).getByRole("button", { name }).click();
  await page.locator('.prefs-sheet').waitFor();
}

async function deep(page, section, id) {
  await page.getByRole("tab", { name: new RegExp(`^${section}`) }).click();
  await page.locator(`.prefs-main [data-action="settings.open-page"][data-target="${id}"]`).first().click();
  await page.locator(`.deep-page[data-setting="${id}"]`).waitFor();
}

try {
  for (const [width, height] of [[320, 740], [390, 844], [430, 932], [782, 390], [1044, 884], [1440, 900]]) {
    const page = await browser.newPage({ viewport: { width, height }, reducedMotion: "reduce" });
    page.on("pageerror", error => failures.push(error.message));
    await page.goto(url, { waitUntil: "networkidle" });
    const size = `${width}x${height}`;
    await open(page, /^扩展$/);
    await page.getByRole("button", { name: "Reasonix", exact: true }).click();
    await check(`${size}: standalone scope picker`, () => contained(page, '.scope-pick'));
    await check(`${size}: scope name, path and exception columns`, () => scopeRows(page));
    await page.getByRole("button", { name: "Reasonix", exact: true }).click();
    await page.getByRole("button", { name: "添加…", exact: true }).click();
    await check(`${size}: add menu`, () => contained(page, '.ext-add-menu'));
    await page.getByRole("menuitem", { name: /接入外部服务/ }).click();
    await page.getByRole("textbox", { name: "一段 JSON、一行命令，或一个 https 地址" }).fill('https://mcp.example.com/sse');
    await page.getByRole("button", { name: "查看内容", exact: true }).click();
    await page.locator('.addsrv[data-stage="confirm"]').waitFor();
    await check(`${size}: header after focusing MCP form`, () => header(page));
    await page.getByRole("button", { name: "返回", exact: true }).click();
    await page.getByRole("button", { name: "取消", exact: true }).click();
    await page.getByRole("tab", { name: "发现", exact: true }).click();
    await page.getByRole("button", { name: /^make-ui-not-ai @/ }).click();
    await check(`${size}: long marketplace source`, async () => {
      const r = await page.locator('.mkt-detail-meta .mono').evaluate(el => {
        const r = el.getBoundingClientRect(), parent = el.parentElement.getBoundingClientRect();
        return { right: r.right, parent: parent.right, scroll: el.scrollWidth, client: el.clientWidth };
      });
      assert.ok(r.right <= r.parent + 1 && r.scroll <= r.client + 1, JSON.stringify(r));
    });
    await page.getByRole("button", { name: "关闭 Esc", exact: true }).click();
    await open(page, /^设置$/);
    await page.getByRole("tab", { name: /^权限与安全/ }).click();
    await page.getByRole("button", { name: "Reasonix", exact: true }).click();
    await check(`${size}: settings scope picker`, () => contained(page, '.scope-pick'));
    await page.getByRole("button", { name: "Reasonix", exact: true }).click();
    await deep(page, "Agent 行为", "agent-presets");
    await page.getByRole("button", { name: "新建预设", exact: true }).click();
    await page.getByRole("textbox", { name: "预设名称", exact: true }).fill('Layout audit draft');
    await page.getByRole("button", { name: "返回预设列表", exact: true }).click();
    await check(`${size}: discard dialog`, () => contained(page, '.preset-confirm'));
    await page.getByRole("button", { name: "继续编辑", exact: true }).click();
    assert.equal(await page.getByRole("textbox", { name: "预设名称", exact: true }).inputValue(), 'Layout audit draft');
    await page.getByRole("button", { name: "返回预设列表", exact: true }).click();
    await page.getByRole("button", { name: "放弃更改", exact: true }).click();
    await deep(page, "数据", "storage");
    await page.getByRole("button", { name: "选择会话文件…", exact: true }).click();
    await check(`${size}: import dialog`, () => contained(page, '.ns-dlg'));
    await page.getByRole("dialog", { name: "导入原生会话", exact: true }).getByRole("button", { name: "取消", exact: true }).click();
    await check(`${size}: header after modal dismissal`, () => header(page));
    const signedIn = new URL(url);
    signedIn.searchParams.set("signedin", "1");
    await page.goto(signedIn.href, { waitUntil: "networkidle" });
    await open(page, /^设置$/);
    await deep(page, "数据", "backup");
    await page.getByRole("textbox", { name: "备注（可选）", exact: true }).fill('Layout audit backup');
    await page.getByRole("textbox", { name: "加密口令（至少 10 个字符）", exact: true }).fill('layout-audit-demo');
    await page.getByRole("textbox", { name: "再输一次", exact: true }).fill('layout-audit-demo');
    await page.getByRole("button", { name: "备份到账号", exact: true }).click();
    await page.getByRole("button", { name: "恢复…", exact: true }).click();
    await check(`${size}: backup passphrase dialog`, () => contained(page, '.bk-dlg'));
    await page.getByRole("textbox", { name: "加密口令", exact: true }).fill('layout-audit-demo');
    await page.getByRole("button", { name: "预览差异", exact: true }).click();
    await page.getByRole("button", { name: "恢复所选 1 项", exact: true }).waitFor();
    await check(`${size}: backup difference preview`, async () => {
      await contained(page, '.bk-dlg');
      assert.ok(await page.locator('.bk-dlg').evaluate(el => el.scrollWidth <= el.clientWidth + 1));
    });
    await page.getByRole("dialog", { name: "恢复备份", exact: true }).getByRole("button", { name: "取消", exact: true }).click();
    await page.close();
  }
} finally {
  await browser.close();
}
assert.deepEqual(failures, [], failures.join('\n'));
