import { initialState, reducer } from "../lib/useController";

function equal(actual: unknown, expected: unknown, message: string) {
  if (actual !== expected) throw new Error(`${message}: got ${String(actual)}, want ${String(expected)}`);
}

let state = reducer(initialState, { type: "event", e: { kind: "turn_status", turnId: "turn-1", status: "queued" } });
equal(state.activeTurnId, "turn-1", "queued lifecycle admits the exact turn id");
state = reducer(state, { type: "event", e: { kind: "turn_started", turnId: "turn-1", status: "in_progress" } });
equal(state.currentAssistant, "a:turn-1:0", "first assistant sampling segment has a stable turn-local identity");
state = reducer(state, { type: "event", e: { kind: "ask_request", turnId: "turn-1", itemId: "ask-1", ask: { id: "ask-1", questions: [] } } });
const stale = reducer(state, { type: "event", e: { kind: "prompt_answered", turnId: "turn-old", itemId: "ask-1", status: "in_progress" } });
equal(stale.ask?.id, "ask-1", "a stale turn cannot answer the prompt");
state = reducer(state, { type: "event", e: { kind: "prompt_answered", turnId: "turn-1", itemId: "ask-1", status: "in_progress" } });
equal(state.ask, undefined, "the exact prompt answer resumes its turn");
state = reducer(state, { type: "event", e: { kind: "text", turnId: "turn-1", text: "partial" } });
state = reducer(state, { type: "event", e: { kind: "turn_done", turnId: "turn-1", status: "interrupted" } });
equal(state.activeTurnId, undefined, "the terminal event releases the lane");
if (!state.items.some((item) => item.kind === "notice" && item.text.includes("interrupted"))) {
  throw new Error("interruption must preserve partial output with an explicit notice");
}

let empty = reducer(initialState, { type: "event", e: { kind: "turn_started", turnId: "turn-empty", status: "in_progress" } });
empty = reducer(empty, { type: "event", e: { kind: "turn_done", turnId: "turn-empty", status: "interrupted" } });
equal(empty.items.some((item) => item.kind === "assistant"), false, "cancelling an empty turn removes its placeholder segment");

console.log("turn lifecycle reducer tests passed");
