import { describe, expect, it } from "vitest";
import type { Item } from "../state/session";
import { foldEarlier, KEEP_RECENT } from "./activityEarlier";

const tool = (id: string, name: string, over: Record<string, unknown> = {}, running = false) =>
  ({ t: "tool", id, running, children: [], tool: { id, name, ...over } }) as unknown as Item;
const reads = (id: string, names: string[]) => ({ t: "reads", id, tools: names.map((name, i) => ({ id: `${id}${i}`, name })) }) as unknown as Item;

describe("folding the earlier steps of a long run of work", () => {
  it("leaves a short run alone", () => {
    const rows = Array.from({ length: KEEP_RECENT }, (_, i) => tool(`a${i}`, "read_file"));
    expect(foldEarlier(rows)).toEqual({ earlier: [], recent: rows, kinds: [] });
  });

  it("keeps the last six and folds the rest, counting each kind", () => {
    const rows = [
      reads("r", ["read_file", "read_file", "grep"]),
      tool("e", "edit_file"), tool("b", "bash"),
      ...Array.from({ length: KEEP_RECENT }, (_, i) => tool(`k${i}`, "read_file")),
    ];
    const fold = foldEarlier(rows);
    expect(fold.recent).toHaveLength(KEEP_RECENT);
    expect(fold.earlier.map((i) => i.id)).toEqual(["r", "e", "b"]);
    expect(fold.kinds).toEqual([["读取", 2], ["搜索", 1], ["编辑", 1], ["运行命令", 1]]);
  });

  it("never folds a failure or a step still running, however old", () => {
    const rows = [
      tool("bad", "bash", { err: "exit 1" }), tool("live", "bash", {}, true), tool("old", "read_file"),
      ...Array.from({ length: KEEP_RECENT }, (_, i) => tool(`k${i}`, "read_file")),
    ];
    const fold = foldEarlier(rows);
    expect(fold.earlier.map((i) => i.id)).toEqual(["old"]);
    expect(fold.recent.map((i) => i.id)).toEqual(["bad", "live", ...Array.from({ length: KEEP_RECENT }, (_, i) => `k${i}`)]);
  });
});
