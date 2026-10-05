// Re-reading a session the stream cannot replay: on mount, on rebinding a pane,
// and on a gap. Two authorities answer it — the transcript is the record, the
// task list is the kernel's.

import { fromHistory, todoStep, type PlanStep, type SessionEvent } from "./session";
import type { AgentPort, HostTodo } from "../port/port";

/** The same mapping the streamed path uses, so the two ingest routes cannot
 *  disagree about a step's state. */
export function planFromHost(todos: HostTodo[]): PlanStep[] {
  return todos.map(todoStep);
}

// restoreSession reads both halves. The task list is not derivable from the
// transcript: complete_step advances it without writing a todo_write, and a
// refused todo_write is written there. An unreachable list is left to the
// reducer rather than blanked.
export async function restoreSession(port: AgentPort): Promise<SessionEvent> {
  const [msgs, todos] = await Promise.all([port.history(), port.todos().then(planFromHost).catch(() => undefined)]);
  return { kind: "__restore", ...fromHistory(msgs), plan: todos };
}

// refreshTodos re-reads only the list, for the points it can move with no
// transcript to rebuild from: a turn ending, a cancel the kernel recovered.
export function refreshTodos(port: AgentPort, dispatch: (ev: SessionEvent) => void): Promise<void> {
  return port
    .todos()
    .then((todos) => dispatch({ kind: "__todos", plan: planFromHost(todos) }))
    .catch(() => {});
}
