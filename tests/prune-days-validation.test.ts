import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { pruneSessionsCommand } from "../src/cli/commands/prune-sessions.js";
import * as sessionModule from "../src/memory/session.js";

// Replicated coercer from src/cli/index.ts
const coerceDays = (v: string) => Number(v);

// Replicated guard check from src/cli/commands/prune-sessions.ts
const isValidDays = (days: any) => Number.isInteger(days) && days >= 1;

describe("prune-sessions --days validation", () => {
  describe("replicated logic", () => {
    it("should reject 1.5", () => {
      const parsed = coerceDays("1.5");
      expect(parsed).toBe(1.5);
      expect(isValidDays(parsed)).toBe(false);
    });

    it("should accept 1e2 and yield 100", () => {
      const parsed = coerceDays("1e2");
      expect(parsed).toBe(100);
      expect(isValidDays(parsed)).toBe(true);
    });

    it("should reject abc", () => {
      const parsed = coerceDays("abc");
      expect(Number.isNaN(parsed)).toBe(true);
      expect(isValidDays(parsed)).toBe(false);
    });

    it("should accept 5", () => {
      const parsed = coerceDays("5");
      expect(parsed).toBe(5);
      expect(isValidDays(parsed)).toBe(true);
    });
  });

  describe("pruneSessionsCommand integration", () => {
    let exitSpy: any;
    let errorSpy: any;

    beforeEach(() => {
      exitSpy = vi.spyOn(process, "exit").mockImplementation((() => {
        throw new Error("process.exit");
      }) as any);
      errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
      vi.spyOn(sessionModule, "listSessions").mockReturnValue([]);
    });

    afterEach(() => {
      vi.restoreAllMocks();
    });

    it("errors and exits on 1.5", () => {
      expect(() => pruneSessionsCommand({ days: coerceDays("1.5"), dryRun: true })).toThrow(
        "process.exit",
      );
      expect(exitSpy).toHaveBeenCalledWith(1);
      expect(errorSpy).toHaveBeenCalled();
    });

    it("works (does not exit) on 1e2", () => {
      pruneSessionsCommand({ days: coerceDays("1e2"), dryRun: true });
      expect(exitSpy).not.toHaveBeenCalled();
      expect(errorSpy).not.toHaveBeenCalled();
    });

    it("errors and exits on abc", () => {
      expect(() => pruneSessionsCommand({ days: coerceDays("abc"), dryRun: true })).toThrow(
        "process.exit",
      );
      expect(exitSpy).toHaveBeenCalledWith(1);
      expect(errorSpy).toHaveBeenCalled();
    });

    it("works (does not exit) on 5", () => {
      pruneSessionsCommand({ days: coerceDays("5"), dryRun: true });
      expect(exitSpy).not.toHaveBeenCalled();
      expect(errorSpy).not.toHaveBeenCalled();
    });
  });
});
