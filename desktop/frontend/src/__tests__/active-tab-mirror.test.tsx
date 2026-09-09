import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { activeTabMirror, useActiveTabMirrorCommit } from "../app-runtime/activeTabMirror";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);

function Probe({ activeTabId }: { activeTabId?: string }) {
  useActiveTabMirrorCommit(activeTabId);
  return null;
}

try {
  await act(async () => root.render(<Probe activeTabId="A" />));
  assert.equal(activeTabMirror().current, "A", "the mirror follows the committed active tab");

  const reads: (string | undefined)[] = [];
  const deferredRead = new Promise<void>((resolve) => {
    setTimeout(() => {
      reads.push(activeTabMirror().current);
      resolve();
    }, 0);
  });
  await act(async () => root.render(<Probe activeTabId="B" />));
  await deferredRead;
  assert.deepEqual(reads, ["B"], "an async continuation reads the replacement tab, never a stale render capture");

  await act(async () => root.render(<Probe activeTabId={undefined} />));
  assert.equal(activeTabMirror().current, undefined, "a committed empty selection clears the mirror");

  await act(async () => root.render(<Probe activeTabId="B" />));
  assert.equal(activeTabMirror().current, "B", "returning to a tab commits its identity again");

  await act(async () => root.unmount());
  assert.equal(activeTabMirror().current, undefined, "unmounting the host releases the mirror");

  console.log("active tab mirror: commit-following writes, async reads and unmount release passed");
} finally { dom.window.close(); }
