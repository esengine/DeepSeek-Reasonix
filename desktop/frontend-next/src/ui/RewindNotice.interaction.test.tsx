// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { RewindNotice } from "./RewindNotice";
import type { RewindUndo } from "../port/port";

afterEach(() => { cleanup(); vi.useRealTimers(); });

const saved: RewindUndo = { transactionId: "tx-saved", turn: 2, files: 0 };
function draw() {
  let available: RewindUndo | null = saved;
  const readUndo = vi.fn(async () => available);
  const onUndo = vi.fn(async (_id: string) => { available = null; });
  const props = { readUndo, onUndo, refreshKey: 0, running: false, shown: true };
  const view = render(<RewindNotice {...props} />);
  return { props, view, readUndo, onUndo, invalidate: () => { available = null; } };
}

it("recovers a saved offer on a fresh mount without any message card", async () => {
  const { props, view } = draw();
  await screen.findByRole("button", { name: "撤销这次回退" });
  view.unmount();
  render(<RewindNotice {...props} />);
  await screen.findByRole("button", { name: "撤销这次回退" });
  expect(screen.getByRole("status").textContent).toContain("回退已完成");
});

it("hides the notice when undo succeeds", async () => {
  const { onUndo } = draw();
  await userEvent.click(await screen.findByRole("button", { name: "撤销这次回退" }));
  expect(onUndo).toHaveBeenCalledWith("tx-saved");
  await waitFor(() => expect(screen.queryByRole("status")).toBeNull());
});

it("shows a failed undo without losing the backend offer", async () => {
  const { onUndo } = draw();
  onUndo.mockRejectedValueOnce(new Error("file changed"));
  await userEvent.click(await screen.findByRole("button", { name: "撤销这次回退" }));
  expect((await screen.findByRole("alert")).textContent).toContain("file changed");
  expect(screen.getByRole("button", { name: "撤销这次回退" }).hasAttribute("disabled")).toBe(false);
});

it("hides during a new turn and does not revive an invalidated offer afterwards", async () => {
  const { props, view, invalidate, readUndo } = draw();
  await screen.findByRole("button", { name: "撤销这次回退" });
  view.rerender(<RewindNotice {...props} running />);
  expect(screen.queryByRole("status")).toBeNull();
  invalidate();
  view.rerender(<RewindNotice {...props} running={false} />);
  await waitFor(() => expect(readUndo).toHaveBeenCalledTimes(2));
  expect(screen.queryByRole("status")).toBeNull();
});

it("keeps valid offers but hides backend invalidation on the idle refresh", async () => {
  vi.useFakeTimers();
  const { invalidate } = draw();
  await act(async () => {});
  await act(async () => { vi.advanceTimersByTime(2000); });
  expect(screen.getByRole("button", { name: "撤销这次回退" })).not.toBeNull();
  invalidate();
  await act(async () => { vi.advanceTimersByTime(2000); });
  expect(screen.queryByRole("status")).toBeNull();
});

it("ignores an old session's query after switching to another session", async () => {
  let finishOld!: (value: RewindUndo | null) => void;
  const old = new Promise<RewindUndo | null>((resolve) => { finishOld = resolve; });
  const next = { ...saved, transactionId: "tx-next" };
  const readUndo = vi.fn().mockReturnValueOnce(old).mockResolvedValue(next);
  const onUndo = vi.fn(async (_id: string) => { readUndo.mockResolvedValue(null); });
  const props = { readUndo, onUndo, refreshKey: 0, running: false, shown: true };
  const view = render(<RewindNotice key="old-session" {...props} />);
  view.rerender(<RewindNotice key="next-session" {...props} />);
  await screen.findByRole("button", { name: "撤销这次回退" });
  await act(async () => { finishOld(saved); });
  await userEvent.click(screen.getByRole("button", { name: "撤销这次回退" }));
  expect(onUndo).toHaveBeenCalledWith("tx-next");
});
