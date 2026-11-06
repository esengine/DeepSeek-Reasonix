// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Queue } from "./Queue";
import type { Queue as QueueSnapshot, QueueItem } from "../port/port";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const item = (over: Partial<QueueItem> = {}): QueueItem => ({
  id: "i1",
  intent: "followup",
  state: "queued",
  preview: "把 gate_test.go 里那三个跳过的用例打开",
  createdAt: "2026-08-25T10:00:00Z",
  ...over,
});

const snapshot = (over: Partial<QueueSnapshot> = {}): QueueSnapshot => ({
  revision: 1,
  paused: false,
  items: [item()],
  capacity: { items: 1, maxItems: 64, bytes: 3174, maxBytes: 64 << 20 },
  ...over,
});

function draw(queue: QueueSnapshot, over: Partial<Parameters<typeof Queue>[0]> = {}) {
  const on = { onSendNow: vi.fn(), onToSteer: vi.fn(), onCancel: vi.fn(), onRetry: vi.fn(), onPause: vi.fn() };
  render(<Queue queue={queue} running {...on} {...over} />);
  return on;
}

describe("the actions on a queued line", () => {
  it("hands the entry to the steer conversion", async () => {
    const on = draw(snapshot());
    await userEvent.click(screen.getByRole("button", { name: "改为插话" }));
    expect(on.onToSteer).toHaveBeenCalledWith(expect.objectContaining({ id: "i1" }));
  });

  it("withdraws exactly that entry", async () => {
    const on = draw(snapshot({ items: [item({ id: "a" }), item({ id: "b" })] }));
    await userEvent.click(screen.getAllByRole("button", { name: "撤回" })[1]);
    expect(on.onCancel).toHaveBeenCalledWith("b");
  });

  it("sends exactly that entry now", async () => {
    const on = draw(snapshot());
    await userEvent.click(screen.getByRole("button", { name: "立即发送" }));
    expect(on.onSendNow).toHaveBeenCalledWith(expect.objectContaining({ id: "i1" }));
  });

  it("offers no conversion or send-now with no turn in the way", () => {
    draw(snapshot(), { running: false });
    expect(screen.queryByRole("button", { name: "改为插话" })).toBeNull();
    expect(screen.queryByRole("button", { name: "立即发送" })).toBeNull();
    expect(screen.getByRole("button", { name: "撤回" })).toBeTruthy();
  });

  it("releases a held queue", async () => {
    const on = draw(snapshot({ paused: true }));
    await userEvent.click(screen.getByRole("button", { name: "继续派发" }));
    expect(on.onPause).toHaveBeenCalledWith(false);
  });
});

describe("retrying a held entry", () => {
  it("asks the kernel to retry exactly that entry", async () => {
    const on = draw(
      snapshot({ paused: true, items: [item({ id: "u1", state: "uncertain", blockCode: "steer_unapplied" })] }),
      { running: false },
    );
    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(on.onRetry).toHaveBeenCalledWith("u1");
  });
});
