// Qualify an assembled app with a loopback provider and disposable data home.
// Usage: node desktop/packaging/attachment-native-smoke.mjs <Reasonix.app> <evidence-dir>
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { createServer } from "node:http";
import { createRequire } from "node:module";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, globSync, unlinkSync, cpSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { packagedSmokeEnv } from "./smoke-env.mjs";
import { waitForSmokeCondition } from "./smoke-poll.mjs";

const require = createRequire(new URL("../electron/package.json", import.meta.url));
const { _electron } = require("playwright");
const bundle = resolve(process.argv[2]);
const evidence = resolve(process.argv[3]);
const home = mkdtempSync(join(tmpdir(), "reasonix-attachment-native-"));
const project = join(home, "project");
mkdirSync(evidence, { recursive: true });
mkdirSync(project, { recursive: true });
const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";
const dataURL = `data:image/png;base64,${png}`;
const digest = createHash("sha256").update(Buffer.from(png, "base64")).digest("hex");
const requests = [];
const checks = [];
const held = new Map();
let app, page, compatibilityRef;
const provider = createServer(async (req, res) => {
  let body = "";
  for await (const part of req) body += part;
  const payload = JSON.parse(body || "{}");
  requests.push(payload);
  const messages = payload.messages || [];
  const lastUser = [...messages].reverse().find(message => message.role === "user");
  if (JSON.stringify(lastUser?.content).includes("CANCEL_IMAGE_REQUEST")) {
    const state = { closed: false };
    held.set("cancel", state);
    res.on("close", () => { state.closed = true; });
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    res.write(": waiting for cancellation\n\n");
    return;
  }
  const toolDone = messages.some(message => message.role === "tool");
  const readTool = JSON.stringify(lastUser?.content).includes("READ_IMAGE") && !toolDone;
  const delta = readTool ? { tool_calls: [{ index: 0, id: "image-tool", type: "function", function: { name: "view_image", arguments: JSON.stringify({ path: join(project, "tool.png") }) } }] }
    : { content: "ATTACHMENT_FIXTURE_OK" };
  res.writeHead(200, { "Content-Type": "text/event-stream" });
  res.end(`data: ${JSON.stringify({ id: "attachment-fixture", choices: [{ index: 0, delta, finish_reason: null }] })}\n\n`
    + `data: ${JSON.stringify({ id: "attachment-fixture", choices: [{ index: 0, delta: {}, finish_reason: readTool ? "tool_calls" : "stop" }] })}\n\ndata: [DONE]\n\n`);
});
await new Promise(resolve => provider.listen(0, "127.0.0.1", resolve));
writeFileSync(join(home, "config.toml"), `default_model = "fixture/vision"\n[desktop]\nprovider_access = ["fixture"]\n[[providers]]\nname = "fixture"\nkind = "openai"\nbase_url = "http://127.0.0.1:${provider.address().port}/v1"\nmodels = ["vision", "vision-alt"]\nvision_models = ["vision", "vision-alt"]\ndefault = "vision"\napi_key_env = "ATTACHMENT_FIXTURE_KEY"\n`);
writeFileSync(join(project, "tool.png"), Buffer.from(png, "base64"));
const invoke = (method, args = []) => page.evaluate(({ method, args }) => window.reasonixDesktop.invoke(method, args), { method, args });
const active = async () => (await invoke("ListTabs")).find(tab => tab.active);
const settle = () => waitForSmokeCondition(async () => (await invoke("ListTabs")).every(tab => !tab.running));
const composerTarget = tab => ({
  kind: "session",
  tabId: tab.id,
  session: tab.session ?? null,
  generation: tab.sessionGeneration ?? 0,
});
const record = text => { checks.push(text); console.log(`PASS ${text}`); };
function imageURLs(value, out = []) {
  if (!value || typeof value !== "object") return out;
  if (value.type === "image_url") out.push(value.image_url.url);
  for (const child of Object.values(value)) {
    if (Array.isArray(child)) child.forEach(item => imageURLs(item, out));
    else if (child && typeof child === "object") imageURLs(child, out);
  }
  return out;
}
async function newProject(mode) {
  const tab = await invoke("EnsureBlankSurface", ["project", project]);
  await invoke("SetActiveTab", [tab.id]);
  await invoke("SetToolApprovalModeForTab", [tab.id, mode]);
  return tab;
}
async function submitImage(tab, label) {
  const target = await invoke("CaptureAttachmentTarget", [composerTarget(tab)]);
  try {
    const draft = await invoke("StageImageForTarget", [target.token, label, "pixel.png", "image/png", dataURL]);
    assert.equal(await invoke("ReadDraftImageForTarget", [target.token, draft.draftId]), dataURL);
    const before = requests.length;
    const request = { input: label, display: label, attachments: [{ clientAttachmentId: label, draftId: draft.draftId }] };
    const receipt = await invoke("StartTurnForAttachmentTarget", [target.token, label, request]);
    await waitForSmokeCondition(() => requests.length > before);
    await settle();
    assert.ok(requests.slice(before).some(request => imageURLs(request).includes(dataURL)));
    const historyImage = await invoke("ReadSessionAttachmentForTab", [tab.id, digest, 0]);
    assert.equal(historyImage.data, png);
    assert.equal(historyImage.done, true);
    const retry = await invoke("StartTurnForAttachmentTarget", [target.token, label, request]);
    assert.equal(retry.turnId, receipt.turnId);
    return { target, draft, request };
  } finally { await invoke("ReleaseAttachmentTarget", [target.token]); }
}
try {
  app = await _electron.launch({ executablePath: join(bundle, "Contents/MacOS/Reasonix"), env: { ...packagedSmokeEnv(process.env, home), ATTACHMENT_FIXTURE_KEY: "loopback-only" }, timeout: 60_000 });
  const manifest = JSON.parse(readFileSync(join(bundle, "Contents/Resources/build.json"), "utf8"));
  await waitForSmokeCondition(async () => {
    for (const candidate of app.windows()) {
      try {
        if (await candidate.evaluate(() => Boolean(window.reasonixDesktop))) {
          const version = await candidate.evaluate(() => window.reasonixDesktop.invoke("Version", []));
          if (version === manifest.version) { page = candidate; return true; }
        }
      } catch { /* The splash window can be replaced during the handshake. */ }
    }
    return false;
  }, { timeout: 60_000 });
  assert.equal(await app.evaluate(({ app }) => app.isPackaged), true);
  await page.locator("textarea").first().waitFor({ state: "visible", timeout: 60_000 });
  record("packaged renderer and service share the stamped version");
  for (const mode of ["workspace-write", "danger-full-access"]) {
    const tab = await newProject(mode);
    await submitImage(tab, `AUTO_${mode}`);
    compatibilityRef ??= (await active()).session;
    record(`${mode}: automatic image request contains exact admitted bytes; retry reuses receipt`);
    const toolTab = await newProject(mode);
    const before = requests.length;
    await invoke("SubmitToTabWithID", [toolTab.id, "READ_IMAGE", `TOOL_${mode}`]);
    await waitForSmokeCondition(() => requests.length >= before + 2);
    await settle();
    assert.ok(requests.slice(before).some(request => imageURLs(request).includes(dataURL)));
    record(`${mode}: view_image returns the same bytes without elevation`);
  }
  const cancelTab = await newProject("workspace-write");
  const cancelTarget = await invoke("CaptureAttachmentTarget", [composerTarget(cancelTab)]);
  const cancelDraft = await invoke("StageImageForTarget", [cancelTarget.token, "cancel-image", "cancel.png", "image/png", dataURL]);
  await invoke("StartTurnForAttachmentTarget", [cancelTarget.token, "cancel-turn", { input: "CANCEL_IMAGE_REQUEST", attachments: [{ clientAttachmentId: "cancel", draftId: cancelDraft.draftId }] }]);
  await waitForSmokeCondition(() => held.has("cancel"));
  await invoke("SetToolApprovalModeForTab", [cancelTab.id, "danger-full-access"]);
  await waitForSmokeCondition(() => held.get("cancel").closed);
  await settle();
  await invoke("ReleaseAttachmentTarget", [cancelTarget.token]);
  await submitImage(cancelTab, "RETRY_AFTER_PERMISSION_CANCEL");
  record("permission change cancels the in-flight image request; a new send succeeds");
  const beforeRebuild = requests.length;
  const rebuildTarget = await invoke("CaptureAttachmentTarget", [composerTarget(cancelTab)]);
  const rebuildDraft = await invoke("StageImageForTarget", [rebuildTarget.token, "rebuild", "rebuild.png", "image/png", dataURL]);
  await invoke("SetModelForTab", [cancelTab.id, "fixture/vision-alt"]);
  await assert.rejects(invoke("ReadDraftImageForTarget", [rebuildTarget.token, rebuildDraft.draftId]));
  const reboundTarget = await invoke("CaptureAttachmentTarget", [composerTarget(await active())]);
  const reboundDraft = await invoke("RebindDraftImageForTarget", [reboundTarget.token, rebuildDraft.draftId]);
  assert.notEqual(reboundDraft.draftId, rebuildDraft.draftId);
  assert.equal(requests.length, beforeRebuild);
  await invoke("StartTurnForAttachmentTarget", [reboundTarget.token, "rebound-turn", { input: "AFTER_RUNTIME_REBUILD", attachments: [{ clientAttachmentId: "rebuild", draftId: rebuildDraft.draftId }] }]);
  await waitForSmokeCondition(() => requests.length > beforeRebuild);
  await settle();
  assert.ok(requests.slice(beforeRebuild).some(request => imageURLs(request).includes(dataURL)));
  await invoke("ReleaseAttachmentTarget", [rebuildTarget.token]);
  await invoke("ReleaseAttachmentTarget", [reboundTarget.token]);
  record("runtime rebuild renews a same-session draft without auto-send; user retry delivers original bytes");
  mkdirSync(join(project, ".reasonix/attachments"), { recursive: true });
  writeFileSync(join(project, ".reasonix/attachments/cli.png"), Buffer.from(png, "base64"));
  const beforeCLI = requests.length;
  const cli = await promisify(execFile)(join(bundle, "Contents/Resources/service/reasonix"), ["run", "--dir", project, "--print", "inspect @.reasonix/attachments/cli.png"], {
    env: { ...packagedSmokeEnv(process.env, home), ATTACHMENT_FIXTURE_KEY: "loopback-only" }, timeout: 60_000,
  });
  assert.match(cli.stdout, /ATTACHMENT_FIXTURE_OK/);
  assert.ok(requests.slice(beforeCLI).some(request => imageURLs(request).includes(dataURL)));
  record("bundled CLI sends exact legacy workspace attachment bytes to the provider");
  await invoke("EnsureBlankSurface", ["global", ""]);
  const global = await active();
  await invoke("SubmitToTabWithID", [global.id, "PIN_GLOBAL_SESSION", "pin-global"]);
  await settle();
  const target = await invoke("CaptureAttachmentTarget", [composerTarget(global)]);
  const other = await invoke("EnsureBlankTab", ["project", project]);
  await invoke("SetActiveTab", [other.id]);
  const draft = await invoke("StageImageForTarget", [target.token, "focus", "focus.png", "image/png", dataURL]);
  assert.equal(await invoke("ReadDraftImageForTarget", [target.token, draft.draftId]), dataURL);
  const otherTarget = await invoke("CaptureAttachmentTarget", [composerTarget(other)]);
  await assert.rejects(invoke("ReadDraftImageForTarget", [otherTarget.token, draft.draftId]));
  await invoke("ReleaseAttachmentTarget", [target.token]);
  await invoke("ReleaseAttachmentTarget", [otherTarget.token]);
  record("Global-to-project switch preserves target ownership and rejects cross-session draft reads");

  // RPC navigation above deliberately bypasses the renderer navigation owner.
  // Reload to hydrate that owner before exercising the real Composer UI.
  await page.reload();
  await page.locator("textarea").first().waitFor({ state: "visible" });
  await waitForSmokeCondition(async () => !(await page.locator("textarea").first().isDisabled()));
  const textarea = page.locator("textarea").first();
  await textarea.fill("RETAIN_FAILED_DRAFT");
  await textarea.evaluate((node, png) => {
    const bytes = Uint8Array.from(atob(png), char => char.charCodeAt(0));
    const data = new DataTransfer();
    data.items.add(new File([bytes], "retained.png", { type: "image/png" }));
    node.dispatchEvent(new ClipboardEvent("paste", { bubbles: true, cancelable: true, clipboardData: data }));
  }, png);
  await page.locator(".composer-context__item").first().waitFor();
  const objects = globSync(`**/.content-v1/objects/**/${digest}`, { cwd: home });
  const workspaceSources = globSync("**/.reasonix/attachments/*", { cwd: home }).filter(path => {
    const bytes = readFileSync(join(home, path));
    return createHash("sha256").update(bytes).digest("hex") === digest;
  });
  const admittedFiles = [...objects, ...workspaceSources];
  assert.ok(admittedFiles.length > 0);
  for (const path of admittedFiles) unlinkSync(join(home, path));
  const before = requests.length;
  await page.locator(".composer__btn--send").click();
  await page.waitForFunction(() => /图片读取失败|could not be read/i.test(document.body.textContent || ""));
  await waitForSmokeCondition(async () => !(await page.locator(".composer__btn--send").isDisabled()));
  assert.equal((await page.locator("body").innerText()).includes(home), false);
  assert.equal(await textarea.inputValue(), "RETAIN_FAILED_DRAFT");
  assert.equal(await page.locator(".composer-context__item").count(), 1);
  assert.equal(requests.length, before);
  await page.screenshot({ path: join(evidence, "failed-draft-retained.png") });
  record("deleted content blocks provider calls and retains the actual Composer text and image");
  for (const path of admittedFiles) writeFileSync(join(home, path), Buffer.from(png, "base64"));
  if (process.argv[4]) {
    assert.ok(compatibilityRef?.sessionId);
    await app.close();
    app = null;
    const oldHome = mkdtempSync(join(tmpdir(), "reasonix-attachment-downgrade-"));
    cpSync(home, oldHome, { recursive: true });
    const sessionDir = join(oldHome, "desktop-sessions-v5/by-id", compatibilityRef.sessionId);
    const protectedFiles = globSync("**/*", { cwd: sessionDir }).filter(path => /manifest\.json$|events\.frames$/.test(path));
    const beforeFiles = protectedFiles.map(path => [path, readFileSync(join(sessionDir, path))]);
    assert.ok(beforeFiles.length >= 2);
    const previous = resolve(process.argv[4]);
    app = await _electron.launch({ executablePath: join(previous, "Contents/MacOS/Reasonix"), env: { ...packagedSmokeEnv(process.env, oldHome), ATTACHMENT_FIXTURE_KEY: "loopback-only" }, timeout: 60_000 });
    await waitForSmokeCondition(async () => {
      for (const candidate of app.windows()) {
        try {
          if (await candidate.evaluate(() => window.reasonixDesktop?.invoke("Version", []))) { page = candidate; return true; }
        } catch { /* Wait for the production service. */ }
      }
      return false;
    }, { timeout: 60_000 });
    const beforeRequests = requests.length;
    await assert.rejects(invoke("OpenSession", [compatibilityRef]), /revision|unsupported|版本|格式/i);
    for (const [path, bytes] of beforeFiles) assert.deepEqual(readFileSync(join(sessionDir, path)), bytes);
    assert.equal(requests.length, beforeRequests);
    record("previous installed binary rejects revision 3 and preserves manifest/event bytes");
  }
  writeFileSync(join(evidence, "result.json"), JSON.stringify({ manifest, home, checks, imageDigest: digest, providerRequests: requests.length }, null, 2));
} catch (error) {
  if (page && !page.isClosed()) {
    await page.screenshot({ path: join(evidence, "failure.png") });
    writeFileSync(join(evidence, "failure-dom.txt"), await page.locator("body").innerText());
  }
  writeFileSync(join(evidence, "failure.json"), JSON.stringify({ home, checks, error: String(error), stack: error.stack }, null, 2));
  throw error;
} finally {
  if (app) await app.close();
  await new Promise(resolve => provider.close(resolve));
}
