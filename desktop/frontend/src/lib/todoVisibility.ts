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
): boolean {
  if (!todoKey || todos.length === 0) return false;
  if (hasIncompleteTodos(todos)) return true;
  return todoKey !== dismissedTodoKey;
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
