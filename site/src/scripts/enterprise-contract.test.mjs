import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptsDir = path.dirname(fileURLToPath(import.meta.url));
const source = await readFile(path.join(scriptsDir, "../pages/index.astro"), "utf8");
const behavior = await readFile(path.join(scriptsDir, "ip-platform.mjs"), "utf8");
const styles = await readFile(path.join(scriptsDir, "../styles/ip-platform.css"), "utf8");

test("live results expose a review and publish workflow", () => {
  assert.match(source, /data-testid="publish-analysis"/);
  assert.match(source, /id="publication-status"/);
  assert.match(behavior, /\/publish/);
  assert.match(behavior, /renderPublishedAsset/);
});

test("Wiki and provenance have dynamic enterprise hooks", () => {
  assert.match(source, /id="wiki-search"/);
  assert.match(source, /id="wiki-search-status"/);
  assert.match(source, /id="wiki-dynamic-title"/);
  assert.match(source, /data-evidence-id/);
  assert.match(behavior, /renderDynamicWiki/);
  assert.match(behavior, /renderEvidence/);
});

test("provider boundary copy is truthful in real mode", () => {
  assert.match(source, /受控文档处理/);
  assert.match(source, /仅处理已授权上传的资料/);
  assert.doesNotMatch(source, /数据未离开企业网络/);
  assert.match(source, /id="provider-boundary-status"/);
});

test("enterprise accessibility contracts cover navigation and drawers", () => {
  assert.match(source, /aria-controls="view-wiki"/);
  assert.match(source, /role="dialog"/);
  assert.match(source, /aria-modal="true"/);
  assert.match(behavior, /trapDrawerFocus/);
  assert.match(styles, /\.wiki-focus-toggle/);
  assert.match(styles, /\.empty-state/);
});

test("SMB workspace login and versioned Wiki controls are wired", () => {
  for (const hook of [
    'id="session-dialog"',
    'id="login-form"',
    'id="workspace-name"',
    'id="profile-role"',
    'id="wiki-edit"',
    'id="wiki-edit-form"',
    'id="wiki-history-drawer"',
  ]) assert.match(source, new RegExp(hook));
  assert.match(behavior, /fetch\("\/api\/session"/);
  assert.match(behavior, /\/api\/auth\/login/);
  assert.match(behavior, /method: "PATCH"/);
  assert.match(behavior, /\/versions/);
  assert.match(behavior, /response\.status === 409/);
});
