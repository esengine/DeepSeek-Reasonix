import assert from "node:assert/strict";
import { test } from "node:test";
import { RpcError } from "../rpc.js";
import { ActionExecutor, type ActRequest } from "./actions.js";
import { DocumentRegistry } from "./documents.js";
import { BROWSER_ERR_NO_GRANT, BROWSER_ERR_STALE_REFERENCE, BROWSER_ERR_TAKEN_OVER, noGrant } from "./errors.js";
import { FakeGuestView, FakeViewFactory, silentLog } from "./fakeGuestViews.js";
import type { ResolveOutput } from "./pageScripts.js";
import { BrowserSurfaceManager } from "./surfaceManager.js";

const code = (value: number) => (error: unknown) => error instanceof RpcError && error.code === value;

async function setup() {
  const factory = new FakeViewFactory();
  const manager = new BrowserSurfaceManager({ views: factory, contentSize: () => null, onTakeover() {}, onCrash() {}, log: silentLog, openWaitMs: 5 });
  let tokens = 0;
  const documents = new DocumentRegistry(() => `tok-${++tokens}`);
  const tab = await manager.open("https://a.test", { taskId: "t", temporary: false });
  const view = tab.view as FakeGuestView;
  const page = view.page;
  const answers = {
    resolve: { ok: true, x: 10, y: 20, width: 100, height: 40, tag: "button", type: "", disabled: false, editable: false, frameOffsetKnown: true } as ResolveOutput,
    identity: true,
    select: { ok: true, selected: ["x"] } as unknown,
    locate: { ok: true, tag: "input", type: "file", path: "html > body:nth-child(2) > input:nth-child(1)" } as unknown,
    beforeResolve: () => {},
  };
  const scripts: string[] = [];
  page.run = (source) => {
    scripts.push(source);
    if (source.startsWith("(function pageResolve")) {
      answers.beforeResolve();
      return answers.resolve;
    }
    if (source.startsWith("(function pageIdentity")) return answers.identity;
    if (source.startsWith("(function pageSelect")) return answers.select;
    if (source.startsWith("(function pageLocate")) return answers.locate;
    if (source.startsWith("({ width")) return { width: 800, height: 600 };
    return undefined;
  };
  const token = documents.issue({ tabId: tab.id, epoch: tab.epoch, snapshotId: "snap", frames: [{ prefix: "", frameTreeNodeId: page.mainFrame.frameTreeNodeId, docId: "doc-1" }] });
  const settled: Array<() => void> = [];
  const actions = new ActionExecutor({
    surfaces: manager,
    documents,
    fileExists: (path) => path.endsWith(".txt"),
    sleep: async () => {
      for (const hook of settled.splice(0)) hook();
    },
  });
  const request = (extra: Partial<ActRequest>): ActRequest => ({
    operationId: "op", tabId: tab.id, documentToken: token, action: "click", ref: "e1", text: "", keys: "", options: [], files: [], submit: false, deltaX: 0, deltaY: 0, ...extra,
  });
  let grantOk = true;
  const verify = () => {
    if (!grantOk) throw noGrant("revoked");
  };
  return { manager, documents, tab, view, page, answers, scripts, token, actions, request, verify, settled, revoke: () => (grantOk = false) };
}

test("acts are refused with the contract codes before any input is dispatched", async () => {
  const s = await setup();
  await assert.rejects(s.actions.act(s.tab, s.request({ documentToken: "nope" }), s.verify), code(BROWSER_ERR_STALE_REFERENCE));
  const other = await s.manager.open("https://b.test", { taskId: "t", temporary: false });
  await assert.rejects(s.actions.act(other, s.request({}), s.verify), code(BROWSER_ERR_STALE_REFERENCE), "a token of another tab is stale");

  s.manager.takeover(s.tab.id, "user click");
  await assert.rejects(s.actions.act(s.tab, s.request({}), s.verify), code(BROWSER_ERR_TAKEN_OVER));
  s.manager.resume(s.tab.id);
  await assert.rejects(s.actions.act(s.tab, s.request({}), s.verify), code(BROWSER_ERR_STALE_REFERENCE), "resume bumps the epoch, so the old snapshot is stale");

  const fresh = s.documents.issue({ tabId: s.tab.id, epoch: s.tab.epoch, snapshotId: "snap2", frames: [{ prefix: "", frameTreeNodeId: s.page.mainFrame.frameTreeNodeId, docId: "doc-1" }] });
  s.view.fire().onNavigate("https://a.test/other", false);
  await assert.rejects(s.actions.act(s.tab, s.request({ documentToken: fresh }), s.verify), code(BROWSER_ERR_STALE_REFERENCE), "navigation bumps the epoch");

  const again = s.documents.issue({ tabId: s.tab.id, epoch: s.tab.epoch, snapshotId: "snap3", frames: [{ prefix: "", frameTreeNodeId: s.page.mainFrame.frameTreeNodeId, docId: "doc-1" }] });
  s.revoke();
  await assert.rejects(s.actions.act(s.tab, s.request({ documentToken: again }), s.verify), code(BROWSER_ERR_NO_GRANT));
  assert.equal(s.page.inputs.length, 0, "nothing reached the page");
});

