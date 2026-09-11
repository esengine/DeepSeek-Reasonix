import React, { act, useLayoutEffect } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import assert from "node:assert/strict";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { useCommittedAsyncCommand } from "../lib/useCommittedAsyncCommand";
import type { CommandOutcome } from "../lib/commandOutcome";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, {
  window: dom.window,
  document: dom.window.document,
  IS_REACT_ACT_ENVIRONMENT: true,
});
const root = createRoot(document.getElementById("root")!);
let retained!: () => void;
let effects = 0;
const failures: string[] = [];
function check(label: string, run: () => void) {
  try { run(); console.log(`PASS ${label}`); }
  catch (error) { failures.push(`${label}: ${String(error)}`); }
}

function Probe() {
  retained = useCommittedCommand(() => { effects += 1; });
  return null;
}

let release!: () => void;
const gate = new Promise<void>((resolve) => { release = resolve; });
let pending!: Promise<CommandOutcome<void>>;
const awaitGate = (input: Promise<void>) => input;
function LayoutProbe() {
  const command = useCommittedAsyncCommand(() => gate, awaitGate);
  useLayoutEffect(() => { pending = command(); }, [command]);
  return null;
}

try {
  await act(async () => root.render(<Probe />));
  retained();
  assert.equal(effects, 1);
  act(() => {
    root.unmount();
    retained();
    check("unmount revokes command authority synchronously", () => assert.equal(effects, 1));
  });
  const layoutRoot = createRoot(document.createElement("div"));
  await act(async () => layoutRoot.render(<LayoutProbe />));
  release();
  const result = await pending;
  check("normal passive setup does not invalidate layout-started work", () => {
    assert.deepEqual(result, { status: "completed", value: undefined });
  });
  await act(async () => layoutRoot.unmount());
  assert.deepEqual(failures, []);
} finally {
  dom.window.close();
}
