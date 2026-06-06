import { describe, expect, it } from "vitest";
import { ToolRateLimiter, normalizeToolRateLimitConfig } from "../src/tools/rate-limit.js";

describe("ToolRateLimiter", () => {
  it("allows calls under the aggregate limit", () => {
    const now = 1_000;
    const limiter = new ToolRateLimiter(
      { aggregate: { maxCalls: 2, windowSeconds: 60 }, tools: {} },
      () => now,
    );

    expect(limiter.consume("read_file").allowed).toBe(true);
    expect(limiter.consume("search_files").allowed).toBe(true);
  });

  it("blocks the first call over the aggregate limit", () => {
    const now = 1_000;
    const limiter = new ToolRateLimiter(
      { aggregate: { maxCalls: 2, windowSeconds: 60 }, tools: {} },
      () => now,
    );

    limiter.consume("read_file");
    limiter.consume("search_files");
    const blocked = limiter.consume("list_directory");

    expect(blocked.allowed).toBe(false);
    expect(blocked.result).toMatchObject({
      error: "rate_limited",
      scope: "all_tools",
      limit: 2,
      windowSeconds: 60,
    });
    expect(blocked.result?.retryAfterMs).toBe(60_000);
  });

  it("lets a per-tool bucket block before the aggregate bucket", () => {
    const now = 1_000;
    const limiter = new ToolRateLimiter(
      {
        aggregate: { maxCalls: 10, windowSeconds: 60 },
        tools: { run_command: { maxCalls: 1, windowSeconds: 60 } },
      },
      () => now,
    );

    expect(limiter.consume("run_command").allowed).toBe(true);
    const blocked = limiter.consume("run_command");

    expect(blocked.allowed).toBe(false);
    expect(blocked.result).toMatchObject({
      tool: "run_command",
      scope: "run_command",
      limit: 1,
      windowSeconds: 60,
    });
  });

  it("per-tool false disables only that tool bucket while aggregate still applies", () => {
    const now = 1_000;
    const limiter = new ToolRateLimiter(
      {
        aggregate: { maxCalls: 2, windowSeconds: 60 },
        tools: { run_command: false },
      },
      () => now,
    );

    expect(limiter.consume("run_command").allowed).toBe(true);
    expect(limiter.consume("run_command").allowed).toBe(true);
    const blocked = limiter.consume("run_command");

    expect(blocked.allowed).toBe(false);
    expect(blocked.result?.scope).toBe("all_tools");
  });

  it("expires old timestamps after the window", () => {
    let now = 1_000;
    const limiter = new ToolRateLimiter(
      { aggregate: { maxCalls: 1, windowSeconds: 1 }, tools: {} },
      () => now,
    );

    expect(limiter.consume("read_file").allowed).toBe(true);
    expect(limiter.consume("read_file").allowed).toBe(false);
    now = 2_001;
    expect(limiter.consume("read_file").allowed).toBe(true);
  });

  it("enabled false never blocks", () => {
    const now = 1_000;
    const limiter = new ToolRateLimiter(false, () => now);

    expect(limiter.consume("run_command").allowed).toBe(true);
    expect(limiter.consume("run_command").allowed).toBe(true);
    expect(limiter.consume("run_command").allowed).toBe(true);
  });

  it("normalizes invalid values to defaults", () => {
    const normalized = normalizeToolRateLimitConfig({
      aggregate: { maxCalls: 0, windowSeconds: 0 },
      tools: {
        run_command: { maxCalls: -1, windowSeconds: 1.5 },
        custom_tool: { maxCalls: 3, windowSeconds: 4 },
      },
    });

    expect(normalized).toMatchObject({
      aggregate: { maxCalls: 200, windowSeconds: 60 },
      tools: {
        run_command: { maxCalls: 60, windowSeconds: 60 },
        run_background: { maxCalls: 10, windowSeconds: 60 },
        custom_tool: { maxCalls: 3, windowSeconds: 4 },
      },
    });
  });
});
