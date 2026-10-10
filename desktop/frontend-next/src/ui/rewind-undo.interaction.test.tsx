// @vitest-environment jsdom
import "./testkit";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { HistoryMessage, RewindResult } from "../port/port";
import type { WireEvent } from "../port/wire";
import type { RuntimeView } from "../port/hub";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });
const original: HistoryMessage[] = [
  { role: "user", content: "Restore this turn", msgIndex: 0 },
  { role: "assistant", content: "Updated file.txt", msgIndex: 1 },
];
const props = {
  rt: { id: "p1", root: "/sample", name: "Sample", sessionPath: "session-a" } as RuntimeView,
  title: "Sample", active: true, visible: true, sideHost: null, side: false,
  onFocus() {}, onReport() {}, onSessionChanged() {}, pulse: 0, findPulse: 0,
  onSettings() {}, needsProject: false, onOpenProject() {}, onKeepHere() {},
  theme: "dark", dockW: 560, dockMax: 880, onDockW() {},
};
async function open(conversation = true, holdInitialStatus = false, runtimePath: string | undefined = "session-a", mutation?: Partial<RewindResult>) {
  const port = new MockPort();
  let history = original;
  let session = "session-a";
  let running = false;
  let emit: (event: WireEvent) => void = () => {};
  const subscribe = port.subscribe.bind(port);
  vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap, bootstrap) => { emit = onEvent; return subscribe(onEvent, onGap, bootstrap); });
  const status = await port.status();
  const statusReads = vi.spyOn(port, "status").mockImplementation(async () => ({ ...status, running, sessionPath: session }));
  let resolveStatus: (() => void) | undefined;
  if (holdInitialStatus) statusReads.mockReturnValueOnce(new Promise((resolve) => { resolveStatus = () => resolve({ ...status, running, sessionPath: session }); }));
  const reads = vi.spyOn(port, "history").mockImplementation(async () => history);
  vi.spyOn(port, "checkpoints").mockImplementation(async () => history.length ? [{ turn: 1, prompt: "Restore this turn", files: mutation ? 3 : 1, msgIndex: 0 }] : []);
  vi.spyOn(port, "prepareRewind").mockResolvedValue({ planId: "plan-1", turn: 1, coverage: "full", canFiles: true, canConversation: true, fileCount: mutation ? 3 : 1, requiresConfirmation: false });
  vi.spyOn(port, "commitRewind").mockImplementation(async () => {
    if (conversation) history = [];
    return { ok: true, conversationOk: conversation, transactionId: "tx-1", undoAvailable: true, deleted: ["file.txt"], ...mutation };
  });
  const undo = vi.spyOn(port, "undoRewind").mockImplementation(async () => { history = original; });
  const view = render(<Pane {...props} rt={{ ...props.rt, sessionPath: runtimePath }} port={port} />);
  await screen.findByRole("button", { name: "回到这里" });
  return { port, reads, undo, view, resolveStatus, start: () => { running = true; emit({ kind: "turn_started" } as WireEvent); }, setSession: (next: string) => { session = next; } };
}
async function restore(scope = "代码和对话") {
  fireEvent.click(screen.getByRole("button", { name: "回到这里" }));
  fireEvent.click(screen.getByRole("menuitem", { name: new RegExp(scope) }));
}

it.each([true, false])("keeps undo through the real Pane reload with conversation removal %j", async (conversation) => {
  const { reads, undo } = await open(conversation);
  const before = reads.mock.calls.length;
  await restore(conversation ? "代码和对话" : "只还原代码");
  await waitFor(() => expect(reads.mock.calls.length).toBeGreaterThan(before));
  await waitFor(() => expect(!!screen.queryByRole("button", { name: "回到这里" })).toBe(!conversation));
  expect(screen.getByText("已还原 1 个文件")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "撤销这次还原" }));
  await waitFor(() => expect(undo).toHaveBeenCalledExactlyOnceWith("tx-1"));
  await screen.findByRole("button", { name: "回到这里" });
  expect(screen.queryByRole("button", { name: "撤销这次还原" })).toBeNull();
});

