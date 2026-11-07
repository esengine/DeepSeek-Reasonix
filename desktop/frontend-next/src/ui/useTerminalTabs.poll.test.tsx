// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import type { TerminalInfo } from "../port/terminal";
import { useTerminalTabs } from "./useTerminalTabs";
import "./testkit";

beforeEach(() => vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] }));
afterEach(() => { cleanup(); vi.useRealTimers(); });

const info = (id: string): TerminalInfo => ({ id, name: "Git Bash", shell: "/usr/bin/bash", dir: "~", cols: 80, rows: 24, running: true, busy: false, created: "2026-11-07T00:00:00Z" });

// React runs the component again for a state update even when the value it is
// handed equals the one it holds, once the component has had a real update. A
// poll that finds nothing new must therefore not call the setter at all.
it("does not render again when a poll finds the same terminals", async () => {
  const port = new MockPort();
  const listing = vi.spyOn(port, "terminals");
  listing.mockResolvedValueOnce([]).mockResolvedValue([info("t1")]);
  let renders = 0;
  renderHook(() => {
    renders += 1;
    return useTerminalTabs({ port, shown: true, pick: () => {}, setDismissed: () => {}, onExternal: () => {}, onError: () => {}, request: 0 });
  });
  await act(async () => {});
  await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
  const afterChange = renders;
  expect(listing).toHaveBeenCalledTimes(2);
  await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
  await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
  expect(listing).toHaveBeenCalledTimes(4);
  expect(renders).toBe(afterChange);
});
