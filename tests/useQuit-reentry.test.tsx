// @vitest-environment jsdom

import { cleanup, render } from "@testing-library/react";
import React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useQuit } from "../src/cli/ui/hooks/useQuit.js";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("useQuit reentry guard", () => {
  it("quittingRef prevents multiple process.exit calls", async () => {
    const exitSpy = vi.spyOn(process, "exit").mockImplementation(() => undefined as never);
    const endSpy = vi.fn();
    const transcriptRef = { current: { end: endSpy } } as never;
    const beforeQuit = vi.fn();

    let quitFn: ReturnType<typeof useQuit> | undefined;
    function Harness() {
      quitFn = useQuit(transcriptRef, { beforeQuit });
      return null;
    }

    render(<Harness />);
    expect(quitFn).toBeDefined();

    quitFn!();
    await vi.waitFor(() => {
      expect(exitSpy).toHaveBeenCalledTimes(1);
    });
    expect(endSpy).toHaveBeenCalledTimes(1);
    expect(beforeQuit).toHaveBeenCalledTimes(1);

    exitSpy.mockClear();
    endSpy.mockClear();
    beforeQuit.mockClear();

    quitFn!();
    await new Promise((r) => setTimeout(r, 50));
    expect(exitSpy).not.toHaveBeenCalled();
    expect(endSpy).not.toHaveBeenCalled();
    expect(beforeQuit).not.toHaveBeenCalled();
  });

  it("beforeQuit error does not prevent process.exit", async () => {
    const exitSpy = vi.spyOn(process, "exit").mockImplementation(() => undefined as never);
    const endSpy = vi.fn();
    const transcriptRef = { current: { end: endSpy } } as never;
    const beforeQuit = vi.fn().mockRejectedValue(new Error("beforeQuit boom"));

    let quitFn: ReturnType<typeof useQuit> | undefined;
    function Harness() {
      quitFn = useQuit(transcriptRef, { beforeQuit });
      return null;
    }

    render(<Harness />);

    quitFn!();
    await vi.waitFor(() => {
      expect(exitSpy).toHaveBeenCalledTimes(1);
    });
  });

  it("null transcriptRef does not crash", async () => {
    const exitSpy = vi.spyOn(process, "exit").mockImplementation(() => undefined as never);
    const transcriptRef = { current: null };

    let quitFn: ReturnType<typeof useQuit> | undefined;
    function Harness() {
      quitFn = useQuit(transcriptRef as never);
      return null;
    }

    render(<Harness />);

    quitFn!();
    await vi.waitFor(() => {
      expect(exitSpy).toHaveBeenCalledTimes(1);
    });
  });
});
