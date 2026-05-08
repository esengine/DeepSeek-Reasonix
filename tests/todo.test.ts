import { describe, expect, it, vi } from "vitest";
import { ToolRegistry } from "../src/tools.js";
import { type TodoItem, registerTodoTool } from "../src/tools/todo.js";

function setup(): { tools: ToolRegistry; updates: TodoItem[][] } {
  const updates: TodoItem[][] = [];
  const tools = new ToolRegistry();
  registerTodoTool(tools, { onTodosUpdated: (t) => updates.push(t) });
  return { tools, updates };
}

describe("todo_write — lightweight in-session task tracker", () => {
  it("registers as readOnly so plan mode allows it", () => {
    const tools = new ToolRegistry();
    registerTodoTool(tools);
    expect(tools.get("todo_write")?.readOnly).toBe(true);
  });

  it("accepts a well-formed todo list and echoes the rendered state", async () => {
    const { tools, updates } = setup();
    const out = await tools.dispatch(
      "todo_write",
      JSON.stringify({
        todos: [
          { content: "Read the spec", activeForm: "Reading the spec", status: "completed" },
          {
            content: "Sketch the parser",
            activeForm: "Sketching the parser",
            status: "in_progress",
          },
          { content: "Write tests", activeForm: "Writing tests", status: "pending" },
        ],
      }),
    );
    expect(out).toContain("1 done");
    expect(out).toContain("1 in progress");
    expect(out).toContain("1 pending");
    expect(out).toContain("[x] Read the spec");
    expect(out).toContain("[>] Sketching the parser");
    expect(out).toContain("[ ] Write tests");
    expect(updates).toHaveLength(1);
    expect(updates[0]).toHaveLength(3);
  });

  it("allows an empty list to clear the tracker", async () => {
    const { tools, updates } = setup();
    const out = await tools.dispatch("todo_write", JSON.stringify({ todos: [] }));
    expect(out).toMatch(/cleared/);
    expect(updates[0]).toEqual([]);
  });

  it("refuses two todos with status:in_progress at once", async () => {
    const { tools } = setup();
    const out = await tools.dispatch(
      "todo_write",
      JSON.stringify({
        todos: [
          { content: "A", activeForm: "Doing A", status: "in_progress" },
          { content: "B", activeForm: "Doing B", status: "in_progress" },
        ],
      }),
    );
    expect(out).toMatch(/at most one/);
  });

  it("refuses an unknown status value", async () => {
    const { tools } = setup();
    const out = await tools.dispatch(
      "todo_write",
      JSON.stringify({
        todos: [{ content: "A", activeForm: "Doing A", status: "blocked" }],
      }),
    );
    expect(out).toMatch(/status/);
    expect(out).toMatch(/pending\|in_progress\|completed/);
  });

  it("refuses an empty content string", async () => {
    const { tools } = setup();
    const out = await tools.dispatch(
      "todo_write",
      JSON.stringify({
        todos: [{ content: "  ", activeForm: "Doing it", status: "pending" }],
      }),
    );
    expect(out).toMatch(/content/);
    expect(out).toMatch(/non-empty/);
  });

  it("refuses an empty activeForm", async () => {
    const { tools } = setup();
    const out = await tools.dispatch(
      "todo_write",
      JSON.stringify({
        todos: [{ content: "Step", activeForm: "", status: "pending" }],
      }),
    );
    expect(out).toMatch(/activeForm/);
  });

  it("refuses a non-array todos field", async () => {
    const { tools } = setup();
    const out = await tools.dispatch("todo_write", JSON.stringify({ todos: "not-an-array" }));
    expect(out).toMatch(/must be an array/);
  });

  it("does not invoke onTodosUpdated when validation fails", async () => {
    const cb = vi.fn();
    const tools = new ToolRegistry();
    registerTodoTool(tools, { onTodosUpdated: cb });
    await tools.dispatch(
      "todo_write",
      JSON.stringify({
        todos: [{ content: "A", activeForm: "Doing A", status: "blocked" }],
      }),
    );
    expect(cb).not.toHaveBeenCalled();
  });
});
