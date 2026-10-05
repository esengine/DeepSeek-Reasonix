import { describe, expect, it } from "vitest";
import { newlyDone, removeHint } from "./Workspaces";

describe("removeHint", () => {
  it("says only what is true when nothing is open", () => {
    expect(removeHint(0, 0)).toBe("不会删除任何文件");
  });

  it("counts the panes the removal will close", () => {
    expect(removeHint(3, 0)).toBe("将先关闭 3 个面板；不会删除任何文件");
  });

  it("says a running turn holds the removal instead of offering to stop it", () => {
    expect(removeHint(3, 2)).toBe("其中 2 个对话正在运行，停止后才能移除");
  });
});

describe("the completion a window watched", () => {
  const done = { run: "done", live: false };
  const running = { run: "running", live: true };

  it("does not call a row done merely because the window just opened", () => {
    // The observable this guards: a session finished yesterday is "done" the
    // moment the rail mounts, so a first sighting must record, not announce.
    const seen: Record<string, string> = {};
    expect(newlyDone(seen, { pane: done })).toEqual([]);
  });

  it("announces the run that was running and then finished", () => {
    const seen: Record<string, string> = {};
    newlyDone(seen, { pane: running });
    expect(newlyDone(seen, { pane: done })).toEqual(["pane"]);
  });

  it("announces it once, not on every later report", () => {
    const seen: Record<string, string> = {};
    newlyDone(seen, { pane: running });
    expect(newlyDone(seen, { pane: done })).toEqual(["pane"]);
    expect(newlyDone(seen, { pane: done })).toEqual([]);
  });

  it("records a freshly opened pane without announcing its history", () => {
    const seen: Record<string, string> = { other: "running" };
    expect(newlyDone(seen, { other: running, opened: done })).toEqual([]);
    expect(seen.opened).toBe("done");
  });

  it("keeps a rail that switches scope from replaying every finished row", () => {
    const seen: Record<string, string> = {};
    newlyDone(seen, { a: done, b: done, c: done });
    expect(newlyDone(seen, { a: done, b: done, c: done, d: done })).toEqual([]);
  });

  it("counts a halt that becomes done, because the turn did land", () => {
    const seen: Record<string, string> = {};
    newlyDone(seen, { pane: { run: "halt", live: false } });
    expect(newlyDone(seen, { pane: done })).toEqual(["pane"]);
  });
});