test("a take-over that lands while the ref resolves cancels the act", async () => {
  const s = await setup();
  s.answers.beforeResolve = () => s.manager.takeover(s.tab.id, "user typing");
  await assert.rejects(s.actions.act(s.tab, s.request({}), s.verify), code(BROWSER_ERR_TAKEN_OVER));
  assert.equal(s.page.inputs.length, 0);
});

// Promoted from prototypes/electron-browser/scripts/verify-runtime.cjs: a
// renderer crash between approval and dispatch must cancel the pending act —
// the recovered page comes back in human mode and nothing is ever replayed.
test("a renderer crash while the ref resolves cancels the act without dispatching", async () => {
  const s = await setup();
  s.answers.beforeResolve = () => s.view.fire().onRenderProcessGone("crashed");
  await assert.rejects(s.actions.act(s.tab, s.request({}), s.verify), code(BROWSER_ERR_TAKEN_OVER));
  assert.equal(s.page.inputs.length, 0, "no input reached the crashed page");
  assert.equal(s.tab.mode, "human", "the recovered tab waits for a fresh grant");
});

// Promoted from the same prototype suite: input already dispatched when the
// renderer dies is treated as applied (executed, no token rotation), so the
// Go-side ledger can never settle it as not-executed and replay it.
test("a renderer crash after dispatch completes the act without replay", async () => {
  const s = await setup();
  s.settled.push(() => s.view.fire().onRenderProcessGone("crashed"));
  assert.deepEqual(await s.actions.act(s.tab, s.request({}), s.verify), { executed: true });
  assert.equal(s.page.inputs.length, 3, "the click was physically dispatched before the crash");
  assert.equal(s.tab.mode, "human");
  assert.deepEqual(s.page.calls.at(-1), "load:https://a.test/", "the crashed view reloads its last URL");
});

test("click dispatches trusted mouse events at the zoomed centre and rotates the token", async () => {
  const s = await setup();
  s.page.zoom = 2;
  const result = await s.actions.act(s.tab, s.request({}), s.verify);
  assert.deepEqual(result, { executed: true, documentToken: "tok-2" });
  assert.deepEqual(s.page.inputs, [
    { type: "mouseMove", x: 120, y: 80 },
    { type: "mouseDown", x: 120, y: 80, button: "left", clickCount: 1 },
    { type: "mouseUp", x: 120, y: 80, button: "left", clickCount: 1 },
  ]);
  assert.ok(s.tab.agentInputUntil > 0, "the agent's own input is marked so its echo is not a take-over");
  await assert.rejects(s.actions.act(s.tab, s.request({}), s.verify), code(BROWSER_ERR_STALE_REFERENCE), "the old token retired");
  const chained = await s.actions.act(s.tab, s.request({ documentToken: "tok-2" }), s.verify);
  assert.deepEqual(chained, { executed: true, documentToken: "tok-3" });
});

test("a navigation or document replacement during the act completes it without a token", async () => {
  const s = await setup();
  s.settled.push(() => s.view.fire().onNavigate("https://a.test/after-click", false));
  assert.deepEqual(await s.actions.act(s.tab, s.request({}), s.verify), { executed: true });
  const token = s.documents.issue({ tabId: s.tab.id, epoch: s.tab.epoch, snapshotId: "snap2", frames: [{ prefix: "", frameTreeNodeId: s.page.mainFrame.frameTreeNodeId, docId: "doc-2" }] });
  s.answers.identity = false;
  assert.deepEqual(await s.actions.act(s.tab, s.request({ documentToken: token }), s.verify), { executed: true });
});

