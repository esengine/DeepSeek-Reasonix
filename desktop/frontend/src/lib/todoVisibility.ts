import type { Todo } from "./tools";

export function todoDismissalKey(todos: Todo[]): string {
  if (todos.length === 0) return "";
  return JSON.stringify(todos.map((todo) => ({
    content: String(todo.content ?? ""),
    status: todoStatus(todo.status),
    activeForm: String(todo.activeForm ?? ""),
    level: typeof todo.level === "number" ? todo.level : 0,
  })));
}

export function shouldShowTodoPanel(
  todoKey: string | null | undefined,
  dismissedTodoKey: string | null,
  todos: Todo[],
): boolean {
  return !!todoKey && todoKey !== dismissedTodoKey && todos.some((todo) => todoStatus(todo.status) !== "completed");
}

function todoStatus(status: unknown): string {
  const normalized = String(status ?? "").trim();
  return normalized || "pending";
}
