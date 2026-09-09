import React, { act, Suspense, startTransition } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import assert from "node:assert/strict";
import { useTranscriptCommand } from "../lib/useTranscriptCommand";

const dom = new JSDOM("<div id='root'></div>");
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
let latest!: (offset: number) => number;
const suspended = new Promise<void>(() => {});
function Probe({ revision, suspend = false }: { revision: number; suspend?: boolean }) {
  latest = useTranscriptCommand((offset: number) => revision + offset);
  if (suspend) throw suspended;
  return null;
}

try {
  await act(async () => root.render(<Suspense><Probe revision={1} /></Suspense>));
  const retainedCommand = latest;
  assert.equal(retainedCommand(10), 11);
  for (let revision = 2; revision <= 100; revision += 1) {
    await act(async () => root.render(<Suspense><Probe revision={revision} /></Suspense>));
    assert.equal(latest, retainedCommand, "commands retain identity across presentation commits");
    assert.equal(retainedCommand(10), revision + 10, "old holders dispatch to only the latest committed presentation");
  }
  await act(async () => startTransition(() => root.render(<Suspense><Probe revision={999} suspend /></Suspense>)));
  assert.equal(retainedCommand(10), 110, "an abandoned/suspended render cannot acquire command authority");
  console.log("PASS stable Transcript commands use committed state without cross-render callback chains");
} finally {
  await act(async () => root.unmount());
  dom.window.close();
}