it("keeps undo after unrelated rerender, supports a refused undo retry and repeated restore", async () => {
  const h = await open();
  await restore();
  await screen.findByRole("button", { name: "撤销这次还原" });
  h.view.rerender(<Pane {...props} port={h.port} title="Renamed conversation" />);
  h.undo.mockRejectedValueOnce(new Error("Tracked file changed"));
  fireEvent.click(screen.getByRole("button", { name: "撤销这次还原" }));
  await screen.findByRole("alert", { name: "" });
  expect(screen.getByText("Tracked file changed")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "撤销这次还原" }));
  await screen.findByRole("button", { name: "回到这里" });
  await restore();
  await screen.findByRole("button", { name: "撤销这次还原" });
  expect(h.port.commitRewind).toHaveBeenCalledTimes(2);
});

it("allows explicit dismissal without invoking undo", async () => {
  const h = await open(); await restore();
  await screen.findByRole("button", { name: "撤销这次还原" });
  fireEvent.click(screen.getByRole("button", { name: "知道了" }));
  expect(screen.queryByText("已还原 1 个文件")).toBeNull(); expect(h.undo).not.toHaveBeenCalled();
});

it("clears the receipt when host status changes even while runtime metadata is stale", async () => {
  const h = await open(); await restore();
  await screen.findByRole("button", { name: "撤销这次还原" });
  h.setSession("session-b");
  h.view.rerender(<Pane {...props} port={h.port} pulse={1} />);
  await waitFor(() => expect(screen.queryByRole("button", { name: "撤销这次还原" })).toBeNull());
  expect(h.undo).not.toHaveBeenCalled();
});

it("does not publish a pending old-session restore or append its prompt to the new session", async () => {
  const h = await open();
  let resolve!: (value: RewindResult) => void;
  vi.mocked(h.port.commitRewind).mockReturnValue(new Promise((done) => { resolve = done; }));
  await restore(); await waitFor(() => expect(h.port.commitRewind).toHaveBeenCalled());
  h.view.rerender(<Pane key="takeover-1" {...props} port={h.port} rt={{ ...props.rt, sessionPath: "session-b" }} />);
  await screen.findByRole("button", { name: "回到这里" });
  const before = h.reads.mock.calls.length;
  await act(async () => resolve({ ok: true, conversationOk: true, transactionId: "tx-1", undoAvailable: true, deleted: ["file.txt"] }));
  expect(screen.queryByRole("button", { name: "撤销这次还原" })).toBeNull();
  expect(h.reads.mock.calls).toHaveLength(before);
  expect((screen.getByRole("combobox", { name: "任务输入" }) as HTMLTextAreaElement).value).toBe("");
});

it("does not mistake initial status hydration for leaving the known conversation", async () => {
  const h = await open(true, true);
  await restore();
  await screen.findByRole("button", { name: "撤销这次还原" });
  await act(async () => h.resolveStatus!());
  expect(screen.getByRole("button", { name: "撤销这次还原" })).toBeTruthy();
});

it("disables repeated undo clicks while the operation is in flight", async () => {
  const h = await open(); await restore();
  const button = await screen.findByRole("button", { name: "撤销这次还原" });
  let resolve!: () => void;
  h.undo.mockReturnValue(new Promise((done) => { resolve = done; }));
  fireEvent.click(button); fireEvent.click(button);
  expect((button as HTMLButtonElement).disabled).toBe(true);
  expect(h.undo).toHaveBeenCalledExactlyOnceWith("tx-1");
  await act(async () => resolve());
  expect(screen.queryByRole("button", { name: "撤销这次还原" })).toBeNull();
});

it("keeps a receipt when runtime metadata catches up to the known host session", async () => {
  const h = await open(true, false, "");
  await restore();
  await screen.findByRole("button", { name: "撤销这次还原" });
  h.view.rerender(<Pane {...props} port={h.port} />);
  expect(screen.getByRole("button", { name: "撤销这次还原" })).toBeTruthy();
});

