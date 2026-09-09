import assert from "node:assert/strict";
import { completionSummaryPresentation, normalizeCompletionSummary } from "../lib/completionSummary";
import { mergeTurnResult, normalizeTurnChanges, turnChangeText, turnCheckState } from "../lib/turnResult";
import { historicalResultNotice, withTurnResult, withRunningChecks } from "../lib/completionResultState";
import { partitionTurnItems } from "../lib/transcriptRows";
import { initialState, type State, type Item } from "../lib/useController";
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
const outside = partitionTurnItems([history!, answer]).flatMap(p => p.outsideItems);
assert.deepEqual(outside.map(i => i.id), ["a0", "history"], "sidecar placement preserves a result footer");
console.log("turn result truth, stable identity, concurrent checks and history passed");
