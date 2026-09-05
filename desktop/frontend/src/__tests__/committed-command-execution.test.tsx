import React, { act, StrictMode, Suspense, startTransition, useLayoutEffect } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import assert from "node:assert/strict";
import { useCommittedAsyncCommand } from "../lib/useCommittedAsyncCommand";
import type { CommandAuthority, CommandOutcome } from "../lib/commandOutcome";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
}
type Input = { target: string; gate: Promise<void>; apply: (target: string) => void };
async function execute(input: Input, authority: CommandAuthority) {
  await input.gate;
  authority.checkpoint();
  input.apply(input.target);
  return input.target;
}

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
const effects: string[] = [];
const apply = (target: string) => { effects.push(target); };
let command!: (gate: Promise<void>) => Promise<CommandOutcome<string>>;
let duringRender!: Promise<CommandOutcome<string>>;
const never = new Promise<void>(() => {});
function Probe({ target, suspend = false, tryBeforeCommit = false }: {
  target: string; suspend?: boolean; tryBeforeCommit?: boolean;
}) {
  const next = useCommittedAsyncCommand((gate: Promise<void>): Input => ({ target, gate, apply }), execute);
  if (tryBeforeCommit) duringRender = next(Promise.resolve());
  useLayoutEffect(() => { command = next; });
  if (suspend) throw never;
  return null;
}

try {
  await act(async () => root.render(<StrictMode><Suspense><Probe target="a" tryBeforeCommit /></Suspense></StrictMode>));
  assert.deepEqual(await duringRender, { status: "cancelled", reason: "not-ready" });
  assert.deepEqual(effects, []);
  const original = command;
  const first = deferred();
  const oldA = command(first.promise);
  await act(async () => root.render(<StrictMode><Suspense><Probe target="b" /></Suspense></StrictMode>));
  assert.equal(command, original);
  const second = deferred();
  const currentB = command(second.promise);
  first.resolve();
  assert.deepEqual(await oldA, { status: "cancelled", reason: "superseded" });
  assert.deepEqual(effects, [], "stale executor performs no intermediate effect");
  second.resolve();
  assert.deepEqual(await currentB, { status: "completed", value: "b" });
  assert.deepEqual(effects, ["b"]);

  const third = deferred();
  const surviving = command(third.promise);
  await act(async () => root.render(<StrictMode><Suspense><Probe target="a" /></Suspense></StrictMode>));
  third.resolve();
  assert.deepEqual(await surviving, { status: "completed", value: "b" }, "rerender does not cancel a source-captured operation");
  assert.deepEqual(effects, ["b", "b"], "source data completion does not retarget to the latest render");

  await act(async () => startTransition(() => root.render(<StrictMode><Suspense><Probe target="abandoned" suspend /></Suspense></StrictMode>)));
  assert.deepEqual(await command(Promise.resolve()), { status: "completed", value: "a" });
  assert.deepEqual(effects, ["b", "b", "a"]);

  const disposedGate = deferred();
  const disposed = command(disposedGate.promise);
  act(() => { root.unmount(); });
  disposedGate.resolve();
  assert.deepEqual(await disposed, { status: "cancelled", reason: "disposed" });
  assert.deepEqual(await original(Promise.resolve()), { status: "cancelled", reason: "disposed" });
  assert.deepEqual(effects, ["b", "b", "a"], "unmount fences the effect after an uncancellable promise");
  console.log("PASS committed capture/executor separates source input from lifecycle-owned effects");
} finally {
  dom.window.close();
}
