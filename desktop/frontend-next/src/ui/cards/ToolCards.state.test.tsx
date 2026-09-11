// @vitest-environment jsdom
import { act } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { ReadsCard } from "./ReadsCard";
import { ToolCard } from "./ToolCard";

afterEach(cleanup);

describe("tool outcome cards", () => {
  it("keeps a failed read visible after reads are folded", () => {
    const { container } = render(<ReadsCard tools={[
      { id: "ok", name: "read_file", args: '{"path":"a.ts"}', output: "1→ok", readOnly: true },
      { id: "bad", name: "read_file", args: '{"path":"b.ts"}', err: "no such file", readOnly: true },
    ]} />);
    expect(screen.getByText("1 项失败")).toBeTruthy();
    expect(container.querySelector('[data-call="bad"][data-bad]')).toBeTruthy();
  });

  it("marks an err-only tool result as failed in its heading", () => {
    render(<ToolCard tool={{ id: "bad", name: "bash", err: "boom", readOnly: false }} running={false} />);
    expect(screen.getByText("失败")).toBeTruthy();
    expect(screen.getByText("boom")).toBeTruthy();
  });
});

// A command that runs for half a minute drew one line of text and a 1.9s pulse
// on a 14px glyph for the whole of it: measured over the fixture's own run, the
// card of a 7.4s call went through 4 distinct texts, and the four calls under
// 600ms through one each. A pulse reads the same at two seconds and at two
// minutes, which is the difference between "working" and "dead" going unsaid.
describe("what a running call says about the wait", () => {
  afterEach(() => vi.useRealTimers());

  it("reports how long it has been running, and says nothing before a second", () => {
    vi.useFakeTimers();
    const tool = { id: "run", name: "bash", args: '{"command":"go test ./..."}', readOnly: false };
    const { container } = render(<ToolCard tool={tool} running />);
    // Nothing yet: a call that answers this fast never looked stuck, and a
    // digit that appears and leaves is its own noise.
    expect(container.querySelector(".cost .live")).toBeNull();

    act(() => { vi.advanceTimersByTime(4000); });
    expect(container.querySelector(".cost .live")?.textContent).toBe("4s");

    act(() => { vi.advanceTimersByTime(8000); });
    expect(container.querySelector(".cost .live")?.textContent).toBe("12s");
  });

  // The slot is the one the settled duration lands in, so the number stops
  // rather than being replaced by a different value in a different place.
  it("hands the slot to the settled duration and stops counting", () => {
    vi.useFakeTimers();
    const tool = { id: "run", name: "bash", readOnly: false };
    const { container, rerender } = render(<ToolCard tool={tool} running />);
    act(() => { vi.advanceTimersByTime(3000); });
    expect(container.querySelector(".cost .live")).toBeTruthy();

    rerender(<ToolCard tool={{ ...tool, durationMs: 3120 }} running={false} />);
    expect(container.querySelector(".cost .live")).toBeNull();
    expect(container.querySelector(".cost")?.textContent).toContain("3.1s");
  });
});
