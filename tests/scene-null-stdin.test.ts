import { describe, expect, it, vi } from "vitest";
import { makeNullStdin } from "../src/cli/ui/scene/null-stdin.js";

describe("makeNullStdin", () => {
  it("never emits a data event", () => {
    const stdin = makeNullStdin();
    const handler = vi.fn();
    stdin.on("data", handler);
    return new Promise<void>((resolve) => {
      setTimeout(() => {
        expect(handler).not.toHaveBeenCalled();
        resolve();
      }, 30);
    });
  });

  it("reports isTTY=true so Ink's tty detection passes", () => {
    const stdin = makeNullStdin();
    expect(stdin.isTTY).toBe(true);
  });

  it("setRawMode is a no-op that returns the stream", () => {
    const stdin = makeNullStdin();
    expect(() => stdin.setRawMode(true)).not.toThrow();
    expect(stdin.setRawMode(true)).toBe(stdin);
  });

  it("pause / resume / isPaused are safe no-ops", () => {
    const stdin = makeNullStdin();
    expect(() => stdin.pause()).not.toThrow();
    expect(() => stdin.resume()).not.toThrow();
    expect(stdin.isPaused()).toBe(false);
  });

  it("setEncoding is a no-op that returns the stream", () => {
    const stdin = makeNullStdin();
    expect(() => stdin.setEncoding("utf8")).not.toThrow();
    expect(stdin.setEncoding("utf8")).toBe(stdin);
  });

  it("ref and unref are safe no-ops", () => {
    const stdin = makeNullStdin();
    expect(() => stdin.ref()).not.toThrow();
    expect(() => stdin.unref()).not.toThrow();
  });
});
