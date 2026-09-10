import assert from "node:assert/strict";
import { test } from "node:test";
import { JSDOM } from "jsdom";
import { FakePage } from "./fakeGuestViews.js";
import { REGISTRY_KEY } from "./snapshot.js";
import { uploadFiles } from "./upload.js";

function callInDOM(dom: JSDOM, params: unknown): unknown {
  const call = params as { functionDeclaration: string; arguments: Array<{ value: unknown }> };
  const fn = dom.window.eval(`(${call.functionDeclaration})`) as (...args: unknown[]) => unknown;
  return fn(...call.arguments.map((arg) => arg.value));
}

test("upload keeps the original snapshot node when another file input takes its CSS position", async () => {
  const dom = new JSDOM('<input type="file" id="upload">', { runScripts: "outside-only" });
  try {
    (dom.window as unknown as Record<string, unknown>)[REGISTRY_KEY] = { docId: "doc", snapshotId: "snap", refs: new Map([["e1", dom.window.document.querySelector("input")]]) };
    const page = new FakePage(1);
    let replaced = false;
    page.debugger.respond = (method, params) => {
      if (method === "Runtime.enable") {
        if (replaced) dom.window.document.querySelector("input")!.outerHTML = '<input type="file" id="upload">';
        page.debugger.emit("Runtime.executionContextCreated", { context: { id: 1, auxData: { isDefault: false } } });
      }
      if (method === "Runtime.callFunctionOn") {
        const node = callInDOM(dom, params);
        return { result: node ? { objectId: "original-input" } : { subtype: "null" } };
      }
      return {};
    };
    const located = { ref: "e1", snapshotId: "snap", tag: "input", type: "file", path: "html > body:nth-child(2) > input:nth-child(1)", frame: page.mainFrame, binding: { prefix: "", frameTreeNodeId: 100, docId: "doc" }, isMainFrame: true };
    assert.deepEqual(await uploadFiles(page, located, ["/tmp/file"], () => {}, () => {}), { executed: true });
    page.debugger.commands.length = 0;
    replaced = true;
    const result = await uploadFiles(page, located, ["/tmp/file"], () => {}, () => {});
    assert.equal(result.executed, false);
    assert.equal(page.debugger.commands.some((command) => command.method === "DOM.setFileInputFiles"), false);
  } finally {
    dom.window.close();
  }
});

test("upload treats script-shaped document and reference values as data", async () => {
  const dom = new JSDOM('<input type="file">', { runScripts: "outside-only" });
  try {
    const docId = '\"); globalThis.compromised = true; //';
    const snapshotId = "'); throw new Error('injected'); //";
    const ref = "` ${globalThis.compromised = true} \\ \n \u2028";
    const input = dom.window.document.querySelector("input");
    const globals = dom.window as unknown as Record<string, unknown>;
    globals[REGISTRY_KEY] = { docId, snapshotId, refs: new Map([[ref, input]]) };
    const page = new FakePage(1);
    page.debugger.respond = (method, params) => {
      if (method === "Runtime.enable") page.debugger.emit("Runtime.executionContextCreated", { context: { id: 1, auxData: { isDefault: false } } });
      if (method === "Runtime.callFunctionOn") {
        const node = callInDOM(dom, params);
        assert.equal(node, input);
        return { result: { objectId: "original-input" } };
      }
      return {};
    };
    const located = { ref, snapshotId, tag: "input", type: "file", path: "unused", frame: page.mainFrame, binding: { prefix: "", frameTreeNodeId: 100, docId }, isMainFrame: true };
    assert.deepEqual(await uploadFiles(page, located, ["/tmp/file"], () => {}, () => {}), { executed: true });
    assert.equal(globals.compromised, undefined);
  } finally {
    dom.window.close();
  }
});
