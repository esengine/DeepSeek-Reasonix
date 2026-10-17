// @vitest-environment jsdom
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { RemoteHosts } from "./RemoteHosts";
import { setShowsSessionQuickActions } from "../state/sessionQuick";
import type { TreeSession } from "../port/hub";

afterEach(() => { cleanup(); localStorage.removeItem("rx-session-quick-actions"); });

function draw(over: Partial<TreeSession> = {}, live = false) {
  setShowsSessionQuickActions(true);
  const session: TreeSession = { path: "/same/task.jsonl", name: "Remote task", ...over };
  const onOpen = vi.fn();
  const onFocus = vi.fn();
  const onClose = vi.fn().mockResolvedValue(undefined);
  const onError = vi.fn();
  const pinSession = vi.fn(async (_path: string, pinned: boolean, _host?: string) => { session.pinned = pinned; });
  const archiveSession = vi.fn(async (_path: string, archived: boolean, _host?: string) => { session.archived = archived; });
  function Harness() {
    const [book, setBook] = useState([{ root: "/project", name: "Project", sessions: [{ ...session }] }]);
    return <RemoteHosts hub={{ pinSession, archiveSession } as never}
      hosts={[{ name: "gpu", target: "test@host", status: "connected", workspace: "/project" }]}
      trees={{ gpu: book }} runtimes={over.runtimeId ? [{ id: over.runtimeId, host: "gpu", base: "", root: "/project", name: "Remote task", sessionPath: session.path }] : []}
      active="" scope={over.archived ? "archived" : "all"} onOpen={onOpen} onFocus={onFocus} onClose={onClose} liveIds={(ids) => live ? ids : []}
      reload={async () => {}} reloadTrees={async () => {}} readTree={async () => setBook([{ root: "/project", name: "Project", sessions: [{ ...session }] }])}
      onError={onError} />;
  }
  render(<Harness />);
  return { onOpen, onFocus, onClose, onError, pinSession, archiveSession };
}

it("pins and unpins on the remote host without opening the same local path", async () => {
  const { pinSession, onOpen, onFocus } = draw();
  await userEvent.click(screen.getByRole("button", { name: "置顶会话：Remote task" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "取消置顶：Remote task" }).getAttribute("aria-pressed")).toBe("true"));
  expect(pinSession).toHaveBeenLastCalledWith("/same/task.jsonl", true, "gpu");
  await userEvent.click(screen.getByRole("button", { name: "取消置顶：Remote task" }));
  await waitFor(() => expect(pinSession).toHaveBeenLastCalledWith("/same/task.jsonl", false, "gpu"));
  expect(onOpen).not.toHaveBeenCalled();
  expect(onFocus).not.toHaveBeenCalled();
});

it("closes an idle remote pane before archiving its transcript on that host", async () => {
  const { archiveSession, onClose, onOpen } = draw({ runtimeId: "remote-pane" });
  let closed!: () => void;
  onClose.mockImplementation(() => new Promise<void>((resolve) => { closed = resolve; }));
  await userEvent.click(screen.getByRole("button", { name: "归档会话：Remote task" }));
  expect(onClose).toHaveBeenCalledWith(["remote-pane"]);
  expect(archiveSession).not.toHaveBeenCalled();
  closed();
  await waitFor(() => expect(archiveSession).toHaveBeenCalledWith("/same/task.jsonl", true, "gpu"));
  await waitFor(() => expect(screen.queryByText("Remote task")).toBeNull());
  expect(onOpen).not.toHaveBeenCalled();
});

it("keeps archive disabled for a running remote conversation", async () => {
  const { archiveSession, onClose } = draw({ runtimeId: "running-pane" }, true);
  const button = screen.getByRole("button", { name: "归档会话：Remote task" }) as HTMLButtonElement;
  expect(button.disabled).toBe(true);
  await userEvent.click(button);
  expect(archiveSession).not.toHaveBeenCalled();
  expect(onClose).not.toHaveBeenCalled();
});

it("restores a remote archive without opening or closing a pane", async () => {
  const { archiveSession, onClose, onOpen } = draw({ archived: true });
  const restore = screen.getByRole("button", { name: "取消归档：Remote task" });
  restore.focus();
  await userEvent.keyboard("{Enter}");
  await waitFor(() => expect(archiveSession).toHaveBeenCalledWith("/same/task.jsonl", false, "gpu"));
  await waitFor(() => expect(screen.queryByText("Remote task")).toBeNull());
  expect(onClose).not.toHaveBeenCalled();
  expect(onOpen).not.toHaveBeenCalled();
});

it("identifies archived and legacy remote transcripts just like local rows", () => {
  draw({ archived: true, legacy: true });
  expect(screen.getByText("已归档")).toBeTruthy();
  expect(screen.getByTitle("由 Reasonix 1.x 创建：Studio 只读打开，继续对话会另存为新会话")).toBeTruthy();
});

it("does not submit duplicate pin writes while the first is pending", async () => {
  const { pinSession } = draw();
  let finish!: () => void;
  const pending = new Promise<void>((resolve) => { finish = resolve; });
  const apply = pinSession.getMockImplementation()!;
  pinSession.mockImplementationOnce(async (...args) => { await pending; await apply(...args); });
  const button = screen.getByRole("button", { name: "置顶会话：Remote task" }) as HTMLButtonElement;
  await userEvent.click(button);
  expect(button.disabled).toBe(true);
  await userEvent.click(button);
  expect(pinSession).toHaveBeenCalledTimes(1);
  finish();
  await waitFor(() => expect(screen.getByRole("button", { name: "取消置顶：Remote task" }).getAttribute("aria-pressed")).toBe("true"));
});

it("reports a remote write failure while leaving the conversation available", async () => {
  const { pinSession, onError } = draw();
  const error = new Error("remote refused");
  pinSession.mockRejectedValueOnce(error);
  await userEvent.click(screen.getByRole("button", { name: "置顶会话：Remote task" }));
  await waitFor(() => expect(onError).toHaveBeenCalledWith(error));
  expect(within(screen.getByRole("treeitem", { name: "Remote task" })).getByRole("button", { name: "置顶会话：Remote task" })).toBeTruthy();
});

it("does not steal focus back after the reader moves to another control during a write", async () => {
  const { pinSession } = draw();
  let finish!: () => void;
  pinSession.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve; }));
  const pin = screen.getByRole("button", { name: "置顶会话：Remote task" });
  await userEvent.click(pin);
  const elsewhere = screen.getByRole("button", { name: /在 Project/ });
  elsewhere.focus();
  await act(async () => { finish(); });
  expect(document.activeElement).toBe(elsewhere);
});
