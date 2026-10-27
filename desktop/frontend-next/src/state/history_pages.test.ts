import { expect, it } from "vitest";
import type { HistoryMessage } from "../port/session";
import { fromHistory, initialState, reduce } from "./session";

const messages: HistoryMessage[] = [
  { role: "system", content: "system", msgIndex: 0 },
  { role: "user", content: "question", msgIndex: 1, createdAt: 123 },
  { role: "assistant", content: "", msgIndex: 2, toolCalls: [{ id: "a", name: "read_file", arguments: '{"path":"a"}' }, { id: "b", name: "read_file", arguments: '{"path":"b"}' }] },
  { role: "tool", content: "A", toolCallId: "a", msgIndex: 3 },
  { role: "tool", content: "B", toolCallId: "b", msgIndex: 4 },
  { role: "assistant", content: "done", msgIndex: 5, workDurationMs: 100 },
];

it("merges a tool result page with its earlier calls without duplicates or lost outputs", () => {
  const tail = fromHistory(messages.slice(3), { snapshot: "snapshot", turnStartedAt: 123,
    toolContext: messages[2].toolCalls!.map((call, ordinal) => ({ call, ordinal, messageIndex: 2 })) });
  const older = fromHistory(messages.slice(0, 3), { snapshot: "snapshot" });
  let state = reduce(initialState, { kind: "__restore", ...tail });
  state = reduce(state, { kind: "__history_prepend", ...older });
  expect(state.items).toEqual(fromHistory(messages, { snapshot: "snapshot" }).items);
  expect(state.entranceOwed).toEqual([]);
  expect(reduce(state, { kind: "__history_prepend", ...older }).items).toEqual(state.items);
});

it("keeps completed live replies, running output and prompts when prepending history", () => {
  const live = { t: "say" as const, id: "live", text: "still writing", done: false };
  const completed = { t: "say" as const, id: "completed", text: "newer reply", done: true };
  const prompt = { t: "approval" as const, id: "prompt", a: { id: "approval" } as never };
  const state = { ...initialState, running: true, items: [completed, live, prompt] };
  const result = reduce(state, { kind: "__history_prepend", ...fromHistory(messages, { snapshot: "snapshot" }) });
  expect(result.items.slice(-3)).toEqual([completed, live, prompt]);
  expect(result.running).toBe(true);
  expect(result.metrics).toBe(state.metrics);
});
