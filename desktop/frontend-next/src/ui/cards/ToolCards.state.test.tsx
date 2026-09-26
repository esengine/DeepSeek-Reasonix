// @vitest-environment jsdom
import { act } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
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

  // The reader is watching the agent operate a page or an application they
  // cannot see. "[image: screenshot]" is the tool telling the model a picture
  // exists; it is not the person being shown one.
  it("shows what the call showed the model, and enlarges one on request", () => {
    const shot = "data:image/jpeg;base64,AAAA";
    const { container } = render(
      <ToolCard tool={{ id: "shot", name: "computer_read", output: "Notes: front window\n[image: screenshot]", images: [shot], readOnly: false }} running={false} />,
    );
    const thumb = container.querySelector<HTMLButtonElement>('[data-action="tool.image-open"]');
    expect(thumb?.querySelector("img")?.getAttribute("src")).toBe(shot);
    expect(container.querySelector(".tshot-full")).toBeNull();
    act(() => thumb!.click());
    expect(container.querySelector<HTMLImageElement>(".tshot-full img")?.getAttribute("src")).toBe(shot);
    // A picture that fills the window has to close without a mouse.
    act(() => { document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" })); });
    expect(container.querySelector(".tshot-full")).toBeNull();
  });

  it("says nothing where a call showed nothing", () => {
    const { container } = render(<ToolCard tool={{ id: "plain", name: "bash", output: "done", readOnly: false }} running={false} />);
    expect(container.querySelector(".tshots")).toBeNull();
  });

  it("marks an err-only tool result as failed in its heading", () => {
    render(<ToolCard tool={{ id: "bad", name: "bash", err: "boom", readOnly: false }} running={false} />);
    expect(screen.getByText("失败")).toBeTruthy();
    expect(screen.getByText("boom")).toBeTruthy();
  });

  it("keeps a completed result compact until its row is opened", () => {
    const { container } = render(<ToolCard tool={{ id: "ok", name: "bash", args: '{"command":"npm test"}', output: "passed", readOnly: false }} running={false} />);
    const disclosure = container.querySelector("details.tool-disclosure") as HTMLDetailsElement;
    expect(disclosure.open).toBe(false);
    expect(screen.getByText("运行命令")).toBeTruthy();
    fireEvent.click(disclosure.querySelector("summary")!);
    expect(disclosure.open).toBe(true);
  });

  it("summarises edit size before the diff is opened", () => {
    render(<ToolCard tool={{ id: "edit", name: "edit_file", args: '{"path":"src/App.tsx"}', diff: "+new\n-old", readOnly: false }} running={false} />);
    expect(screen.getByLabelText("新增 1 行，删除 1 行").textContent).toBe("+1−1");
  });

  it("uses the prototype's compact audit row and structured details inside activity", () => {
    const { container } = render(
      <ToolCard
        activity
        tool={{ id: "call_example123", name: "read_file", args: '{"path":"README.md"}', output: "hello", readOnly: true }}
        running={false}
      />,
    );
    expect(screen.getByText("read_file")).toBeTruthy();
    const disclosure = container.querySelector("details.tool-disclosure") as HTMLDetailsElement;
    fireEvent.click(disclosure.querySelector("summary")!);
    expect(disclosure.open).toBe(true);
    expect(screen.getByText("调用 ample123")).toBeTruthy();
    expect(screen.getByText("输入")).toBeTruthy();
    expect(screen.getByText("结果")).toBeTruthy();
    expect(screen.getAllByText(/README\.md/)).toHaveLength(2);
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

// curl, wget, pip and most download tools redraw their meter by returning to
// column zero, and a browser draws a carriage return as a space: every frame
// the command ever printed lands on one line that grows for the whole download.
describe("a command that redraws its own line", () => {
  const terms = (container: HTMLElement) => [...container.querySelectorAll("pre.term")].map((p) => p.textContent);

  it("shows the latest frame of a progress meter, not every frame side by side", () => {
    const output = "  % Total\n  5 10.0M    5  512k\r 50 10.0M   50 5120k\r100 10.0M  100 10.0M\nsaved";
    const { container } = render(<ToolCard tool={{ id: "dl", name: "bash", args: '{"command":"curl -O"}', output, readOnly: false }} running />);
    expect(terms(container)).toEqual(["  % Total\n100 10.0M  100 10.0M\nsaved"]);
  });

  it("keeps what a shorter frame does not cover, as a terminal would", () => {
    const { container } = render(<ToolCard tool={{ id: "dl", name: "bash", output: "downloading 100%\rdone\n", readOnly: false }} running={false} />);
    expect(terms(container)).toEqual(["doneloading 100%\n"]);
  });

  it("treats a CRLF line ending as a line ending", () => {
    const { container } = render(<ToolCard tool={{ id: "win", name: "bash", output: "one\r\ntwo\r\n", readOnly: false }} running={false} />);
    expect(terms(container)).toEqual(["one\ntwo\n"]);
  });

  it("holds the last frame while the next one is still arriving", () => {
    const { container } = render(<ToolCard tool={{ id: "dl", name: "bash", output: "a\n 40%\r", readOnly: false }} running />);
    expect(terms(container)).toEqual(["a\n 40%"]);
  });
});
