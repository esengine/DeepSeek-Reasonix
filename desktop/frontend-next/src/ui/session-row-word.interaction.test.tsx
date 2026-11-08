// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import "./testkit";
import { Workspaces } from "./Workspaces";
import { RailSearch } from "./railsearch";
import type { TreeSession, TreeWorkspace } from "../port/hub";

afterEach(() => { cleanup(); vi.useRealTimers(); });

function draw(session: TreeSession, run?: string) {
  vi.useFakeTimers({ toFake: ["Date"], now: NOON });
  const tree: TreeWorkspace[] = [{ root: "/a", name: "alpha", remembered: true, sessions: [session] }];
  render(<RailSearch><Workspaces
    hub={{} as never} tree={tree} treeRead runtimes={[]} active="" folded={new Set()} onFold={() => {}} reload={vi.fn().mockResolvedValue(undefined)}
    onOpen={vi.fn().mockResolvedValue(undefined)} onFocus={() => {}} onClose={vi.fn().mockResolvedValue(undefined)}
    liveIds={() => []} runs={run && session.runtimeId ? { [session.runtimeId]: { run } } as never : {}} scope="all" onArchive={vi.fn()} onRename={() => {}} onError={vi.fn()}
    adder={{ add: () => {}, close: () => {}, at: null } as never}
  /></RailSearch>);
  return within(screen.getByRole("treeitem", { name: /plan/ }));
}

const NOON = new Date(2026, 5, 15, 12, 0, 0).getTime();
const base = { path: "/a/s/one.jsonl", name: "one", title: "plan", lastActiveAt: NOON - 3600_000 } as TreeSession;

it("says 运行中 in place of the time while the conversation runs", () => {
  const row = draw({ ...base, runtimeId: "r1" }, "running");
  expect(row.getByText("运行中").getAttribute("data-run")).toBe("running");
  expect(row.queryByText(/小时|分钟|昨天/)).toBeNull();
});

it("says 等你确认 when the conversation is waiting on the person", () => {
  const row = draw({ ...base, runtimeId: "r1" }, "halt");
  expect(row.getByText("等你确认").getAttribute("data-run")).toBe("halt");
});

it("keeps the time for a conversation that is not running", () => {
  const row = draw(base);
  expect(row.queryByText("运行中")).toBeNull();
  expect(row.queryByText("等你确认")).toBeNull();
  expect(row.getByText(/前|小时|分钟/)).toBeTruthy();
});
