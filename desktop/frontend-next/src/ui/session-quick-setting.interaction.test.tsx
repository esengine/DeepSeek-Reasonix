// @vitest-environment jsdom
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "./testkit";
import { Workspaces } from "./Workspaces";
import { SessionQuickSection } from "./SessionQuickSection";
import { setShowsSessionQuickActions, showsSessionQuickActions } from "../state/sessionQuick";
import type { TreeSession, TreeWorkspace } from "../port/hub";

beforeEach(() => setShowsSessionQuickActions(false));
afterEach(() => {
  cleanup();
  setShowsSessionQuickActions(false);
});

const PATH = "/w/.reasonix/sessions/session.jsonl";
const TITLE = "review the changes";

function draw(over: Partial<TreeSession> = {}, live = false) {
  const session = { path: PATH, name: "session", title: TITLE, ...over };
  const tree: TreeWorkspace[] = [{ root: "/w", name: "w", sessions: [session] }];
  const onArchive = vi.fn().mockResolvedValue(undefined);
  const onOpen = vi.fn().mockResolvedValue(undefined);
  const onPin = vi.fn();
  render(<Workspaces
    hub={{ removeSession: vi.fn().mockResolvedValue(undefined) } as never} tree={tree} treeRead
    runtimes={session.runtimeId ? [{ id: session.runtimeId, base: "", root: "/w", name: "w", sessionPath: PATH }] : []}
    active="" folded={new Set()} onFold={() => {}} reload={vi.fn().mockResolvedValue(undefined)}
    onOpen={onOpen} onFocus={vi.fn()} onClose={vi.fn().mockResolvedValue(undefined)}
    liveIds={(ids) => live ? ids : []} runs={{}} scope="all"
    onPin={onPin} onArchive={onArchive} onRename={() => {}} onError={vi.fn()}
    adder={{ add: () => {}, close: () => {}, at: null } as never}
  />);
  return { onArchive, onOpen, onPin };
}

const row = () => screen.getByRole("treeitem", { name: new RegExp(TITLE) });
const buttons = () => within(row()).queryAllByRole("button", { name: /归档|置顶|删除/ });

it("hides pin and archive for an explicit off preference", () => {
  draw();
  expect(buttons()).toEqual([]);
});

it("offers pin on the left and archive on the right without a quick delete action", async () => {
  setShowsSessionQuickActions(true);
  const { onArchive, onOpen } = draw();
  expect(buttons().map((b) => b.getAttribute("data-action"))).toEqual(["session.pin", "session.archive"]);
  await userEvent.click(within(row()).getByRole("button", { name: `归档会话：${TITLE}` }));
  expect(onArchive).toHaveBeenCalledExactlyOnceWith(PATH, true, undefined);
  expect(onOpen).not.toHaveBeenCalled();
});

it("keeps deletion in the context menu and archive off while the session runs", async () => {
  setShowsSessionQuickActions(true);
  draw({ runtimeId: "r1" }, true);
  expect((within(row()).getByRole("button", { name: `归档会话：${TITLE}` }) as HTMLButtonElement).disabled).toBe(true);
  expect(within(row()).queryByRole("button", { name: `删除会话：${TITLE}` })).toBeNull();
  await userEvent.pointer({ target: row(), keys: "[MouseRight]" });
  await userEvent.click(screen.getByRole("menuitem", { name: "删除会话" }));
  expect(screen.getByRole("alertdialog", { name: `删除「${TITLE}」？` })).toBeTruthy();
});

it("pins without opening the conversation and exposes its pressed state", async () => {
  setShowsSessionQuickActions(true);
  const { onPin, onOpen } = draw();
  const pin = within(row()).getByRole("button", { name: `置顶会话：${TITLE}` });
  expect(pin.getAttribute("aria-pressed")).toBe("false");
  pin.focus();
  await userEvent.keyboard("{Enter}");
  expect(onPin).toHaveBeenCalledExactlyOnceWith(PATH);
  expect(onOpen).not.toHaveBeenCalled();
});

it("does not submit archive twice while the first request is still pending", async () => {
  setShowsSessionQuickActions(true);
  const { onArchive } = draw();
  let finish!: () => void;
  onArchive.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve; }));
  const archive = within(row()).getByRole("button", { name: `归档会话：${TITLE}` });
  await userEvent.click(archive);
  await userEvent.click(archive);
  expect(onArchive).toHaveBeenCalledTimes(1);
  await act(async () => { finish(); });
});

it("opens a local conversation only once when Enter is pressed on its row", async () => {
  const { onOpen } = draw();
  row().focus();
  await userEvent.keyboard("{Enter}");
  expect(onOpen).toHaveBeenCalledExactlyOnceWith({ root: "/w", sessionPath: PATH });
});

it("shows quick actions by default while preserving an explicit off choice", () => {
  localStorage.removeItem("rx-session-quick-actions");
  expect(showsSessionQuickActions()).toBe(true);
  setShowsSessionQuickActions(false);
  expect(showsSessionQuickActions()).toBe(false);
});

it("shows or hides the buttons on a list that is already drawn when the setting flips", () => {
  draw();
  expect(buttons()).toEqual([]);
  act(() => setShowsSessionQuickActions(true));
  expect(buttons()).toHaveLength(2);
  act(() => setShowsSessionQuickActions(false));
  expect(buttons()).toEqual([]);
});

it("remembers the explicitly disabled choice made in Settings", async () => {
  expect(showsSessionQuickActions()).toBe(false);
  render(<SessionQuickSection />);
  const show = screen.getByRole("button", { name: "显示" });
  expect(show.getAttribute("aria-pressed")).toBe("false");
  await userEvent.click(show);
  expect(showsSessionQuickActions()).toBe(true);
  expect(show.getAttribute("aria-pressed")).toBe("true");
  await userEvent.click(screen.getByRole("button", { name: "关闭" }));
  expect(showsSessionQuickActions()).toBe(false);
});
