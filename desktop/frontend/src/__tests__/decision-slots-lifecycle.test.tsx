import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { DecisionFooterSlots } from "../app-shell/DecisionFooterRegion";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
let ready = true;
let release!: () => void;
const gate = new Promise<void>(resolve => { release = resolve; });
function Decision() { if (!ready) throw gate; return <button id="decision">decision</button>; }
const paint = () => act(async () => root.render(<DecisionFooterSlots todo={<button id="todo">todo</button>} undo={<button id="undo">undo</button>} decision={<Decision />} />));
try {
  await paint();
  const todo = document.getElementById("todo")!;
  const undo = document.getElementById("undo")!;
  undo.focus();
  ready = false;
  await paint();
  assert.equal(document.getElementById("todo"), todo);
  assert.equal(document.getElementById("undo"), undo);
  assert.equal(undo.style.display, "", "a loading decision never hides the existing undo action");
  assert.equal(todo.style.display, "", "a loading decision never hides existing work status");
  assert.equal(document.activeElement, undo);
  await act(async () => { ready = true; release(); });
  assert.equal(document.getElementById("undo"), undo);
  console.log("decision slots: independent Suspense preserves visible siblings and focus");
} finally { await act(async () => root.unmount()); dom.window.close(); }
