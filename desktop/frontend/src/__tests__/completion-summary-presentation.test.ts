import assert from "node:assert/strict";
import { completionSummaryPresentation, normalizeCompletionSummary } from "../lib/completionSummary";
import { mergeTurnResult, normalizeTurnChanges, turnChangeText, turnCheckState } from "../lib/turnResult";
import { historicalResultNotice, withTurnResult, withRunningChecks } from "../lib/completionResultState";
import { ChatSource } from "../lib/chatViewSource";
import { initialState, reducer, type State, type Item } from "../lib/useController";
import { t } from "../lib/i18n";
import type { TurnChanges } from "../lib/types";

const diff: TurnChanges = { id: "0:1", turn: 0, coverage: "complete", files: [{ path: "a.ts", kind: "modify", added: 2, removed: 1 }], added: 2, removed: 1, reasons: [] };
const legacy = { ...mergeTurnResult(), mutations: 17, changed_files: 9 };
assert.equal(turnChangeText(legacy, t), "Change statistics unavailable");
assert.equal(turnCheckState(legacy).status, "unknown");
const receipt = { verdict: "partial", diff, verifications: [] };
const result = normalizeCompletionSummary(mergeTurnResult(legacy, receipt, "turn-0", 0));
assert.equal(result.checkpointTurn, 0);
assert.match(turnChangeText(result, t), /1.*file.*\+2 −1/);
assert.equal(turnCheckState(result).status, "none");
assert.equal(completionSummaryPresentation(result, "standard", t)?.title, "Turn result");
assert.match(turnChangeText(mergeTurnResult(undefined, { ...receipt, diff: { ...diff, coverage: "partial" } }), t), /partial/i);
assert.equal(normalizeTurnChanges({ ...diff, added: -1 })?.coverage, "unknown");
for (const [check, expected] of [
  [{ command: "test", passed: true, exitCode: 1 }, "failed"],
  [{ command: "test", passed: true, stale: true }, "stale"],
  [{ command: "test", passed: false, interrupted: true }, "interrupted"],
  [{ command: "test", passed: true, exitCode: 0 }, "passed"],
] as const) assert.equal(turnCheckState(mergeTurnResult(undefined, { ...receipt, verifications: [check] })).status, expected);
const user: Item = { kind: "user", id: "u0", text: "change" };
let state = withTurnResult({ ...initialState, items: [user], seq: 10 } as State, result);
const stableId = state.items[1].id;
state = withTurnResult(state, result);
assert.equal(state.items.length, 2);
assert.equal(state.items[1].id, stableId);
state = withTurnResult({ ...state, items: [...state.items, { ...user, id: "u1" }] }, { ...result, turnId: "turn-1" });
assert.equal(state.items.length, 4);
assert.equal(state.items[1].id, stableId);
const tool = (id: string): Item => ({ kind: "tool", id, name: "exec_command", args: '{"command":"go test ./..."}', readOnly: false, status: "running", verifying: true });
state = withRunningChecks({ ...state, items: [...state.items, tool("c1"), tool("c2")] });
assert.equal(state.completionSummary?.liveChecks?.length, 2);
state = withRunningChecks({ ...state, items: state.items.map(i => i.kind === "tool" && i.id === "c1" ? { ...i, status: "done" } : i) });
assert.equal(state.completionSummary?.checking, true, "one completed parallel check does not stop the other");
const history = historicalResultNotice({ role: "notice", content: "", completionReceipt: receipt, checkpointTurn: 0, turnId: "turn-0" }, "history");
assert.equal(history?.completionSummary?.receipt?.diff?.id, diff.id);
assert.equal(history?.completionSummary?.checkpointTurn, 0);
const answer: Item = { kind: "assistant", id: "a0", text: "done", reasoning: "", streaming: false };
const source = new ChatSource("completion-results");
source.update({ items: [user, history!, answer], running: false, hydrating: false, hasOlder: false, loadingOlder: false });
assert.equal(source.getNodeSnapshot("history")?.kind, "notice", "historical results survive as ordinary records");
source.dispose();
for (const phase of ["checking", "verifying", "working", "reviewing"]) {
  const plain: State = { ...initialState, items: [user], seq: 1 };
  const next = reducer(plain, { type: "event", e: { kind: "turn_phase", phase } });
  assert.equal(next.turnPhase, phase);
  assert.equal(next.completionSummary?.checking ?? false, false, `${phase} is not verification evidence`);
  assert.deepEqual(next.items, plain.items, `${phase} must not insert a result card`);

  const checking = withRunningChecks({ ...plain, items: [user, tool("check")] });
  const resultId = checking.items.find(i => i.kind === "notice")!.id;
  const active = reducer(checking, { type: "event", e: { kind: "turn_phase", phase } });
  assert.equal(active.completionSummary?.checking, true, `${phase} preserves real running checks`);
  assert.equal(active.items.find(i => i.kind === "notice")!.id, resultId);
}
let live = reducer({ ...initialState, items: [user] }, { type: "event", e: { kind: "tool_dispatch", tool: { id: "live", name: "bash", readOnly: false, args: '{"command":"go test ./..."}' } } });
live = reducer(live, { type: "event", e: { kind: "tool_progress", tool: { id: "live", name: "bash", readOnly: false, verifying: true, output: "running" } } });
assert.equal(live.completionSummary?.checking, true);
live = reducer(live, { type: "event", e: { kind: "turn_phase", phase: "working" } });
assert.equal(live.completionSummary?.checking, true);
live = reducer(live, { type: "event", e: { kind: "tool_result", tool: { id: "live", name: "bash", readOnly: false, output: "PASS" } } });
assert.equal(live.completionSummary?.checking, false);
console.log("turn result truth, stable identity, concurrent checks, phases and history passed");
