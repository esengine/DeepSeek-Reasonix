import type { Todo } from "./tools";

export interface TodoPanelSourceItem {
  kind?: string;
  id?: string;
  name?: string;
  args?: string;
  status?: string;
  error?: unknown;
  parentId?: string;
}

export interface TodoPanelState {
  id: string;
  todos: Todo[];
}

export function shouldShowTodoPanel(
  todoId: string | null | undefined,
  dismissedTodoId: string | null,
  todos: Todo[],
): boolean {
  return !!todoId && todoId !== dismissedTodoId && hasOpenTodos(todos);
}

export function deriveTodoPanelState(items: readonly TodoPanelSourceItem[]): TodoPanelState | null {
  let panel: TodoPanelState | null = null;
  let baseIndex = -1;
  for (let i = items.length - 1; i >= 0; i -= 1) {
    const item = items[i];
    if (!isSuccessfulTopLevelTool(item)) continue;
    if (item.name === "todo_write") {
      const todos = parseTodosFromArgs(item.args);
      panel = item.id && todos.length > 0 ? { id: item.id, todos } : null;
      baseIndex = i;
      break;
    }
  }
  if (!panel) return null;
  for (let i = baseIndex + 1; i < items.length; i += 1) {
    const item = items[i];
    if (isSuccessfulTopLevelTool(item) && item.name === "complete_step") {
      const step = parseStepFromArgs(item.args);
      if (!step) continue;
      panel = { ...panel, todos: advanceTodos(panel.todos, step) };
    }
  }
  return panel;
}

function isSuccessfulTopLevelTool(item: TodoPanelSourceItem): boolean {
  return item.kind === "tool" && !item.parentId && item.status === "done" && !item.error;
}

function hasOpenTodos(todos: readonly Todo[]): boolean {
  return todos.some((todo) => todoStatus(todo.status) !== "completed");
}

function parseTodosFromArgs(args: string | undefined): Todo[] {
  const parsed = parseObject(args);
  if (!Array.isArray(parsed.todos)) return [];
  const todos: Todo[] = [];
  for (const raw of parsed.todos) {
    if (!raw || typeof raw !== "object") continue;
    const item = raw as Record<string, unknown>;
    if (typeof item.content !== "string") continue;
    const todo: Todo = {
      content: item.content.trim(),
      status: typeof item.status === "string" ? item.status.trim() : "",
    };
    if (typeof item.activeForm === "string") todo.activeForm = item.activeForm.trim();
    if (typeof item.level === "number") todo.level = item.level;
    todos.push(todo);
  }
  return todos;
}

function parseStepFromArgs(args: string | undefined): string {
  const parsed = parseObject(args);
  return typeof parsed.step === "string" ? parsed.step.trim() : "";
}

function parseObject(args: string | undefined): Record<string, unknown> {
  if (!args) return {};
  try {
    const parsed = JSON.parse(args) as unknown;
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

function advanceTodos(todos: readonly Todo[], step: string): Todo[] {
  const index = matchTodoIndex(step, todos);
  if (index < 0 || todoStatus(todos[index]?.status) === "completed") return [...todos];
  const next = todos.map((todo, i) => (i === index ? { ...todo, status: "completed" } : { ...todo }));
  promoteNextPendingTodo(next);
  return next;
}

function promoteNextPendingTodo(todos: Todo[]) {
  if (todos.some((todo) => todoStatus(todo.status) === "in_progress")) return;
  const next = todos.find((todo) => todoStatus(todo.status) === "pending");
  if (next) next.status = "in_progress";
}

function matchTodoIndex(step: string, todos: readonly Todo[]): number {
  const numeric = parseStepIndex(normalizeStepText(step));
  if (numeric >= 1 && numeric <= todos.length) return numeric - 1;
  for (let i = 0; i < todos.length; i += 1) {
    const todo = todos[i];
    if (sameStepText(step, todo.content) || sameStepText(step, todo.activeForm ?? "")) return i;
  }

  const norm = normalizeStepText(step);
  let found = -1;
  for (let i = 0; i < todos.length; i += 1) {
    const todo = todos[i];
    if (stepTextContains(norm, normalizeStepText(todo.content)) || stepTextContains(norm, normalizeStepText(todo.activeForm ?? ""))) {
      if (found >= 0 && found !== i) return -1;
      found = i;
    }
  }
  return found;
}

function parseStepIndex(step: string): number {
  const trimmed = step.trim().replace(/\.$/, "").trim();
  if (!/^\d+$/.test(trimmed)) return -1;
  return Number.parseInt(trimmed, 10);
}

function todoStatus(status: unknown): string {
  return typeof status === "string" && status.trim() ? status.trim() : "pending";
}

function sameStepText(a: string, b: string): boolean {
  const na = normalizeStepText(a);
  const nb = normalizeStepText(b);
  return na !== "" && na === nb;
}

function normalizeStepText(value: string): string {
  let out = "";
  for (const char of value) {
    const code = char.codePointAt(0) ?? 0;
    out += code >= 0xff01 && code <= 0xff5e ? String.fromCodePoint(code - 0xfee0) : char;
  }
  return out.split(/\s+/).join("").toLowerCase();
}

function stepTextContains(a: string, b: string): boolean {
  if (!a || !b) return false;
  const short = [...a].length < [...b].length ? a : b;
  if ([...short].length < 6) return false;
  return a.includes(b) || b.includes(a);
}
