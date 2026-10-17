import assert from "node:assert/strict";
import fs from "node:fs";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const require = createRequire(import.meta.url);
const { _electron } = require(path.join(root, "desktop/frontend-next/node_modules/playwright"));
const executablePath = process.env.REASONIX_TEST_ELECTRON || require("electron");
const home = fs.mkdtempSync(path.join(os.tmpdir(), "reasonix-attachments-live-"));
const workspace = path.join(home, "workspace");
const nativeBytes = 111 * 1024 * 1024;
const streamBytes = 65 * 1024 * 1024;
const artifacts = process.env.REASONIX_ATTACHMENT_ARTIFACTS;
let app;
let passed = false;

try {
  fs.mkdirSync(workspace);
  fs.writeFileSync(path.join(home, "config.toml"), [
    'default_model = "smoke/smoke-model"', "[desktop]", "welcomed = true",
    "[[providers]]", 'name = "smoke"', 'kind = "openai"',
    'base_url = "http://127.0.0.1:9/v1"', 'models = ["smoke-model"]',
    'default = "smoke-model"', 'api_key_env = "SMOKE_API_KEY"', "",
  ].join("\n"));
  fs.writeFileSync(path.join(home, ".env"), "SMOKE_API_KEY=fixture\n");
  const apk = path.join(home, "large.apk");
  const file = fs.openSync(apk, "w");
  fs.ftruncateSync(file, nativeBytes);
  fs.closeSync(file);
  app = await _electron.launch({
    executablePath,
    cwd: workspace,
    args: [path.join(root, "desktop/electron"), "--user-data-dir=" + path.join(home, "electron-profile")],
    env: {
      ...process.env, REASONIX_HOME: home, REASONIX_STATE_HOME: home,
      REASONIX_CACHE_HOME: path.join(home, "cache"),
      REASONIX_STUDIO_HOST: process.env.REASONIX_STUDIO_HOST || path.join(root, "desktop/electron/bin/reasonix-studio-host" + (process.platform === "win32" ? ".exe" : "")),
      REASONIX_STUDIO_PAGE: path.join(root, "desktop/frontend-next/dist"),
    },
  });
  let page;
  for (let i = 0; i < 200; i++) {
    page = app.windows().find(candidate => candidate.url().startsWith("http://127.0.0.1:"));
    if (page) break;
    await new Promise(resolve => setTimeout(resolve, 200));
  }
  assert.ok(page, "shell must open its loopback page");
  await page.waitForLoadState("domcontentloaded");
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.locator("#boot").waitFor({ state: "hidden", timeout: 60000 });
  const status = await page.evaluate(async root => {
    const response = await fetch("/runtimes", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ root }) });
    return response.status;
  }, workspace);
  assert.equal(status, 200);
  const project = page.locator(".wsrow").filter({ has: page.locator(".wsname").filter({ hasText: /^workspace$/ }) });
  await project.locator("button.wsadd").click();
  // A blank pane in this workspace may be reused without creating a runtime.
  await page.waitForFunction(() => document.querySelector('.pane[data-active] input[type=file][data-action-change="session.attach"]') && !document.querySelector('.panes[aria-busy="true"]'));
  const traffic = [];
  page.on("request", request => {
    if (/\/(drop|attachments)/.test(request.url())) traffic.push({ method: request.method(), url: request.url(), contentType: request.headers()["content-type"] });
  });
  const ready = count => page.waitForFunction(n => document.querySelectorAll('.pane[data-active] .shots .shot[data-state="ready"]').length >= n, count);
  await page.locator('.pane[data-active] input[type=file][data-action-change="session.attach"]').setInputFiles(apk);
  await ready(1);
  assert.equal(traffic.length, 1);
  assert.ok(traffic[0].url.endsWith("/drop"));
  await page.evaluate(() => {
    const source = document.createElement("input"); source.type = "file"; source.id = "stream-source"; document.body.append(source);
  });
  await page.locator("#stream-source").setInputFiles(apk);
  for (const [index, kind] of ["paste", "drop"].entries()) {
    const answer = page.waitForResponse(response => response.url().endsWith("/drop"));
    await page.evaluate(kind => {
      const transfer = new DataTransfer(); transfer.items.add(document.querySelector("#stream-source").files[0]);
      const target = document.querySelector('.pane[data-active] textarea[data-action-paste="session.attach"]');
      const event = kind === "paste" ? new ClipboardEvent("paste", { clipboardData: transfer, bubbles: true }) : new DragEvent("drop", { dataTransfer: transfer, bubbles: true });
      target.dispatchEvent(event);
    }, kind);
    assert.equal((await answer).status(), 200);
    await ready(index + 2);
  }
  assert.equal(traffic.length, 3);
  assert.ok(traffic.every(request => request.url.endsWith("/drop")));
  assert.equal(new Set(traffic.map(request => request.url)).size, 1);
  const uploaded = page.waitForResponse(response => response.url().includes("/attachments/upload"));
  await page.evaluate(bytes => {
    const source = document.querySelector("#stream-source").files[0];
    const file = new File([source.slice(0, bytes)], "stream.apk", { type: "application/vnd.android.package-archive" });
    if (window.reasonixHost.pathForFile(file)) throw Error("virtual file unexpectedly has a native path");
    const transfer = new DataTransfer(); transfer.items.add(file);
    const input = document.querySelector('.pane[data-active] input[type=file][data-action-change="session.attach"]');
    input.files = transfer.files;
    input.dispatchEvent(new Event("change", { bubbles: true }));
  }, streamBytes);
  assert.equal((await uploaded).status(), 200);
  await ready(4);
  const saved = fs.readdirSync(path.join(workspace, ".reasonix/attachments")).find(file => file.endsWith(".apk"));
  assert.ok(saved, "streamed attachment must be saved");
  assert.equal(fs.statSync(path.join(workspace, ".reasonix/attachments", saved)).size, streamBytes);
  assert.equal(traffic.length, 4);
  assert.equal(traffic[3].contentType, "application/octet-stream");
  const configPath = path.join(home, "config.toml");
  fs.appendFileSync(configPath, "\n[attachments]\nmax_stream_bytes = 4\n");
  const directory = path.join(workspace, ".reasonix/attachments");
  const before = fs.readdirSync(directory).sort();
  const refused = page.waitForResponse(response => response.url().includes("/attachments/upload") && response.status() === 413);
  await page.evaluate(() => {
    const transfer = new DataTransfer();
    transfer.items.add(new File(["12345"], "over-limit.apk"));
    const input = document.querySelector('.pane[data-active] input[type=file][data-action-change="session.attach"]');
    input.files = transfer.files;
    input.dispatchEvent(new Event("change", { bubbles: true }));
  });
  const rejection = await (await refused).json();
  assert.equal(rejection.code, "attachment.too_large");
  assert.equal(rejection.params.limit_bytes, 4);
  assert.equal(rejection.params.limit_source, "user");
  const failedChip = page.locator('.pane[data-active] .shot[data-state="failed"]');
  await failedChip.locator(".why").waitFor({ state: "visible" });
  assert.ok((await failedChip.locator(".why").textContent()).includes("MB"));
  assert.deepEqual(fs.readdirSync(directory).sort(), before);
  fs.writeFileSync(configPath, fs.readFileSync(configPath, "utf8").replace("max_stream_bytes = 4", "max_stream_bytes = 0"));
  const retried = page.waitForResponse(response => response.url().includes("/attachments/upload"));
  await failedChip.locator("button.retry").click();
  assert.equal((await retried).status(), 200);
  await ready(5);
  assert.ok(await page.locator('.pane[data-active] textarea[data-action-paste="session.attach"]').isVisible());
  const result = { nativeBytes, nativeEntries: ["picker", "paste", "drop"], streamBytes, readyAttachments: 5, limitRejection: rejection, unlimitedRetry: true, traffic };
  if (artifacts) {
    fs.mkdirSync(artifacts, { recursive: true });
    await page.screenshot({ path: path.join(artifacts, "attachments-live.png") });
    fs.writeFileSync(path.join(artifacts, "attachments-live.json"), JSON.stringify(result, null, 2) + "\n");
  }
  console.log(JSON.stringify(result));
  passed = true;
} finally {
  await app?.evaluate(({ app }) => app.exit(0)).catch(() => {});
  await app?.close();
  if (passed) fs.rmSync(home, { recursive: true, force: true });
  else console.error("Failed run retained fixtures at " + home);
}
