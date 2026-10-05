// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { useRevealed } from "./reveal";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function Probe({ text, streaming = true }: { text: string; streaming?: boolean }) {
  return <span>{useRevealed(text, streaming)}</span>;
}

describe("paced streaming text", () => {
  it("stops requesting frames once it catches the current text", () => {
    const frames = new Map<number, FrameRequestCallback>();
    let id = 0;
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      frames.set(++id, cb);
      return id;
    });
    vi.stubGlobal("cancelAnimationFrame", (n: number) => frames.delete(n));

    const view = render(<Probe text="a" />);
    expect(frames.size).toBe(0);
    view.rerender(<Probe text="abcdefghij" />);
    expect(frames.size).toBe(1);

    act(() => {
      while (frames.size) {
        const [next, cb] = frames.entries().next().value as [number, FrameRequestCallback];
        frames.delete(next);
        cb(performance.now());
      }
    });
    expect(view.container.textContent).toBe("abcdefghij");
    expect(frames.size).toBe(0);
  });

  it("shows the complete stream immediately when motion is reduced", () => {
    vi.stubGlobal("matchMedia", ((query: string) => ({
      matches: true,
      media: query,
      onchange: null,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
      dispatchEvent: () => false,
    })) as typeof matchMedia);
    const view = render(<Probe text="a" />);
    view.rerender(<Probe text="all at once" />);
    expect(view.container.textContent).toBe("all at once");
  });
});
