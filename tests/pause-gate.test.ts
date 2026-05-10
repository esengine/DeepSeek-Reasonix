/** Tests for the PauseGate core — ask/resolve/on/current. */

import { describe, expect, it, vi } from "vitest";
import { type ConfirmationChoice, PauseGate } from "../src/core/pause-gate.js";

describe("PauseGate", () => {
  it("ask creates a pending request and notifies listeners", async () => {
    const gate = new PauseGate();
    const listener = vi.fn();
    gate.on(listener);

    const promise = gate.ask({ kind: "run_command", payload: { command: "echo hi" } });
    expect(listener).toHaveBeenCalledTimes(1);
    const req = listener.mock.calls[0]![0]! as { id: number; kind: string; payload: unknown };
    expect(req.kind).toBe("run_command");
    expect((req.payload as { command: string }).command).toBe("echo hi");
    expect(gate.current).not.toBeNull();
    expect(gate.current?.kind).toBe("run_command");

    gate.resolve(req.id, { type: "run_once" } as ConfirmationChoice);
    await expect(promise).resolves.toEqual({ type: "run_once" });
    expect(gate.current).toBeNull();
  });

  it("resolve with unknown id is a no-op (does not throw)", () => {
    const gate = new PauseGate();
    expect(() => gate.resolve(999, { type: "cancel" })).not.toThrow();
  });

  it("on() returns an unsubscribe function", async () => {
    const gate = new PauseGate();
    const listener = vi.fn();
    const unsub = gate.on(listener);
    unsub();

    // After unsubscribe, ask should throw (no listeners)
    expect(() => gate.ask({ kind: "run_command", payload: { command: "test" } })).toThrow(
      "no confirmation listener",
    );
    expect(listener).not.toHaveBeenCalled();
  });

  it("supports all pause kinds with their payload shapes", async () => {
    const gate = new PauseGate();
    const listener = vi.fn();
    gate.on(listener);

    const entries: Array<{ opts: { kind: string; payload: Record<string, unknown> } }> = [
      { opts: { kind: "run_command", payload: { command: "x" } } },
      { opts: { kind: "plan_proposed", payload: { plan: "#", steps: [], summary: "test" } } },
      { opts: { kind: "plan_checkpoint", payload: { stepId: "s1", result: "done" } } },
      { opts: { kind: "plan_revision", payload: { reason: "r", remainingSteps: [] } } },
      { opts: { kind: "choice", payload: { question: "q", options: [], allowCustom: false } } },
    ];

    for (const { opts } of entries) {
      const p = gate.ask(opts as any);
      const req = listener.mock.lastCall?.[0] as { kind: string };
      expect(req.kind).toBe(opts.kind);
      gate.resolve(req.id, { type: "run_once" } as ConfirmationChoice);
      await expect(p).resolves.toBeDefined();
    }
  });

  it("listener errors do not break the gate", async () => {
    const gate = new PauseGate();
    gate.on(() => {
      throw new Error("listener crash");
    });
    const listener2 = vi.fn();
    gate.on(listener2);

    const promise = gate.ask({ kind: "run_command", payload: { command: "crash" } });
    // Second listener should still fire despite the first throwing
    expect(listener2).toHaveBeenCalledTimes(1);
    const req = listener2.mock.calls[0]![0]! as { id: number };
    gate.resolve(req.id, { type: "run_once" } as ConfirmationChoice);
    await expect(promise).resolves.toEqual({ type: "run_once" });
  });

  it("CheckpointVerdict carries feedback on revise", async () => {
    const gate = new PauseGate();
    gate.on(() => {});

    const promise = gate.ask({
      kind: "plan_checkpoint",
      payload: { stepId: "step-1", result: "done" },
    });
    const req = gate.current!;
    gate.resolve(req.id, { type: "revise", feedback: "rename to auth-tokens.ts instead" });
    await expect(promise).resolves.toEqual({
      type: "revise",
      feedback: "rename to auth-tokens.ts instead",
    });
  });

  it("CheckpointVerdict revise without feedback is accepted", async () => {
    const gate = new PauseGate();
    gate.on(() => {});

    const promise = gate.ask({
      kind: "plan_checkpoint",
      payload: { stepId: "step-2", result: "added tests" },
    });
    const req = gate.current!;
    // Bare revise — no feedback string
    gate.resolve(req.id, { type: "revise" });
    await expect(promise).resolves.toEqual({ type: "revise" });
  });

  it("multiple pending requests queue independently", async () => {
    const gate = new PauseGate();
    const listener = vi.fn();
    gate.on(listener);

    const p1 = gate.ask({ kind: "run_command", payload: { command: "first" } });
    const p2 = gate.ask({ kind: "run_command", payload: { command: "second" } });

    // current should return the first one (FIFO by insertion order)
    expect((gate.current?.payload as { command: string }).command).toBe("first");

    const calls = listener.mock.calls;
    const id1 = (calls[0]![0]! as { id: number }).id;
    const id2 = (calls[1]![0]! as { id: number }).id;

    // Resolve in reverse order — should still work independently
    gate.resolve(id2, { type: "deny" } as ConfirmationChoice);
    gate.resolve(id1, { type: "run_once" } as ConfirmationChoice);

    await expect(p1).resolves.toEqual({ type: "run_once" });
    await expect(p2).resolves.toEqual({ type: "deny" });
    expect(gate.current).toBeNull();
  });

  it("emits audit events for run_once / deny / always_allow shell decisions", async () => {
    const gate = new PauseGate();
    const audit = vi.fn();
    gate.setAuditListener(audit);
    gate.on(() => {});

    const allow = gate.ask({ kind: "run_command", payload: { command: "npm test" } });
    gate.resolve(gate.current!.id, { type: "run_once" } as ConfirmationChoice);
    await expect(allow).resolves.toEqual({ type: "run_once" });

    const deny = gate.ask({ kind: "run_background", payload: { command: "npm run dev" } });
    gate.resolve(gate.current!.id, {
      type: "deny",
      denyContext: "too risky",
    } as ConfirmationChoice);
    await expect(deny).resolves.toEqual({ type: "deny", denyContext: "too risky" });

    const always = gate.ask({ kind: "run_command", payload: { command: "npm run lint" } });
    gate.resolve(gate.current!.id, {
      type: "always_allow",
      prefix: "npm run",
    } as ConfirmationChoice);
    await expect(always).resolves.toEqual({ type: "always_allow", prefix: "npm run" });

    expect(audit.mock.calls).toEqual([
      [
        {
          type: "tool.confirm.allow",
          kind: "run_command",
          payload: { command: "npm test" },
        },
      ],
      [
        {
          type: "tool.confirm.deny",
          kind: "run_background",
          payload: { command: "npm run dev" },
          denyContext: "too risky",
        },
      ],
      [
        {
          type: "tool.confirm.always_allow",
          kind: "run_command",
          payload: { command: "npm run lint" },
          prefix: "npm run",
        },
      ],
    ]);
  });

  it("cancelAll resolves every pending request with its kind's safe-cancel verdict", async () => {
    const gate = new PauseGate();
    gate.on(() => {});

    const shell = gate.ask({ kind: "run_command", payload: { command: "rm -rf /" } });
    const plan = gate.ask({ kind: "plan_proposed", payload: { plan: "#", steps: [] } });
    const cp = gate.ask({ kind: "plan_checkpoint", payload: { stepId: "s1", result: "ok" } });
    const rev = gate.ask({ kind: "plan_revision", payload: { reason: "r", remainingSteps: [] } });
    const ch = gate.ask({
      kind: "choice",
      payload: { question: "q", options: [], allowCustom: false },
    });

    gate.cancelAll();

    await expect(shell).resolves.toEqual({ type: "deny" });
    await expect(plan).resolves.toEqual({ type: "cancel" });
    await expect(cp).resolves.toEqual({ type: "stop" });
    await expect(rev).resolves.toEqual({ type: "cancelled" });
    await expect(ch).resolves.toEqual({ type: "cancel" });
    expect(gate.current).toBeNull();
  });

  it("cancelAll on an empty gate is a no-op", () => {
    const gate = new PauseGate();
    gate.on(() => {});
    expect(() => gate.cancelAll()).not.toThrow();
    expect(gate.current).toBeNull();
  });

  it("does not emit audit events for non-tool pauses", async () => {
    const gate = new PauseGate();
    const audit = vi.fn();
    gate.setAuditListener(audit);
    gate.on(() => {});

    const promise = gate.ask({
      kind: "plan_proposed",
      payload: { plan: "# Plan", steps: [], summary: "ship it" },
    });
    gate.resolve(gate.current!.id, { type: "approve" });
    await expect(promise).resolves.toEqual({ type: "approve" });
    expect(audit).not.toHaveBeenCalled();
  });
});
