import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { canonicalHistorySlice, canonicalMessage } from "../lib/canonicalTranscriptBackend";
import { installDesktopHostStub } from "./desktopHostStub";
import { mockHistoryContentField, mockHistorySlice } from "../lib/bridgeHistoryFixtures";
import { makeMockSessionReaderBindings } from "../lib/sessionReaderBridge";
import type { HistoryContentRef, HistoryMessage, HistorySliceRequest } from "../lib/types";

const marker = "ASYNC LAYOUT EXPANSION COMPLETE";
const messages: HistoryMessage[] = [
  { role: "user", content: "question", reasoning: "inline reasoning", toolCalls: [{ id: "call-1", name: "read", arguments: "{}" }] },
  { role: "assistant", content: `${"preview ".repeat(700)}\n${marker}`, reasoning: "complete reasoning" },
];
let contentReads = 0;
const host = Object.assign({
  async HistoryForTab() { return messages; },
  async HistorySliceForTab(tabID: string, req: HistorySliceRequest) { return mockHistorySlice(tabID, messages, req, true); },
  async HistoryContentForTab(_tabID: string, ref: HistoryContentRef) {
    contentReads += 1;
    return { entryId: ref.entryId, field: ref.field, chunk: 0, chunks: 1, data: mockHistoryContentField(messages[1], ref), done: true, stale: false };
  },
}, makeMockSessionReaderBindings());

const view = await host.SessionOpenForTab("tab-1");
assert.equal(view.recent.entries.length, 2);
const inline = canonicalMessage(view.recent.entries[0], view.recent.entries[0].inline);
assert.equal(inline.reasoning, "inline reasoning");
assert.equal(inline.toolCalls?.[0].id, "call-1");
const persistent = view.recent.entries[1];
assert.ok(persistent.contentRef, "lazy fixture keeps a canonical content reference");
assert.equal((persistent.inline as HistoryMessage | undefined)?.content.includes(marker), false, "open response contains only the legacy preview");

const chunk = await host.SessionHistoryContentForTab("tab-1", persistent.contentRef!, 0);
assert.equal(contentReads, 1, "canonical hydration preserves the legacy asynchronous read path");
const decoded = new TextDecoder().decode(Uint8Array.from(atob(chunk.data), character => character.charCodeAt(0)));
const hydrated = canonicalMessage(persistent, JSON.parse(decoded));
assert.equal(hydrated.messageId, persistent.messageId, "hydration keeps the stable message identity");
assert.ok(hydrated.content.includes(marker));
assert.equal(hydrated.reasoning, "complete reasoning");

const dom = new JSDOM("", { url: "http://localhost/" });
globalThis.window = dom.window as unknown as Window & typeof globalThis;
const stub = installDesktopHostStub({ SessionHistoryWindowForTab: async () => ({
  messages: [{ ...view.recent.entries[0], position: 50, visibleTurn: 4 }], status: "ready", snapshotSequence: 0,
  coverageSequence: 0, generation: "mock", totalTurns: 4, hasOlder: true, hasNewer: false, olderCursor: "older",
}) });
const limited = await canonicalHistorySlice("tab-1", { cursor: "" });
assert.equal(limited.entries.length, 1);
assert.equal(limited.hasOlder, true, "a byte-limited recent window still exposes older history");
assert.ok(limited.nextCursor);
assert.equal(limited.revisionKnown, false, "zero sequence does not invent a durable fingerprint");
stub.commands.SessionHistoryWindowForTab = async () => ({ messages: [], status: "ready", snapshotSequence: 0, coverageSequence: 0, totalTurns: 0, hasOlder: false, hasNewer: false });
const empty = await canonicalHistorySlice("tab-1", { cursor: "" });
assert.equal(empty.entries.length, 0, "an empty recent snapshot does not require a locator read");
stub.uninstall();
dom.window.close();

console.log("PASS protocol 7 mock sessions preserve benchmark history and lazy hydration");