test("non-interactable targets report executed:false with the same token", async () => {
  const s = await setup();
  s.answers.resolve = { ok: false, reason: "element is covered by another element" };
  assert.deepEqual(await s.actions.act(s.tab, s.request({}), s.verify), { executed: false, reason: "element is covered by another element", documentToken: s.token });
  s.answers.resolve = { ok: true, x: 0, y: 0, width: 5, height: 5, tag: "button", type: "", disabled: true, editable: false, frameOffsetKnown: true };
  assert.equal((await s.actions.act(s.tab, s.request({}), s.verify)).reason, "element is disabled");
  s.answers.resolve = { ok: true, x: 0, y: 0, width: 5, height: 5, tag: "div", type: "", disabled: false, editable: false, frameOffsetKnown: false };
  assert.match((await s.actions.act(s.tab, s.request({}), s.verify)).reason ?? "", /cross-origin frame/);
  assert.equal((await s.actions.act(s.tab, s.request({ action: "explode" }), s.verify)).executed, false);
  assert.equal((await s.actions.act(s.tab, s.request({ ref: "" }), s.verify)).reason, "this action needs a ref");
  assert.equal(s.page.inputs.length, 0);
});

test("type clicks to focus, inserts text and submits with Enter", async () => {
  const s = await setup();
  s.answers.resolve = { ok: true, x: 0, y: 0, width: 10, height: 10, tag: "input", type: "text", disabled: false, editable: true, frameOffsetKnown: true };
  const result = await s.actions.act(s.tab, s.request({ action: "type", text: "hello", submit: true }), s.verify);
  assert.deepEqual(result, { executed: true, documentToken: "tok-2" });
  assert.deepEqual(s.page.inserted, ["hello"]);
  assert.deepEqual(s.page.inputs.map((event) => event.type), ["mouseMove", "mouseDown", "mouseUp", "keyDown", "char", "keyUp"]);
  s.answers.resolve = { ok: true, x: 0, y: 0, width: 10, height: 10, tag: "button", type: "", disabled: false, editable: false, frameOffsetKnown: true };
  assert.equal((await s.actions.act(s.tab, s.request({ action: "type", text: "x", documentToken: "tok-2" }), s.verify)).reason, "element is not editable");
});

test("press sends chords, scroll sends inverted wheel deltas, select runs in the page", async () => {
  const s = await setup();
  assert.deepEqual(await s.actions.act(s.tab, s.request({ action: "press", keys: "Control+a Escape", ref: "" }), s.verify), { executed: true, documentToken: "tok-2" });
  assert.deepEqual(s.page.inputs.map((event) => `${event.type}:${(event as { keyCode?: string }).keyCode ?? ""}`), ["keyDown:a", "keyUp:a", "keyDown:Escape", "keyUp:Escape"]);
  assert.match((await s.actions.act(s.tab, s.request({ action: "press", keys: "Control+", ref: "", documentToken: "tok-2" }), s.verify)).reason ?? "", /malformed/);

  s.page.inputs.length = 0;
  assert.deepEqual(await s.actions.act(s.tab, s.request({ action: "scroll", ref: "", deltaY: 300, documentToken: "tok-2" }), s.verify), { executed: true, documentToken: "tok-3" });
  assert.deepEqual(s.page.inputs, [{ type: "mouseWheel", x: 400, y: 300, deltaX: -0, deltaY: -300, canScroll: true }]);
  assert.equal((await s.actions.act(s.tab, s.request({ action: "scroll", ref: "", documentToken: "tok-3" }), s.verify)).reason, "scroll deltas are both zero");

  assert.deepEqual(await s.actions.act(s.tab, s.request({ action: "select", options: ["x"], documentToken: "tok-3" }), s.verify), { executed: true, documentToken: "tok-4" });
  assert.ok(s.scripts.some((source) => source.startsWith("(function pageSelect") && source.includes('"options":["x"]')));
  s.answers.select = { ok: false, reason: "stale" };
  await assert.rejects(s.actions.act(s.tab, s.request({ action: "select", documentToken: "tok-4" }), s.verify), code(BROWSER_ERR_STALE_REFERENCE));
});

test("press focuses a non-editable ref before sending keys", async () => {
  const s = await setup();
  s.answers.resolve = { ok: true, x: 0, y: 0, width: 10, height: 10, tag: "button", type: "", disabled: false, editable: false, frameOffsetKnown: true };
  s.page.run = (source) => source.startsWith("(function pageFocus") ? true : source.startsWith("(function pageIdentity") ? true : s.answers.resolve;
  const result = await s.actions.act(s.tab, s.request({ action: "press", ref: "e1", keys: "Enter" }), s.verify);
  assert.equal(result.executed, true);
  assert.equal(s.page.inputs.filter((event) => event.type === "keyDown").length, 1);
});

