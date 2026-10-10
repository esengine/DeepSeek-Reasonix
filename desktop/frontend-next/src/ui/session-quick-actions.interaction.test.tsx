// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import type { TreeSession, TreeWorkspace } from "../port/hub";

afterEach(cleanup);

const PATH = "/w/.reasonix/sessions/session.jsonl";
const TITLE = "review the changes";

function draw(over: Partial<TreeSession> = {}, live = false) {
  const session = { path: PATH, name: "session", title: TITLE, ...over };
  const tree: TreeWorkspace[] = [{ root: "/w", name: "w", sessions: [session] }];
  const onArchive = vi.fn().mockResolvedValue(undefined);
  const onClose = vi.fn().mockResolvedValue(undefined);
  const onOpen = vi.fn().mockResolvedValue(undefined);
  const onFocus = vi.fn();
  const onError = vi.fn();
  const removeSession = vi.fn().mockResolvedValue(undefined);
  const reload = vi.fn().mockResolvedValue(undefined);
  render(<Workspaces
    hub={{ removeSession } as never} tree={tree} treeRead
    runtimes={session.runtimeId ? [{ id: session.runtimeId, base: "", root: "/w", name: "w", sessionPath: PATH }] : []}
    active="" folded={new Set()} onFold={() => {}} reload={reload}
    onOpen={onOpen} onFocus={onFocus} onClose={onClose}
    liveIds={(ids) => live ? ids : []} runs={{}}
    scope={session.archived ? "archived" : "all"}
    onArchive={onArchive} onRename={() => {}} onError={onError}
    adder={{ add: () => {}, close: () => {}, at: null } as never}
  />);
  return { onArchive, onClose, onOpen, onFocus, onError, removeSession, reload };
}

const row = () => screen.getByRole("treeitem", { name: new RegExp(TITLE) });
const archive = () => within(row()).getByRole("button", { name: `归档会话：${TITLE}` });
const trash = () => within(row()).getByRole("button", { name: `删除会话：${TITLE}` });

it.each([undefined, "r1"])("archives directly without opening or focusing the session held by %j", async (runtimeId) => {
  const { onArchive, onOpen, onFocus } = draw({ runtimeId });
  await userEvent.click(archive());
  expect(onArchive).toHaveBeenCalledExactlyOnceWith(PATH, true, runtimeId);
  expect(onOpen).not.toHaveBeenCalled();
  expect(onFocus).not.toHaveBeenCalled();
  expect(screen.queryByRole("menu")).toBeNull();
});

it("restores an archived session through the same callback", async () => {
  const { onArchive } = draw({ archived: true });
  await userEvent.click(within(row()).getByRole("button", { name: `取消归档：${TITLE}` }));
  expect(onArchive).toHaveBeenCalledExactlyOnceWith(PATH, false, undefined);
});

it("keeps quick archive unavailable while the session is running", async () => {
  const { onArchive } = draw({ runtimeId: "r1" }, true);
  const button = archive() as HTMLButtonElement;
  expect(button.disabled).toBe(true);
  await userEvent.click(button);
  expect(onArchive).not.toHaveBeenCalled();
});

it("opens the existing delete confirmation and restores row focus on cancellation", async () => {
  const { removeSession, onOpen } = draw();
  await userEvent.click(trash());
  expect(screen.getByRole("alertdialog", { name: `删除「${TITLE}」？` })).toBeTruthy();
  expect(removeSession).not.toHaveBeenCalled();
  expect(onOpen).not.toHaveBeenCalled();
  await userEvent.keyboard("{Escape}");
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(removeSession).not.toHaveBeenCalled();
  expect(document.activeElement).toBe(row());
});

it("waits for an idle pane to close before deleting and reloading", async () => {
  const { onClose, removeSession, reload } = draw({ runtimeId: "r1" });
  let closed!: () => void;
  onClose.mockImplementation(() => new Promise<void>((resolve) => { closed = resolve; }));
  await userEvent.click(trash());
  await userEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "删除" }));
  expect(onClose).toHaveBeenCalledWith(["r1"]);
  expect(removeSession).not.toHaveBeenCalled();
  closed();
  await waitFor(() => expect(removeSession).toHaveBeenCalledExactlyOnceWith(PATH));
  expect(reload).toHaveBeenCalled();
});

it("keeps the running-session delete warning without stopping its pane", async () => {
  const { onClose, removeSession } = draw({ runtimeId: "r1" }, true);
  await userEvent.click(trash());
  expect(screen.getByText("正在运行，停止后才能删除")).toBeTruthy();
  await userEvent.keyboard("{Escape}");
  expect(onClose).not.toHaveBeenCalled();
  expect(removeSession).not.toHaveBeenCalled();
});

it("reports an archive failure without turning the gesture into an open", async () => {
  const { onArchive, onError, onOpen } = draw();
  const error = new Error("archive failed");
  onArchive.mockRejectedValue(error);
  await userEvent.click(archive());
  await waitFor(() => expect(onError).toHaveBeenCalledExactlyOnceWith(error));
  expect(onOpen).not.toHaveBeenCalled();
  expect(row()).toBeTruthy();
});

it("allows keyboard activation and keeps the version badge independently usable", async () => {
  const { onArchive, onOpen } = draw({ unread: true, versions: [{ path: PATH + ".v1", name: "v1", turns: 2 }] });
  archive().focus();
  await userEvent.keyboard("{Enter}");
  expect(onArchive).toHaveBeenCalledExactlyOnceWith(PATH, true, undefined);
  const badge = within(row()).getByRole("button", { name: "+1" });
  await userEvent.click(badge);
  expect(badge.getAttribute("aria-expanded")).toBe("true");
  expect(screen.getByText("早先版本 · 2 轮")).toBeTruthy();
  expect(row().querySelector(".unread-dot")).toBeTruthy();
  expect(onOpen).not.toHaveBeenCalled();
  await userEvent.click(screen.getByText(TITLE));
  expect(onOpen).toHaveBeenCalledWith({ root: "/w", sessionPath: PATH });
});