it("retries history after a successful undo without applying the transaction twice", async () => {
  const h = await open(); await restore();
  const undo = await screen.findByRole("button", { name: "撤销这次还原" });
  await waitFor(() => expect((undo as HTMLButtonElement).disabled).toBe(false));
  h.reads.mockRejectedValueOnce(new Error("History unavailable"));
  fireEvent.click(undo);
  const retry = await screen.findByRole("button", { name: "重试刷新会话" });
  expect(screen.getByRole("alert").textContent).toBe("History unavailable");
  expect(screen.queryByRole("button", { name: "撤销这次还原" })).toBeNull();
  fireEvent.click(retry);
  await screen.findByRole("button", { name: "回到这里" });
  expect(screen.queryByRole("button", { name: "重试刷新会话" })).toBeNull();
  expect(h.undo).toHaveBeenCalledExactlyOnceWith("tx-1");
});


it.each(["commit", "undo"])("reloads the actual Pane after %s completes behind a new-run event", async (operation) => {
  const h = await open();
  if (operation === "undo") { await restore(); await screen.findByRole("button", { name: "撤销这次还原" }); }
  let release!: () => void;
  const response = new Promise<void>((resolve) => { release = resolve; });
  const commit = vi.mocked(h.port.commitRewind);
  const mutation = operation === "commit" ? commit.getMockImplementation()! : h.undo.getMockImplementation()!;
  if (operation === "commit") commit.mockImplementation(async (plan) => { const result = await mutation(plan); await response; return result as RewindResult; });
  else h.undo.mockImplementation(async (tx) => { await mutation(tx); await response; });
  if (operation === "commit") await restore();
  else fireEvent.click(screen.getByRole("button", { name: "撤销这次还原" }));
  await act(async () => h.start());
  const before = h.reads.mock.calls.length;
  await act(async () => release());
  await waitFor(() => expect(h.reads.mock.calls.length).toBeGreaterThan(before));
  const userCards = h.view.container.querySelectorAll('[data-k="me"] .txt');
  expect([...userCards].map((card) => card.textContent)).toEqual(operation === "undo" ? ["Restore this turn"] : []);
  expect(screen.queryByRole("button", { name: "撤销这次还原" })).toBeNull();
  expect((screen.getByRole("combobox", { name: "任务输入" }) as HTMLTextAreaElement).value).toBe(operation === "undo" ? "Restore this turn" : "");
});

it("does not replay a pending rewind history read into a switched conversation", async () => {
  const h = await open();
  let resolve!: (history: HistoryMessage[]) => void;
  h.reads.mockReturnValueOnce(new Promise((done) => { resolve = done; }));
  await restore();
  await screen.findByRole("button", { name: "撤销这次还原" });
  h.setSession("session-b");
  h.view.rerender(<Pane {...props} port={h.port} pulse={1} />);
  await waitFor(() => expect(screen.queryByRole("button", { name: "撤销这次还原" })).toBeNull());
  await act(async () => resolve([{ role: "user", content: "Stale prior-session text", msgIndex: 0 }]));
  expect(screen.queryByText("Stale prior-session text")).toBeNull();
});


it.each([
  { label: "mixed restore and delete", written: ["a", "b"], deleted: ["c"], conversation: false, count: 3 },
  { label: "restore only", written: ["a", "b", "c"], deleted: undefined, conversation: false, count: 3 },
  { label: "restore only with empty deletes", written: ["a", "b", "c"], deleted: [], conversation: false, count: 3 },
  { label: "fewer writes than planned", written: ["a"], deleted: undefined, conversation: false, count: 1 },
  { label: "conversation only", written: undefined, deleted: undefined, conversation: true, count: 0 },
])("preserves upstream committed-file counts after the Pane reload: $label", async ({ written, deleted, conversation, count }) => {
  const h = await open(conversation, false, "session-a", { written, deleted });
  fireEvent.click(screen.getByRole("button", { name: "回到这里" }));
  expect(screen.getAllByText("3 个文件")).toHaveLength(2);
  fireEvent.click(screen.getByRole("menuitem", { name: new RegExp(conversation ? "只回退对话" : "只还原代码") }));
  await screen.findByText(`已还原 ${count} 个文件`);
  const undo = screen.getByRole<HTMLButtonElement>("button", { name: "撤销这次还原" });
  await waitFor(() => expect(undo.disabled).toBe(false));
  fireEvent.click(undo);
  await waitFor(() => expect(h.undo).toHaveBeenCalledExactlyOnceWith("tx-1"));
});
