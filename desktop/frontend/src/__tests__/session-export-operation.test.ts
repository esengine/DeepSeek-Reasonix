import { noteSessionObservation, sessionObservationDiagnostics } from "../lib/sessionObservationDiagnostics";
import { notePromptSubmission } from "../lib/promptSubmissionDiagnostics";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { installDesktopHostStub } from "./desktopHostStub";
import { exportChunks, runSessionExport } from "../lib/sessionExportOperation";
import { cancelSessionExport, sessionExportProgress } from "../lib/sessionExportProgress";
import type { SessionExportHandle } from "../generated/desktopContract.generated";
const dom = new JSDOM("<!doctype html><html><body></body></html>", { url: "http://localhost" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document });
const body = Buffer.from("第一条问题\n工具参数和完整输出✓\n最终回答", "utf8");
let cancelled = 0, finished = 0;
let exportedObservation = "";
let blocked = false;
let release!: () => void;
let started!: () => void;
const readStarted = new Promise<void>(resolve => { started = resolve; });
const gate = new Promise<void>(resolve => { release = resolve; });
const stub = installDesktopHostStub({
 BeginSessionExportForTarget: async (_selector: unknown, _tab: string, _format: string, _title: string, observation: string) => {
  exportedObservation = observation;
  return { exportId: "fixed-A", format: "clipboard", snapshot: {} };
 },
 ReadSessionExportChunk: async (_id: string, offset: number) => {
  if (blocked) { started(); await gate; }
  const end = Math.min(offset + 1, body.length);
  return { data: body.subarray(offset, end).toString("base64"), nextOffset: end, done: end === body.length };
 },
 CancelSessionExport: async () => { cancelled++; },
 FinishSessionExport: async () => { finished++; return { paths: [], records: 137, pages: 0 }; },
 CancelSession: async () => { throw Error("export must not cancel execution"); },
});
try {
 noteSessionObservation("session-id:shared", { action: "unsubscribe", tabId: "remote-A", generation: 1, sequence: 7 });
 noteSessionObservation("session-id:shared", { action: "terminal", tabId: "remote-B", generation: 2, sequence: 9 });
 assert.equal(sessionObservationDiagnostics("session-id:shared", "remote-A").events[0]?.action, "unsubscribe");
 assert.equal(sessionObservationDiagnostics("session-id:shared", "remote-B").events.length, 1, "equal remote routes do not mix renderer bindings");
 let text = "";
 for await (const part of exportChunks({ exportId: "fixed-A" } as SessionExportHandle)) text += part;
 assert.equal(text, body.toString("utf8"), "UTF-8 characters crossing every RPC boundary survive");
 const input = { selector: { ref: { hostId: "local", sessionId: "A" } }, tabId: "tab-A", format: "clipboard", title: "A", remote: false, residentItems: 0, runningStream: false, unresolvedTools: 0 };
 notePromptSubmission({ tabId: "tab-A", sessionId: "A", sessionGeneration: 0, promptId: "1", turnId: "turn-A", runtimeEpoch: "runtime-A", kind: "approval" }, "transport", "accepted");
 const result = await runSessionExport(input);
 const promptEvent = JSON.parse(exportedObservation).lifecycleDiagnostics.events.find((event: { action: string }) => event.action === "prompt-transport");
 assert.equal(promptEvent.status, "accepted", "session exports contain the actual approval transport outcome");
 assert.equal(promptEvent.prompt.bindingGeneration, 0, "export preserves the valid initial generation");
 assert.equal(result.text, text);
 assert.equal(finished, 1);
 blocked = true;
 const pending = runSessionExport(input);
 await readStarted;
 await cancelSessionExport("fixed-A");
 release();
 assert.equal((await pending).cancelled, true);
 assert.equal(finished, 1, "cancelled export is never published");
 assert.ok(cancelled >= 1);
 assert.equal(sessionExportProgress.getSnapshot().length, 0);
 console.log("session export operations: UTF-8 chunk boundaries, full clipboard and independent cancellation passed");
} finally { stub.uninstall(); dom.window.close(); }
