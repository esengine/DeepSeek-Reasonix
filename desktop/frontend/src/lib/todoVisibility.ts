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
  if (!todoKey || todos.length === 0) return false;
  if (hasIncompleteTodos(todos)) return true;
  return todoKey !== dismissedTodoKey;
}

function todoStatus(status: unknown): string {
  const normalized = String(status ?? "").trim();
  return normalized || "pending";
}

function hasIncompleteTodos(todos: Todo[]): boolean {
  return todos.some((todo) => todoStatus(todo.status) !== "completed");
}
