import { describe, expect, it } from "vitest";
import type { TreeWorkspace } from "../port/hub";
import { isUnread, seen } from "./unread";

const book = (): TreeWorkspace[] => [
  { root: "/w", name: "acme", sessions: [{ path: "/a", name: "a", unread: true }, { path: "/b", name: "b" }] },
];

describe("seen", () => {
  it("hands back the same tree when nothing was cleared", () => {
    const tree = book();
    expect(seen(tree, "", new Set())).toBe(tree);
  });

  it("clears only the key of the host it belongs to", () => {
    const tree = book();
    expect(seen(tree, "", new Set(["gpu\n/a"]))).toBe(tree);
    expect(seen(tree, "gpu", new Set(["gpu\n/a"]))[0].sessions[0].unread).toBe(false);
    expect(seen(tree, "", new Set(["\n/a"]))[0].sessions[0].unread).toBe(false);
  });
});

describe("isUnread", () => {
  it("reads a remote pane from its own host's book and a missing field as read", () => {
    const rt = { id: "r", base: "", root: "/w", name: "w", sessionPath: "/a", host: "gpu" };
    expect(isUnread(rt, [], { gpu: book() })).toBe(true);
    expect(isUnread(rt, book(), {})).toBe(false);
    expect(isUnread({ ...rt, sessionPath: "/b" }, [], { gpu: book() })).toBe(false);
  });
});
