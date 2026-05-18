import { describe, expect, it } from "vitest";
import { RegexRunner } from "../src/tools/fs/regex-runner.js";

describe("RegexRunner — worker-isolated regex evaluation", () => {
  it("returns line indices for matches against a literal pattern", async () => {
    const runner = new RegexRunner();
    try {
      const text = "alpha\nbeta\nalpha-two\ngamma";
      const hits = await runner.testLines(text, "alpha", "i");
      expect(hits).toEqual([0, 2]);
    } finally {
      await runner.shutdown();
    }
  });

  it("propagates a regex syntax error from the worker", async () => {
    const runner = new RegexRunner();
    try {
      await expect(runner.testLines("anything", "[unterminated", "i")).rejects.toThrow();
    } finally {
      await runner.shutdown();
    }
  });

  it("kills the worker on catastrophic backtracking + reports a timeout (issue #1236)", async () => {
    // (a+)+! on a 30-char run of 'a' is exponential — would take tens of
    // seconds inside re.test. With a 300 ms timeout the worker is terminated
    // and the call rejects fast.
    const runner = new RegexRunner({ defaultTimeoutMs: 300 });
    try {
      const evilLine = "a".repeat(30);
      const start = Date.now();
      await expect(runner.testLines(evilLine, "(a+)+!", "")).rejects.toThrow(/exceeded/);
      expect(Date.now() - start).toBeLessThan(2000);
    } finally {
      await runner.shutdown();
    }
  });

  it("the worker is respawned after a timeout — next call still works", async () => {
    const runner = new RegexRunner({ defaultTimeoutMs: 300 });
    try {
      await expect(runner.testLines("a".repeat(30), "(a+)+!", "")).rejects.toThrow(/exceeded/);
      // Next call uses a fresh worker.
      const hits = await runner.testLines("hello\nworld", "world", "");
      expect(hits).toEqual([1]);
    } finally {
      await runner.shutdown();
    }
  });

  it("an aborted signal rejects immediately and tears down the worker", async () => {
    const runner = new RegexRunner({ defaultTimeoutMs: 60_000 });
    try {
      const ctrl = new AbortController();
      const pending = runner.testLines("a".repeat(30), "(a+)+!", "", { signal: ctrl.signal });
      setTimeout(() => ctrl.abort(), 50);
      const start = Date.now();
      await expect(pending).rejects.toThrow(/aborted/);
      expect(Date.now() - start).toBeLessThan(2000);
    } finally {
      await runner.shutdown();
    }
  });
});
