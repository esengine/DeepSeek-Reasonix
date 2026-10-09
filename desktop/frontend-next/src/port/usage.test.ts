import { afterEach, describe, expect, it, vi } from "vitest";
import { SsePort } from "./sse";

afterEach(() => vi.unstubAllGlobals());

describe("usage range query", () => {
  it("sends explicit calendar dates instead of folding them back into a day count", async () => {
    let called = "";
    vi.stubGlobal("fetch", async (url: string) => {
      called = url;
      return { ok: true, json: async () => ({}) } as Response;
    });

    await new SsePort().usage({ from: "2026-08-01", to: "2026-08-31" });

    expect(called).toBe("/usage?from=2026-08-01&to=2026-08-31");
  });
});
