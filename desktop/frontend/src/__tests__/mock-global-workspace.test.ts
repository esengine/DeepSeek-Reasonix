import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { app } from "../lib/bridge";

const dom = new JSDOM("", { url: "http://localhost/?mock=1" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage });
try {
  const globalRoot = (await app.ListProjectTree()).find(node => node.kind === "global_folder")?.root;
  assert.ok(globalRoot, "the global folder exposes its actual directory");
  const initial = (await app.ListTabs()).find(tab => tab.scope === "global");
  const opened = await app.OpenGlobalTab("fixture-global");
  const blank = await app.EnsureBlankSurface("global", "");
  for (const tab of [initial, opened, blank]) {
    assert.equal(tab?.workspaceRoot, globalRoot, "mock global tabs match the native directory identity");
    assert.equal(tab?.workspacePath, globalRoot);
    assert.equal(tab?.cwd, globalRoot);
  }
  console.log("mock global workspace: initial, opened and blank tabs preserve the native directory identity");
} finally { dom.window.close(); }
