import assert from "node:assert/strict";
import { act } from "react";
import { createTranscriptHarness } from "./transcript-dom-harness";
import type { Item } from "../lib/useController";

const harness = await createTranscriptHarness({ deterministic: true });
const items: Item[] = [
  { kind: "user", id: "u1", text: "hello", checkpointTurn: 1 },
  { kind: "tool", id: "t1", name: "read_file", args: "{}", output: "result", readOnly: true, status: "done" },
  { kind: "assistant", id: "a1", text: "answer", reasoning: "thought", streaming: false },
];
try {
  await harness.loadModule("/src/components/ChatToolBody.tsx");
  await harness.render(items);
  await harness.settle();
  assert.ok(harness.container.querySelector(".chat-column"));
  assert.equal(harness.container.querySelectorAll(".transcript__window-item").length, 0);
  assert.equal(harness.container.querySelectorAll(".chat-tool").length, 0, "completed process unmounts heavy rows");
  const disclosure = harness.container.querySelector<HTMLButtonElement>(".chat-process");
  await act(async () => disclosure!.click());
  const tool = harness.container.querySelector<HTMLElement>(".chat-tool [data-disclosure-row]");
  assert.ok(tool);
  await act(async () => tool.click());
  await harness.settle();
  assert.ok(harness.container.querySelector(".dsh-ToolRow-ioCard"), "tool opens an inline preview");
  await act(async () => harness.container.querySelector<HTMLButtonElement>(".dsh-ToolRow-inspectButton")!.click());
  assert.ok(harness.container.querySelector('[role="dialog"]'));
  for (let i = 0; i < 60; i++) {
    await harness.render(items.map(item => item.kind === "assistant" ? { ...item, text: `answer ${i}`, streaming: true } : item), { running: true });
    await act(async () => { harness.resizeNotifications.forEach(notify => notify()); harness.clock.advance(16); });
  }
  assert.ok(harness.container.querySelector(".chat-column"), "60 geometry changes do not trip React nested update fuse");
  await harness.render(items, { geometrySessionKey: "other" });
  assert.equal(harness.container.querySelector('[role="dialog"]'), null, "session replacement closes details");
  console.log("chat natural flow: process disclosure, details, 60-commit cascade and session isolation passed");
} finally { await harness.unmount(); await harness.close(); }
