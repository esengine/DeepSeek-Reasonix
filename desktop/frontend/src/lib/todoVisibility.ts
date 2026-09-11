import type { Todo } from "./tools";

export interface TodoPanelScopeInput {
  activeTabId?: string | null;
  activeTab?: {
    id?: string | null;
    scope?: string | null;
    workspaceRoot?: string | null;
    topicId?: string | null;
    sessionPath?: string | null;
  } | null;
  eventChannel?: string | null;
}

export type TodoPresentationStatus = "pending" | "in_progress" | "waiting" | "paused" | "completed";

export interface TodoRuntimePresentation {
  running: boolean;
  pendingPrompt: boolean;
}

export function todoPresentationStatus(
  status: Todo["status"],
  runtime: TodoRuntimePresentation,
): TodoPresentationStatus {
  switch (todoStatus(status)) {
    case "completed":
      return "completed";
    case "in_progress":
      if (runtime.pendingPrompt) return "waiting";
      return runtime.running ? "in_progress" : "paused";
    default:
      return "pending";
  }
}

export function todoContinueTarget(
  targetTabId: string | null | undefined,
  activeTabId: string | null | undefined,
  runtime: TodoRuntimePresentation & { ready: boolean; readOnly?: boolean },
): string | null {
  const target = String(targetTabId ?? "").trim();
  if (!target || target !== String(activeTabId ?? "").trim()) return null;
  if (!runtime.ready || runtime.readOnly || runtime.running || runtime.pendingPrompt) return null;
  return target;
}

export function resolveTodoPanelTodos(
  canonical: Todo[] | null | undefined,
  live?: Todo[] | null,
): Todo[] {
  // `live` is set only when the transcript has a completed top-level todo_write.
  // Prefer it over MetaForTab — meta only refreshes on turn_done / focus change,
  // so mid-turn status flips otherwise freeze until the user switches tabs (#7642).
  if (live !== undefined && live !== null) return live;
  return Array.isArray(canonical) ? canonical : [];
}

export function sameTodoList(a: Todo[] | null | undefined, b: Todo[] | null | undefined): boolean {
  if (a === b) return true;
  if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return false;
  return a.every((todo, index) => {
    const other = b[index];
    return (
      todo.content === other.content &&
      todo.status === other.status &&
      todo.activeForm === other.activeForm &&
      todo.level === other.level
    );
  });
}

export function todoDismissalKey(todos: Todo[]): string {
  if (todos.length === 0) return "";
  return JSON.stringify(todos.map((todo) => ({
    content: String(todo.content ?? ""),
    status: todoStatus(todo.status),
    activeForm: String(todo.activeForm ?? ""),
    level: typeof todo.level === "number" ? todo.level : 0,
  })));
}

export function todoPanelScope({ activeTab, activeTabId, eventChannel }: TodoPanelScopeInput): string {
  const tabId = String(activeTabId ?? "").trim();
  const tab = !tabId || activeTab?.id === tabId ? activeTab : null;
  const sessionPath = tab?.sessionPath?.trim();
  if (sessionPath) return `session:${sessionPath}`;
  if (tabId) return `tab:${tabId}`;
  const topicId = tab?.topicId?.trim();
  if (tab && topicId) return `topic:${tab.scope ?? ""}:${tab.workspaceRoot ?? ""}:${topicId}`;
  const channel = String(eventChannel ?? "").trim();
  return channel ? `event:${channel}` : "";
}

export function scopedTodoDismissalKey(scope: string | null | undefined, todoKey: string | null | undefined): string {
  const key = String(todoKey ?? "").trim();
  if (!key) return "";
  const prefix = String(scope ?? "").trim();
  return prefix ? `${prefix}\0${key}` : key;
}

export function dismissedTodoKeyForScope(
  scope: string | null | undefined,
  dismissedKeys: ReadonlySet<string> | null | undefined,
  todoKey: string | null | undefined,
): string | null {
  const scopedKey = scopedTodoDismissalKey(scope, todoKey);
  if (!scopedKey || !dismissedKeys?.has(scopedKey)) return null;
  return todoKey ?? null;
}

export function todoBatchKey(todos: Todo[]): string {
  if (todos.length === 0) return "";
  return JSON.stringify(todos.map((todo) => ({
    content: String(todo.content ?? ""),
    level: typeof todo.level === "number" ? todo.level : 0,
  })));
}

export function scopedTodoBatchKey(scope: string | null | undefined, batchKey: string | null | undefined): string {
  const key = String(batchKey ?? "").trim();
  if (!key) return "";
  const prefix = String(scope ?? "").trim();
  return prefix ? `${prefix}\0${key}` : key;
}

export function shouldShowTodoPanel(
  todoKey: string | null | undefined,
  dismissedTodoKey: string | null,
  todos: Todo[],
  persisted?: { batchKey?: string | null; batches?: readonly string[] | null },
): boolean {
  if (!todoKey || todos.length === 0) return false;
  if (hasIncompleteTodos(todos)) return true;
  if (todoKey === dismissedTodoKey) return false;
  const batchKey = String(persisted?.batchKey ?? "").trim();
  if (batchKey && persisted?.batches?.includes(batchKey)) return false;
  return true;
}

export function sameStringList(a?: readonly string[] | null, b?: readonly string[] | null): boolean {
  if (a === b) return true;
  const left = Array.isArray(a) ? a : [];
  const right = Array.isArray(b) ? b : [];
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

export function shouldOpenTodoPanelByDefault(): boolean {
  return false;
}

function todoStatus(status: unknown): string {
  const normalized = String(status ?? "").trim();
  return normalized || "pending";
}

function hasIncompleteTodos(todos: Todo[]): boolean {
  return todos.some((todo) => todoStatus(todo.status) !== "completed");
}
