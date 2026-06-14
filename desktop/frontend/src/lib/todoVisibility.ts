import type { Todo } from "./tools";

export function shouldShowTodoPanel(
  todoId: string | null | undefined,
  dismissedTodoId: string | null,
  todos: Todo[],
): boolean {
  if (!todoId || todoId === dismissedTodoId || todos.length === 0) return false;
  // Hide the panel when every todo item is completed — the user no longer
  // needs to see the list, and after a restart the panel stays gone.
  return todos.some((t) => t.status !== "completed");
}
