import assert from "node:assert/strict";
import { act } from "react";
import { createTranscriptHarness } from "./transcript-dom-harness";
import type { Item } from "../lib/useController";

const harness = await createTranscriptHarness({ deterministic: true });
const { getTranscriptStore } = await harness.loadModule<typeof import("../lib/transcriptStore")>("/src/lib/transcriptStore.ts");
const pending = new Map<string, (text: string) => void>();
const release = getTranscriptStore().registerContentResolver("details-race", async (id, field) => {
  if (field !== "tool") return undefined;
  return new Promise<string>(resolve => pending.set(id, resolve));
});
const items: Item[] = [
  { kind: "user", id: "u", text: "question" },
  ...["first", "second"].map(id => ({ kind: "tool" as const, id, name: id, args: "{}", output: `${id} preview`, status: "done" as const })),
];
try {
  await harness.loadModule("/src/components/ChatToolBody.tsx");
  await harness.render(items, { tabId: "details-race" }); await harness.settle();
  const open = async (index: number) => {
    await act(async () => (harness.container.querySelectorAll<HTMLElement>(".chat-tool [data-disclosure-row]")[index]).click());
    await act(async () => harness.container.querySelectorAll<HTMLButtonElement>(".dsh-ToolRow-inspectButton")[index]!.click());
    await act(async () => harness.container.querySelector<HTMLButtonElement>(".chat-details__body > .btn")!.click());
  };
  await open(0);
  assert.ok(pending.has("first"));
  await act(async () => harness.container.querySelector(".chat-details")!.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })));
  await open(1);
  await act(async () => pending.get("first")!(JSON.stringify({ output: "stale first full" })));
  assert.ok(!harness.container.querySelector(".chat-details")!.textContent?.includes("stale first full"));
  await act(async () => pending.get("second")!(JSON.stringify({ output: "complete second result" })));
  await harness.settle();
  assert.ok(harness.container.querySelector(".chat-details")!.textContent?.includes("complete second result"));
  await harness.render(items, { tabId: "another-session", geometrySessionKey: "replacement" });
  assert.equal(harness.container.querySelector(".chat-details"), null);
  const archived: Item[] = [{ kind: "tool", id: "corrupt", name: "bash", args: "{}", output: "retained preview", status: "done", dataArchived: true }];
  await harness.render(archived, { tabId: "details-race", geometrySessionKey: "inline-content" });
  await act(async () => harness.container.querySelector<HTMLElement>(".chat-tool [data-disclosure-row]")!.click());
  await harness.settle();
  await act(async () => harness.container.querySelector<HTMLButtonElement>(".chat-tool .btn")!.click());
  await act(async () => pending.get("corrupt")!("malformed full content"));
  await harness.settle();
  assert.ok(harness.container.textContent?.includes("retained preview"), "bad full content retains the preview");
  assert.equal(harness.container.querySelector("[data-terminal]"), null, "a bad response cannot turn preview into a complete copyable result");
  assert.equal(harness.container.querySelector<HTMLButtonElement>(".chat-tool .btn")!.disabled, false, "failed full content can be retried");
  console.log("tool details: old target completion cannot overwrite the current drawer; session replacement closes details");
} finally { release(); await harness.unmount(); await harness.close(); }