test("upload validates files and sets them through the DevTools protocol", async () => {
  const s = await setup();
  assert.equal((await s.actions.act(s.tab, s.request({ action: "upload" }), s.verify)).reason, "upload needs files");
  assert.match((await s.actions.act(s.tab, s.request({ action: "upload", files: ["relative.txt"] }), s.verify)).reason ?? "", /absolute/);
  assert.match((await s.actions.act(s.tab, s.request({ action: "upload", files: ["/tmp/missing.bin"] }), s.verify)).reason ?? "", /not found/);
  s.page.debugger.respond = (method) => {
    if (method === "Runtime.enable") s.page.debugger.emit("Runtime.executionContextCreated", { context: { id: 9, auxData: { isDefault: false } } });
    return method === "Runtime.callFunctionOn" ? { result: { objectId: "obj-9" } } : {};
  };
  const result = await s.actions.act(s.tab, s.request({ action: "upload", files: ["/tmp/a.txt"] }), s.verify);
  assert.deepEqual(result, { executed: true, documentToken: "tok-2" });
  assert.deepEqual(s.page.debugger.commands.map((command) => command.method), ["Runtime.enable", "Runtime.callFunctionOn", "Runtime.disable", "DOM.setFileInputFiles", "Runtime.releaseObjectGroup"]);
  assert.deepEqual(s.page.debugger.commands[3].params, { objectId: "obj-9", files: ["/tmp/a.txt"] });
  assert.equal(s.page.debugger.attached, false, "the debugger is detached afterwards");
  s.answers.locate = { ok: true, tag: "input", type: "text", path: "html" };
  assert.equal((await s.actions.act(s.tab, s.request({ action: "upload", files: ["/tmp/a.txt"], documentToken: "tok-2" }), s.verify)).reason, "element is not a file input");
});

test("takeover after a focus click preserves an unknown receipt and stops typing", async () => {
  for (const action of ["type", "press"]) {
    const s = await setup();
    s.answers.resolve = { ok: true, x: 0, y: 0, width: 10, height: 10, tag: "input", type: "text", disabled: false, editable: true, frameOffsetKnown: true };
    s.settled.push(() => s.manager.takeover(s.tab.id, "user"));
    const result = await s.actions.act(s.tab, s.request({ action, text: "hello", keys: "Enter" }), s.verify);
    assert.equal(result.outcome, "unknown");
    assert.equal(result.executed, false);
    assert.equal(s.page.inputs.length, 3);
    assert.deepEqual(s.page.inserted, []);
  }
});

test("takeover during insertText cancels the pending submit without a false no-effect receipt", async () => {
  const s = await setup();
  s.answers.resolve = { ok: true, x: 0, y: 0, width: 10, height: 10, tag: "input", type: "text", disabled: false, editable: true, frameOffsetKnown: true };
  s.page.insertText = async () => { s.manager.takeover(s.tab.id, "user"); };
  const result = await s.actions.act(s.tab, s.request({ action: "type", text: "hello", submit: true }), s.verify);
  assert.equal(result.outcome, "unknown");
  assert.equal(s.page.inputs.filter((event) => event.type === "keyDown").length, 0);
});

test("upload verifies takeover and grant after CDP lookup, before attaching files", async () => {
  for (const invalidate of ["takeover", "revoke", "navigation"]) {
    const s = await setup();
    s.page.debugger.respond = (method) => {
      if (method === "Runtime.enable") s.page.debugger.emit("Runtime.executionContextCreated", { context: { id: 9, auxData: { isDefault: false } } });
      if (method === "Runtime.callFunctionOn") {
        if (invalidate === "takeover") s.manager.takeover(s.tab.id, "user");
        if (invalidate === "revoke") s.revoke();
        if (invalidate === "navigation") s.view.fire().onNavigate("https://other.test", false);
        return { result: { objectId: "original-input" } };
      }
      return {};
    };
    await assert.rejects(s.actions.act(s.tab, s.request({ action: "upload", files: ["/tmp/a.txt"] }), s.verify));
    assert.equal(s.page.debugger.commands.some((command) => command.method === "DOM.setFileInputFiles"), false);
  }
});

test("select receipt survives a takeover after its DOM mutation, and lost script replies stay unknown", async () => {
  const s = await setup();
  s.page.run = () => { s.manager.takeover(s.tab.id, "user"); return { ok: true, selected: ["x"] }; };
  assert.deepEqual(await s.actions.act(s.tab, s.request({ action: "select", options: ["x"] }), s.verify), { executed: true });
  const other = await setup();
  other.page.run = () => { throw new Error("renderer gone after change"); };
  assert.equal((await other.actions.act(other.tab, other.request({ action: "select", options: ["x"] }), other.verify)).outcome, "unknown");
});
