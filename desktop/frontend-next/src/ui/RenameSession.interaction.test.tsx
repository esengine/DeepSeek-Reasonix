// @vitest-environment jsdom
import { afterEach, beforeAll, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { RenameSession } from "./RenameSession";
import { PaneTabs } from "./PaneTabs";

beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () { this.open = true; };
  HTMLDialogElement.prototype.close = function () { this.open = false; };
});
afterEach(cleanup);

function draw() {
  const hub = { renameSession: vi.fn().mockResolvedValue(undefined), autoNameSession: vi.fn().mockResolvedValue({ title: "new topic" }) };
  const onClose = vi.fn();
  const onSaved = vi.fn();
  render(<RenameSession session={{ path: "/session.jsonl", title: "old title" }} hub={hub} onClose={onClose} onSaved={onSaved} />);
  return { hub, onClose, onSaved };
}

it("saves a trimmed manual title once on Enter", async () => {
  const { hub, onSaved } = draw();
  const input = screen.getByRole("textbox", { name: "聊天标题" });
  await userEvent.clear(input);
  await userEvent.type(input, "  new title  {Enter}");
  await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
  expect(hub.renameSession).toHaveBeenCalledExactlyOnceWith("/session.jsonl", "new title");
  expect(hub.autoNameSession).not.toHaveBeenCalled();
});

it("fills a generated title without saving, then saves on confirmation", async () => {
  const { hub, onSaved, onClose } = draw();
  let finish!: (value: { title: string }) => void;
  hub.autoNameSession.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
  await userEvent.click(screen.getByRole("button", { name: "自动命名" }));
  expect((screen.getByRole("button", { name: "命名中…" }) as HTMLButtonElement).disabled).toBe(true);
  await userEvent.keyboard("{Escape}");
  expect(onClose).not.toHaveBeenCalled();
  finish({ title: "latest topic" });
  await waitFor(() => expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("latest topic"));
  expect(hub.autoNameSession).toHaveBeenCalledExactlyOnceWith("/session.jsonl");
  expect(hub.renameSession).not.toHaveBeenCalled();
  expect(onSaved).not.toHaveBeenCalled();
  expect(onClose).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "\u4fdd\u5b58" }));
  await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
  expect(hub.renameSession).toHaveBeenCalledExactlyOnceWith("/session.jsonl", "latest topic");
});

it("cancels a generated draft without saving a title", async () => {
  const { hub, onSaved, onClose } = draw();
  await userEvent.click(screen.getByRole("button", { name: "\u81ea\u52a8\u547d\u540d" }));
  await waitFor(() => expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("new topic"));
  await userEvent.click(screen.getByRole("button", { name: "\u53d6\u6d88" }));
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(onSaved).not.toHaveBeenCalled();
  expect(hub.renameSession).not.toHaveBeenCalled();
});

it("keeps the original title on failure and supports retry or cancellation", async () => {
  const { hub, onSaved, onClose } = draw();
  hub.autoNameSession.mockRejectedValueOnce(new Error("model unavailable"));
  await userEvent.click(screen.getByRole("button", { name: "自动命名" }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("old title");
  expect(onSaved).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(onClose).toHaveBeenCalledTimes(1);
});

it("opens the rename dialog from the tab rename menu", async () => {
  const onRename = vi.fn();
  const rt = { id: "r1", base: "", root: "/w", name: "w", sessionPath: "/session.jsonl" };
  render(<PaneTabs tabs={[{ rt, title: "old title", run: "idle", live: false }]} active="r1" showRoot={false} onFocus={() => {}} onClose={() => {}} onRename={onRename} />);
  fireEvent.contextMenu(screen.getByRole("tab"));
  await userEvent.click(screen.getByRole("menuitem", { name: "重命名" }));
  expect(onRename).toHaveBeenCalledExactlyOnceWith(rt, "old title");
});


it("renames a tab inline on double-click and saves once on Enter", async () => {
  const onRename = vi.fn();
  const rt = { id: "r1", base: "", root: "/w", name: "w", sessionPath: "/session.jsonl" };
  render(<PaneTabs tabs={[{ rt, title: "old title", run: "idle", live: false }]} active="r1" showRoot={false} onFocus={() => {}} onClose={() => {}} onRename={onRename} />);
  await userEvent.dblClick(screen.getByRole("tab"));
  const input = screen.getByRole("textbox", { name: "重命名该会话" });
  await userEvent.clear(input);
  await userEvent.type(input, "  new title  {Enter}");
  expect(onRename).toHaveBeenCalledExactlyOnceWith(rt, "new title", "inline");
  expect(screen.queryByRole("textbox")).toBeNull();
});

it("cancels tab double-click renaming with Escape without closing the tab", async () => {
  const onRename = vi.fn();
  const onClose = vi.fn();
  const rt = { id: "r1", base: "", root: "/w", name: "w", sessionPath: "/session.jsonl" };
  render(<PaneTabs tabs={[{ rt, title: "old title", run: "idle", live: false }]} active="r1" showRoot={false} onFocus={() => {}} onClose={onClose} onRename={onRename} />);
  await userEvent.dblClick(screen.getByRole("tab"));
  const input = screen.getByRole("textbox", { name: "重命名该会话" });
  await userEvent.clear(input);
  await userEvent.keyboard("{Escape}");
  expect(onRename).not.toHaveBeenCalled();
  expect(onClose).not.toHaveBeenCalled();
  expect(screen.queryByRole("textbox")).toBeNull();
});


it("saves an empty dialog title as a reset", async () => {
  const { hub, onSaved } = draw();
  await userEvent.clear(screen.getByRole("textbox", { name: "聊天标题" }));
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
  expect(hub.renameSession).toHaveBeenCalledExactlyOnceWith("/session.jsonl", "");
  expect(hub.autoNameSession).not.toHaveBeenCalled();
});

it.each(["Escape", "取消"])("does not reset a cleared dialog when cancelled via %s", async (action) => {
  const { hub, onClose } = draw();
  await userEvent.clear(screen.getByRole("textbox", { name: "聊天标题" }));
  if (action === "Escape") await userEvent.keyboard("{Escape}");
  else await userEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(hub.renameSession).not.toHaveBeenCalled();
});

it("allows retry after an empty-title reset fails", async () => {
  const { hub, onSaved } = draw();
  hub.renameSession.mockRejectedValueOnce(new Error("reset failed"));
  await userEvent.clear(screen.getByRole("textbox", { name: "聊天标题" }));
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(onSaved).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
});

it("resets a cleared tab title when the inline input loses focus", async () => {
  const onRename = vi.fn();
  const rt = { id: "r1", base: "", root: "/w", name: "w", sessionPath: "/session.jsonl" };
  render(<PaneTabs tabs={[{ rt, title: "old title", run: "idle", live: false }]} active="r1" showRoot={false} onFocus={() => {}} onClose={() => {}} onRename={onRename} />);
  await userEvent.dblClick(screen.getByRole("tab"));
  await userEvent.clear(screen.getByRole("textbox", { name: "重命名该会话" }));
  await userEvent.tab();
  expect(onRename).toHaveBeenCalledExactlyOnceWith(rt, "", "inline");
});
