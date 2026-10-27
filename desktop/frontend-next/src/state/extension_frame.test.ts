import { describe, expect, it } from "vitest";
import { foldExtension } from "./extension";
import { initialState } from "./session";
import type { ExtensionSurface } from "../port/wire";

const frame = (props: Record<string, unknown>): ExtensionSurface => ({
  pluginId: "board", surfaceId: "kanban", kind: "frame", frame: { frame: "board", title: "Board", fallback: "3 cards", props },
});

describe("a frame publication", () => {
  it("stands with the views and a republish replaces it in place", () => {
    let s = foldExtension(initialState, frame({ n: 1 }));
    s = foldExtension(s, frame({ n: 2 }));
    expect(s.views).toHaveLength(1);
    expect(s.views[0].frame?.props).toEqual({ n: 2 });
    expect(s.items.filter((i) => i.t === "extension")).toHaveLength(0);
  });

  it("leaves the views once it has expired", () => {
    let s = foldExtension(initialState, frame({ n: 1 }));
    s = foldExtension(s, { pluginId: "board", surfaceId: "kanban", kind: "frame", code: "surface.expired" });
    expect(s.views).toHaveLength(0);
  });
});
