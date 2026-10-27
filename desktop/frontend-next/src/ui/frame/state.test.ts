import { expect, it } from "vitest";
import { FrameState, frameKey } from "./state";
import type { ExtensionSurface } from "../../port/wire";

const view: ExtensionSurface = { pluginId: "p", surfaceId: "s", sessionId: "session", generation: 1, kind: "frame", frame: { frame: "board", title: "Board", fallback: "Board" } };

it("preserves a move but retires withdrawn, expired and replaced documents", () => {
  const state = new FrameState(), key = frameKey(view);
  expect(state.opened(key)).toEqual({ reason: "publish" });
  state.save(key, { selected: "a" });
  state.retain([view]);
  expect(state.opened(key)).toEqual({ reason: "remount", state: { selected: "a" } });
  for (const next of [[], [{ ...view, code: "surface.expired" }], [{ ...view, generation: 2 }], [{ ...view, frame: { ...view.frame!, frame: "editor" } }]]) {
    state.save(key, { selected: "private" });
    state.retain(next);
    expect(state.opened(key)).toEqual({ reason: "publish" });
  }
});

it("does not share state across runtime owners even when every wire identity matches", () => {
  const a = new FrameState(), b = new FrameState(), key = frameKey(view);
  a.save(key, { private: "a" });
  expect(b.opened(key)).toEqual({ reason: "publish" });
  expect(a.scope).not.toBe(b.scope);
});
