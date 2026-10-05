import { describe, expect, it } from "vitest";
import { fromHistory, initialState, reduce, type Item, type SessionEvent, type SessionState } from "./session";
import { refreshTodos, restoreSession } from "./restore";
import type { AgentPort, HistoryMessage, HostTodo } from "../port/port";

// The list as the model wrote it. Its arguments persist in the transcript; the
// kernel's own state moves independently of them.
const WROTE = JSON.stringify({
  todos: [
    { step_id: "n1", content: "item one", status: "in_progress" },
    { step_id: "n2", content: "item two", status: "pending" },
  ],
});

const history: HistoryMessage[] = [
  { role: "user", content: "walk the list with me" },
  { role: "assistant", content: "", toolCalls: [{ id: "t1", name: "todo_write", arguments: WROTE }] },
  { role: "tool", content: "Todos updated: 2 total", toolCallId: "t1" },
  { role: "assistant", content: "", toolCalls: [{ id: "c1", name: "complete_step", arguments: '{"step":"n1"}' }] },
  { role: "tool", content: "Signed off", toolCallId: "c1" },
];

function port(todos: HostTodo[] | Error): AgentPort {
  return {
    history: async () => history,
    todos: async () => {
      if (todos instanceof Error) throw todos;
      return todos;
    },
  } as unknown as AgentPort;
}

const run = (evs: SessionEvent[], from: SessionState = initialState) => evs.reduce(reduce, from);

describe("where the task panel gets its list", () => {
  // A rebuild must not reset the panel to the list as the model last wrote it:
  // a signed-off item would render as the current one, unmovable by any later
  // call.
  it("takes the kernel's list, advances and all", async () => {
    const ev = await restoreSession(port([
      { content: "item one", status: "completed" },
      { content: "item two", status: "in_progress" },
    ]));
    expect(run([ev]).plan).toEqual([
      { text: "item one", status: "completed", activeForm: undefined, level: undefined },
      { text: "item two", status: "in_progress", activeForm: undefined, level: undefined },
    ]);
  });

  // Replaying the writes alone loses every sign-off: complete_step advances the
  // list without writing a todo_write.
  it("does not re-derive the list from the transcript", async () => {
    const ev = await restoreSession(port([]));
    expect("plan" in ev && ev.plan).toEqual([]);
  });

  // A failed read is not a kernel with no plan, and blanking the panel would
  // state something the window does not know.
  it("leaves the panel alone when the list cannot be read", async () => {
    const shown = run([{ kind: "__todos", plan: [{ text: "item one", status: "pending" }] }]);
    const ev = await restoreSession(port(new Error("offline")));
    expect(run([ev], shown).plan).toEqual([{ text: "item one", status: "pending" }]);
  });

  // A refused list is still a call in the transcript and an event on the wire.
  // Its payload is backed by no host state, and nothing later contradicts it.
  it("ignores a todo_write the kernel refused", () => {
    const shown = run([{ kind: "__todos", plan: [{ text: "item one", status: "pending" }] }]);
    const after = run(
      [{ kind: "tool_result", tool: { id: "t9", name: "todo_write", args: WROTE, err: "rejected", readOnly: true } } as SessionEvent],
      shown,
    );
    expect(after.plan).toEqual([{ text: "item one", status: "pending" }]);
  });

  // A finished list is spent whichever ingest path carries it: the kernel keeps
  // it, and the two paths must not disagree about drawing it.
  it("draws a finished list as no list, from either side", async () => {
    const ev = await restoreSession(port([{ content: "item one", status: "completed" }]));
    expect(run([ev]).plan).toEqual([]);
  });

  it("re-reads only the list when asked", async () => {
    let got: SessionEvent | null = null;
    await refreshTodos(port([{ content: "item two", status: "in_progress" }]), (ev) => void (got = ev));
    expect(got).toEqual({
      kind: "__todos",
      plan: [{ text: "item two", status: "in_progress", activeForm: undefined, level: undefined }],
    });
  });
});

// Reopening a session had every reply attributed to whatever the composer is
// set to now, because only the live turn_started carried a model. The message
// carries it, so a transcript read a day later still says who answered.
describe("which model wrote a rebuilt reply", () => {
  it("comes from the message, not from the current setting", () => {
    const { items } = fromHistory([
      { role: "user", content: "hi", msgIndex: 0 },
      { role: "assistant", content: "hello", msgIndex: 1, modelRef: "yyds/claude-opus-4.8" },
    ]);
    const say = items.find((i) => i.t === "say") as Extract<Item, { t: "say" }>;
    expect(say.model).toBe("yyds/claude-opus-4.8");
  });
});
