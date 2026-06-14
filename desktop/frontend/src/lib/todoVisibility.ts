import type { Todo } from "./tools";

export function shouldShowTodoPanel(
  todoId: string | null | undefined,
  dismissedTodoId: string | null,
  todos: Todo[],
): boolean {
  return !!todoId && todoId !== dismissedTodoId && todos.length > 0;
}
