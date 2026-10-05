// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { Context } from "./Context";
import { MockPort } from "../../port/mock";
import type { AgentPort, ContextBreakdown } from "../../port/port";

afterEach(cleanup);

// The user's own shape: a 1M window that folds at 160k because of a bound they
// never set. The fold point is 16% of the window, and before this the only way
// to move it was a settings sheet two clicks away.
const wide: ContextBreakdown = {
  used: 82_000, window: 1_000_000, compact_at: 160_000,
  boundary: "economic", capacity_at: 850_000,
  system: 20_000, tools: 12_000, user: 10_000, reply: 20_000, output: 20_000,
};

const mount = async (over: Partial<ContextBreakdown> = {}) => {
  const port = new MockPort() as unknown as AgentPort;
  await port.setContextWindow(1_000_000);
  await port.saveCompaction(160_000);
  const onCtx = vi.fn();
  render(<Context ctx={{ ...wide, ...over }} legend port={port} onCtx={onCtx} />);
  return { port, onCtx };
};

describe("changing the fold point where it is read", () => {
  it("offers the fold point as the way in, not only as a figure", async () => {
    await mount();
    const entry = screen.getByRole("button", { name: "160k" });
    expect(entry.getAttribute("aria-expanded")).toBe("false");
  });

  // The mark was drawn as a notch and read as a handle, which is the control
  // a reader already expected. It is the same element, not a second one placed
  // beside it — otherwise the thing being read and the thing being dragged are
  // two objects the reader has to match up.
  it("turns the mark itself into the handle", async () => {
    await mount();
    await userEvent.click(screen.getByRole("button", { name: "160k" }));
    const handle = await screen.findByRole("slider", { name: "维护点" });
    expect(handle.closest(".ctxcapbar")).toBeTruthy();
  });

  // The bound is an absolute input size, which is its whole point: the same
  // 16% is 160k against one window and 20k against another. A track measured
  // in shares would rewrite the setting every time the model changed.
  it("carries tokens, never a share of the window", async () => {
    await mount();
    await userEvent.click(screen.getByRole("button", { name: "160k" }));
    const handle = await screen.findByRole("slider", { name: "维护点" });
    expect(handle.getAttribute("value")).toBe("160000");
  });

  // The handle and the mark are one object, so they must be placed by one
  // arithmetic. Given its own min..max the track cut off everything below the
  // floor and above capacity, and the handle then sat at one fraction of the
  // track while the mark sat at another — dragging to the visual middle moved
  // the mark somewhere else entirely.
  it("spans the same range the bar it sits on does", async () => {
    await mount();
    await userEvent.click(screen.getByRole("button", { name: "160k" }));
    const handle = await screen.findByRole("slider", { name: "维护点" });
    expect(handle.getAttribute("max")).toBe("1000000");
    fireEvent.change(handle, { target: { value: "500000" } });
    const mark = document.querySelector(".ctxcapbar > b") as HTMLElement;
    expect(mark.style.left).toBe("50%");
  });

  // Past capacity the economic bound has nothing left to tighten, so the far
  // stretch of the track is that answer rather than a very large threshold.
  it("says what letting go past capacity will mean, before it happens", async () => {
    await mount();
    await userEvent.click(screen.getByRole("button", { name: "160k" }));
    const handle = await screen.findByRole("slider", { name: "维护点" });
    fireEvent.change(handle, { target: { value: "900000" } });
    expect(screen.getByText("松开即按窗口容量")).toBeTruthy();
  });

  // The footnote answers "why 160k"; the chosen mode answers "what this does".
  // Stacked, they are two grey paragraphs in one narrow column.
  it("stands the footnote down while the editor is open", async () => {
    await mount();
    expect(screen.getByText(/不随窗口放大/)).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "160k" }));
    await screen.findByRole("slider", { name: "维护点" });
    expect(screen.queryByText(/不随窗口放大/)).toBeNull();
  });

  // The write rebuilds the runtime, so the gauge that comes back is the kernel's
  // — not this reply's arithmetic. A panel that kept its old figure would show a
  // fold point the session no longer folds at.
  // The far end is not "a very large threshold" — it is the other answer, the
  // one that retires the economic bound and leaves the window share firing.
  it("reads the far end as capacity-only, and hands back the kernel's gauge", async () => {
    const { onCtx } = await mount();
    await userEvent.click(screen.getByRole("button", { name: "160k" }));
    const handle = await screen.findByRole("slider", { name: "维护点" });
    fireEvent.change(handle, { target: { value: "850000" } });
    fireEvent.pointerUp(handle);
    await waitFor(() => expect(onCtx).toHaveBeenCalled());
    expect(onCtx.mock.calls.at(-1)?.[0].compact_at).toBe(850_000);
    expect(onCtx.mock.calls.at(-1)?.[0].boundary).toBe("capacity");
  });

  // Holding the handle owns the figure above it. A mark that moves under the
  // pointer while its own reading stays put is two controls disagreeing.
  it("moves the reading with the handle before anything is written", async () => {
    const { onCtx } = await mount();
    await userEvent.click(screen.getByRole("button", { name: "160k" }));
    const handle = await screen.findByRole("slider", { name: "维护点" });
    fireEvent.change(handle, { target: { value: "400000" } });
    expect(screen.getByRole("button", { name: "400k" })).toBeTruthy();
    expect(onCtx).not.toHaveBeenCalled();
  });

  // Two editors in one column, each answering for a different ceiling.
  it("closes the window editor when the fold editor opens", async () => {
    await mount();
    await userEvent.click(screen.getByRole("button", { name: "1.0M" }));
    expect(screen.getByLabelText("上下文窗口（tokens）")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "160k" }));
    await screen.findByRole("slider", { name: "维护点" });
    expect(screen.queryByLabelText("上下文窗口（tokens）")).toBeNull();
  });
});
