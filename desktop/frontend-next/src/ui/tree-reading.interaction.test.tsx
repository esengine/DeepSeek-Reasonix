// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { TreeReading, Workspaces } from "./Workspaces";

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function draw(treeRead: boolean) {
  return (
    <Workspaces
      hub={{} as never}
      tree={[]}
      treeRead={treeRead}
      runtimes={[]}
      active=""
      folded={new Set()}
      onFold={() => {}}
      reload={async () => {}}
      onOpen={async () => {}}
      onFocus={() => {}}
      onClose={async () => {}}
      liveIds={() => []}
      runs={{}}
      onRename={() => {}}
      onError={() => {}}
      adder={{ add: () => {}, close: () => {}, at: null } as never}
    />
  );
}

it("counts the seconds the first tree read has been pending", () => {
  render(draw(false));
  expect(screen.getByText("正在读取文件夹…")).toBeTruthy();
  expect(screen.queryByText(/^\ds$/)).toBeNull();
  act(() => vi.advanceTimersByTime(3000));
  expect(screen.getByText("3s")).toBeTruthy();
  act(() => vi.advanceTimersByTime(2000));
  expect(screen.getByText("5s")).toBeTruthy();
});

it("keeps the ticking seconds out of the status announcement", () => {
  render(draw(false));
  act(() => vi.advanceTimersByTime(3000));
  const status = screen.getByRole("status");
  expect(screen.getByText("3s").getAttribute("aria-hidden")).toBe("true");
  expect(status.contains(screen.getByText("3s"))).toBe(true);
});

it("drops the clock once the read settles", () => {
  const { rerender } = render(draw(false));
  act(() => vi.advanceTimersByTime(3000));
  rerender(draw(true));
  expect(screen.queryByText("3s")).toBeNull();
  expect(screen.queryByText("正在读取文件夹…")).toBeNull();
  expect(screen.getByText("尚无文件夹")).toBeTruthy();
  expect(vi.getTimerCount()).toBe(0);
});

it("clears its interval on unmount", () => {
  const { unmount } = render(<TreeReading />);
  expect(vi.getTimerCount()).toBe(1);
  unmount();
  expect(vi.getTimerCount()).toBe(0);
});
