// @vitest-environment jsdom
import { useEffect, useState } from "react";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import { Queue } from "./Queue";
import { useQueueActions } from "./queueactions";

const ports: MockPort[] = [];
afterEach(async () => {
  cleanup();
  for (const port of ports.splice(0)) await port.setQueuePaused(true);
  localStorage.clear();
  vi.restoreAllMocks();
});

const BODIES = ["First queued instruction", "Second queued instruction", "Third queued instruction"];
const STEER = "Selected guidance: " + "keep the whole instruction, including its final sentence. ".repeat(4);

async function queued() {
  const port = new MockPort();
  ports.push(port);
  let now = Date.now();
  vi.spyOn(Date, "now").mockImplementation(() => ++now);
  await port.setQueuePaused(true);
  for (const text of BODIES) await port.queueFollowup(text);
  await port.setQueuePaused(false);
  return port;
}

function draw(port: MockPort) {
  const dispatch = vi.fn();
  const fail = vi.fn();
  const beforeCancel: string[][] = [];
  const cancel = port.cancel.bind(port);
  vi.spyOn(port, "cancel").mockImplementation(async () => {
    const snapshot = await port.queue();
    beforeCancel.push(await Promise.all(snapshot.items.map((item) => port.readQueued(item.id))));
    await cancel();
  });
  function Panel() {
    const [moved, setMoved] = useState(0);
    useEffect(() => {
      const detach = port.subscribe((event) => {
        if (event.kind === "inbox_changed") setMoved((n) => n + 1);
      });
      return () => { detach(); };
    }, [port]);
    const actions = useQueueActions({ port: port as unknown as AgentPort, dispatch, fail, moved });
    return <Queue queue={actions.queue} running
      onRead={actions.onQueueRead} onEdit={actions.onQueueEdit} onMove={actions.onQueueMove}
      onCancel={actions.onQueueCancel} onSendNow={actions.onQueueSendNow}
      onRetry={actions.onQueueRetry} onRefresh={actions.onQueueRefresh} onPause={actions.onQueuePause} />;
  }
  render(<Panel />);
  return { beforeCancel, dispatch, fail };
}

async function send(index: number) {
  const buttons = await screen.findAllByRole("button", { name: "立即发送" });
  await userEvent.click(buttons[index]);
}

describe("sending the selected queue entry now", () => {
  it.each([1, 2])("puts row %i first before stopping the current reply", async (index) => {
    const port = await queued();
    const { beforeCancel, fail } = draw(port);
    await send(index);
    await waitFor(() => expect(beforeCancel).toEqual([[BODIES[index], ...BODIES.filter((_, i) => i !== index)]]));
    expect(fail).not.toHaveBeenCalled();
    const snapshot = await port.queue();
    expect(snapshot.items).toHaveLength(3);
  });

  it("keeps the order when the selected entry is already first", async () => {
    const port = await queued();
    const { beforeCancel, fail } = draw(port);
    await send(0);
    await waitFor(() => expect(beforeCancel).toEqual([BODIES]));
    expect(fail).not.toHaveBeenCalled();
  });

  it("promotes the replacement follow-up for accepted guidance, preserving its full body", async () => {
    const port = await queued();
    const accepted = await port.steer(STEER);
    const { beforeCancel, dispatch, fail } = draw(port);
    await send(3);
    await waitFor(() => expect(beforeCancel).toEqual([[STEER, ...BODIES]]));
    const snapshot = await port.queue();
    expect(snapshot.items.map((item) => item.id)).not.toContain(accepted.itemId);
    expect(snapshot.items[0].intent).toBe("followup");
    expect(dispatch).toHaveBeenCalledWith({ kind: "__unsent", id: accepted.itemId });
    expect(dispatch).toHaveBeenCalledWith(expect.objectContaining({ kind: "__queued", itemId: snapshot.items[0].id }));
    expect(fail).not.toHaveBeenCalled();
  });

  it("keeps the current reply running when reordering fails", async () => {
    const port = await queued();
    const error = new Error("queue write failed");
    vi.spyOn(port, "moveQueued").mockRejectedValue(error);
    const { beforeCancel, fail } = draw(port);
    await send(1);
    await waitFor(() => expect(fail).toHaveBeenCalledWith(error));
    expect(beforeCancel).toEqual([]);
    const snapshot = await port.queue();
    expect(await Promise.all(snapshot.items.map((item) => port.readQueued(item.id)))).toEqual(BODIES);
  });

  it.each(["readQueued", "cancelQueued", "queueFollowup"] as const)("does not stop the reply if guidance conversion fails at %s", async (method) => {
    const port = await queued();
    await port.steer(STEER);
    const error = new Error("conversion failed");
    vi.spyOn(port, method).mockRejectedValue(error);
    const { beforeCancel, fail } = draw(port);
    await send(3);
    await waitFor(() => expect(fail).toHaveBeenCalledWith(error));
    expect(beforeCancel).toEqual([]);
  });

  it("offers no send-now action while the queue is paused", async () => {
    const port = await queued();
    await port.setQueuePaused(true);
    const { beforeCancel } = draw(port);
    await screen.findByText(BODIES[0]);
    expect(screen.queryByRole("button", { name: "立即发送" })).toBeNull();
    expect(beforeCancel).toEqual([]);
  });
});
