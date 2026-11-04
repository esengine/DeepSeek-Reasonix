import { describe, expect, it } from "vitest";
import { initialState, reduce, type Item, type SessionEvent } from "./session";

const run = (events: SessionEvent[]) => events.reduce(reduce, initialState);
const list = (...status: string[]) => JSON.stringify({ todos: status.map((s, i) => ({ content: `step ${i + 1}`, status: s })) });
const wrote = (id: string, args: string) => ({ kind: "tool_result", tool: { id, name: "todo_write", readOnly: true, output: "ok", args } }) as SessionEvent;
const gate = { kind: "approval_request", approval: { id: "a1", tool: "exit_plan_mode", subject: "", kind: "plan" } } as SessionEvent;
const started = { kind: "notice", code: "decision_receipt", decisionReceipt: { id: "a1", kind: "plan", outcome: "start_execution" } } as SessionEvent;
const card = (items: Item[]) => items.find((it): it is Extract<Item, { t: "approval" }> => it.t === "approval")!;

// The pane lets a finished list go at the turn's end; the card that ran it is
// where the finished list is still read, so it is kept there as it arrived.
describe("the list a started plan's card keeps", () => {
  it("is the last one the kernel reported, after the pane lets a spent plan go", () => {
    const s = run([{ kind: "turn_started" } as SessionEvent, gate, started, wrote("t1", list("completed", "in_progress")), wrote("t2", list("completed", "completed")), { kind: "turn_done" } as SessionEvent]);
    expect(s.plan).toEqual([]);
    expect(card(s.items).steps?.map((x) => x.status)).toEqual(["completed", "completed"]);
  });

  it("is nothing for a plan nobody started, and follows the canonical re-read too", () => {
    expect(card(run([gate, wrote("t1", list("in_progress"))]).items).steps).toBeUndefined();
    const s = run([gate, started, { kind: "__todos", plan: [{ text: "only", status: "completed" }] } as SessionEvent]);
    expect(card(s.items).steps).toEqual([{ text: "only", status: "completed" }]);
  });
});
