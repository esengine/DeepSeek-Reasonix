import { describe, expect, it } from "vitest";
import type { Tool } from "../port/wire";
import { foldTool } from "./fold";
import type { Item } from "./session_types";

const tool = (id: string, name: string, parentId?: string): Tool => ({ id, name, parentId, readOnly: true });
const run = (events: Tool[]): Item[] => events.reduce<Item[]>((items, t) => foldTool(items, t, true), []);

describe("a delegate's calls stay inside the card that spawned them", () => {
  it("nests a task's own call under the task", () => {
    const items = run([tool("task", "use_capability"), tool("task/ls", "ls", "task")]);
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ t: "tool", children: [{ id: "task/ls" }] });
  });

  it("keeps a fleet worker's call out of the main list", () => {
    const items = run([
      tool("fl", "use_capability"),
      tool("fl/fleet-1", "task", "fl"),
      tool("fl/fleet-1/read", "read_file", "fl/fleet-1"),
    ]);
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ t: "tool" });
    const kids = (items[0] as Extract<Item, { t: "tool" }>).children;
    expect(kids.map((c) => c.id)).toEqual(["fl/fleet-1", "fl/fleet-1/read"]);
  });
});
