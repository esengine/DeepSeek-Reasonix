import type { ToolRegistry } from "../tools.js";

export type TodoStatus = "pending" | "in_progress" | "completed";

export interface TodoItem {
  content: string;
  status: TodoStatus;
  activeForm: string;
}

export interface TodoToolOptions {
  onTodosUpdated?: (todos: TodoItem[]) => void;
}

const DESCRIPTION =
  "In-session task tracker for 3+ step work. NOT a plan — no approval gate, no checkpoint, no files touched. Each call REPLACES the entire list (set semantics) — pass the FULL list. Exactly one item may be in_progress at a time; flip to completed the moment that step's done. Pass `[]` to clear. For approval gates use submit_plan; for branching choices use ask_choice.";

function validateTodos(raw: unknown): TodoItem[] {
  if (!Array.isArray(raw)) {
    throw new Error("todo_write: `todos` must be an array");
  }
  const out: TodoItem[] = [];
  let inProgressCount = 0;
  for (let i = 0; i < raw.length; i++) {
    const entry = raw[i];
    if (!entry || typeof entry !== "object") {
      throw new Error(`todo_write: todo #${i + 1} must be an object`);
    }
    const e = entry as Record<string, unknown>;
    const content = typeof e.content === "string" ? e.content.trim() : "";
    const activeForm = typeof e.activeForm === "string" ? e.activeForm.trim() : "";
    const status = e.status;
    if (!content) {
      throw new Error(`todo_write: todo #${i + 1} \`content\` must be a non-empty string`);
    }
    if (!activeForm) {
      throw new Error(`todo_write: todo #${i + 1} \`activeForm\` must be a non-empty string`);
    }
    if (status !== "pending" && status !== "in_progress" && status !== "completed") {
      throw new Error(
        `todo_write: todo #${i + 1} \`status\` must be one of pending|in_progress|completed (got ${JSON.stringify(status)})`,
      );
    }
    if (status === "in_progress") {
      inProgressCount++;
      if (inProgressCount > 1) {
        throw new Error(
          "todo_write: at most one todo may be in_progress at a time — mark the previous one completed first",
        );
      }
    }
    out.push({ content, status, activeForm });
  }
  return out;
}

function renderTodos(todos: TodoItem[]): string {
  if (todos.length === 0) return "todos cleared (0 items)";
  let done = 0;
  let inProgress = 0;
  let pending = 0;
  for (const t of todos) {
    if (t.status === "completed") done++;
    else if (t.status === "in_progress") inProgress++;
    else pending++;
  }
  const header = `todos updated · ${done} done · ${inProgress} in progress · ${pending} pending`;
  const lines = todos.map((t) => {
    if (t.status === "completed") return `[x] ${t.content}`;
    if (t.status === "in_progress") return `[>] ${t.activeForm}`;
    return `[ ] ${t.content}`;
  });
  return `${header}\n${lines.join("\n")}`;
}

export function registerTodoTool(registry: ToolRegistry, opts: TodoToolOptions = {}): ToolRegistry {
  registry.register({
    name: "todo_write",
    description: DESCRIPTION,
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        todos: {
          type: "array",
          description:
            "The COMPLETE new todo list. Replaces whatever was there before. Pass [] to clear.",
          items: {
            type: "object",
            properties: {
              content: {
                type: "string",
                description: 'Imperative step description, e.g. "Add tests for parser".',
              },
              status: {
                type: "string",
                enum: ["pending", "in_progress", "completed"],
                description: "Current state. Exactly one item may be in_progress.",
              },
              activeForm: {
                type: "string",
                description: 'Gerund form shown while in_progress, e.g. "Adding tests for parser".',
              },
            },
            required: ["content", "status", "activeForm"],
          },
        },
      },
      required: ["todos"],
    },
    fn: async (args: { todos: unknown }) => {
      const todos = validateTodos(args?.todos);
      opts.onTodosUpdated?.(todos);
      return renderTodos(todos);
    },
  });
  return registry;
}
