import { describe, expect, it } from "vitest";
import {
  resolveApprovalPrompt,
  toApprovalPrompt,
} from "../src/approval-prompt.js";

describe("toApprovalPrompt", () => {
  describe("shell (run_command)", () => {
    it("produces correct metadata", () => {
      const p = toApprovalPrompt({
        id: 1,
        kind: "run_command",
        payload: { command: "npm test", cwd: "/home/user/project", timeoutSec: 120 },
      });
      expect(p.kind).toBe("shell");
      expect(p.tone).toBe("warn");
      expect(p.title).toBe("Run command");
      expect(p.subtitle).toBe("npm test");
      expect(p.preview).toBe("npm test");
      expect(p.meta).toEqual({ cwd: "/home/user/project", timeout: "120s" });
    });

    it("derives two-token prefix for known wrappers", () => {
      const p = toApprovalPrompt({
        id: 1,
        kind: "run_command",
        payload: { command: "npm install" },
      });
      const always = p.actions.find((a) => a.id === "always_allow")!;
      expect(always.label).toBe("Always allow — npm install");
      expect(p.data?.prefix).toBe("npm install");
    });

    it("derives single-token prefix for unknown commands", () => {
      const p = toApprovalPrompt({
        id: 1,
        kind: "run_command",
        payload: { command: "ls -la" },
      });
      expect(p.data?.prefix).toBe("ls");
      expect(p.actions.find((a) => a.id === "always_allow")!.label).toBe("Always allow — ls");
    });

    it("includes secondaryInput on deny action", () => {
      const p = toApprovalPrompt({
        id: 1,
        kind: "run_command",
        payload: { command: "echo hi" },
      });
      const deny = p.actions.find((a) => a.id === "deny")!;
      expect(deny.secondaryInput).toEqual({
        hint: "Reason for denial (optional)",
        required: false,
      });
    });

    it("omits meta when no optional fields present", () => {
      const p = toApprovalPrompt({
        id: 1,
        kind: "run_command",
        payload: { command: "echo hi" },
      });
      expect(p.meta).toBeUndefined();
    });
  });

  describe("shell (run_background)", () => {
    it("uses background title and wait meta", () => {
      const p = toApprovalPrompt({
        id: 2,
        kind: "run_background",
        payload: { command: "node server.js", waitSec: 3 },
      });
      expect(p.title).toBe("Run background command");
      expect(p.meta).toEqual({ wait: "3s" });
    });
  });

  describe("path_access", () => {
    it("produces read-flavored labels", () => {
      const p = toApprovalPrompt({
        id: 3,
        kind: "path_access",
        payload: {
          path: "/etc/passwd",
          intent: "read",
          toolName: "read_file",
          sandboxRoot: "/home/user",
          allowPrefix: "/etc",
        },
      });
      expect(p.kind).toBe("path");
      expect(p.title).toBe("Access path — read");
      expect(p.actions.find((a) => a.id === "run_once")!.label).toBe("Allow read");
    });

    it("produces write-flavored labels", () => {
      const p = toApprovalPrompt({
        id: 3,
        kind: "path_access",
        payload: {
          path: "/tmp/out.txt",
          intent: "write",
          toolName: "write_file",
          sandboxRoot: "/home/user",
          allowPrefix: "/tmp",
        },
      });
      expect(p.title).toBe("Access path — write");
      expect(p.actions.find((a) => a.id === "run_once")!.label).toBe("Allow write");
    });

    it("stores allowPrefix in data", () => {
      const p = toApprovalPrompt({
        id: 3,
        kind: "path_access",
        payload: {
          path: "/tmp/x",
          intent: "read",
          toolName: "read_file",
          sandboxRoot: "/home/user",
          allowPrefix: "/tmp",
        },
      });
      expect(p.data?.prefix).toBe("/tmp");
    });

    it("includes tool → path preview", () => {
      const p = toApprovalPrompt({
        id: 3,
        kind: "path_access",
        payload: {
          path: "/etc/hosts",
          intent: "read",
          toolName: "read_file",
          sandboxRoot: "/home/user",
          allowPrefix: "/etc",
        },
      });
      expect(p.preview).toBe("read_file → /etc/hosts");
      expect(p.meta).toEqual({ sandboxRoot: "/home/user" });
    });
  });

  describe("plan_proposed", () => {
    it("uses summary as subtitle when available", () => {
      const p = toApprovalPrompt({
        id: 4,
        kind: "plan_proposed",
        payload: { plan: "Step 1\nStep 2\nStep 3", summary: "Refactor auth layer", steps: [{}, {}] },
      });
      expect(p.kind).toBe("plan");
      expect(p.tone).toBe("accent");
      expect(p.title).toBe("Approve plan");
      expect(p.subtitle).toBe("Refactor auth layer");
      expect(p.meta).toEqual({ steps: "2" });
    });

    it("falls back to truncated plan when no summary", () => {
      const p = toApprovalPrompt({
        id: 4,
        kind: "plan_proposed",
        payload: { plan: "a".repeat(100) },
      });
      expect(p.subtitle).toBe("a".repeat(80) + "…");
    });

    it("has approve/refine/cancel actions", () => {
      const p = toApprovalPrompt({
        id: 4,
        kind: "plan_proposed",
        payload: { plan: "x" },
      });
      expect(p.actions.map((a) => a.id)).toEqual(["approve", "refine", "cancel"]);
    });
  });

  describe("plan_checkpoint", () => {
    it("shows progress meta", () => {
      const p = toApprovalPrompt({
        id: 5,
        kind: "plan_checkpoint",
        payload: { stepId: "s1", title: "Migrate DB", result: "OK", completed: 2, total: 5 },
      });
      expect(p.kind).toBe("checkpoint");
      expect(p.title).toBe("Checkpoint — Migrate DB");
      expect(p.meta).toEqual({ progress: "2/5" });
    });

    it("has continue/revise/stop actions", () => {
      const p = toApprovalPrompt({
        id: 5,
        kind: "plan_checkpoint",
        payload: { stepId: "s1", result: "OK" },
      });
      expect(p.actions.map((a) => a.id)).toEqual(["continue", "revise", "stop"]);
    });
  });

  describe("plan_revision", () => {
    it("shows reason preview", () => {
      const p = toApprovalPrompt({
        id: 6,
        kind: "plan_revision",
        payload: { reason: "Tests failing", remainingSteps: [{}, {}] },
      });
      expect(p.kind).toBe("revision");
      expect(p.title).toBe("Approve plan revision");
      expect(p.subtitle).toBe("Tests failing");
      expect(p.preview).toBe("Tests failing");
      expect(p.meta).toEqual({ steps: "2" });
    });

    it("has accept/reject actions", () => {
      const p = toApprovalPrompt({
        id: 6,
        kind: "plan_revision",
        payload: { reason: "x" },
      });
      expect(p.actions.map((a) => a.id)).toEqual(["accept", "reject"]);
    });
  });

  describe("choice", () => {
    it("maps options to custom actions plus cancel", () => {
      const p = toApprovalPrompt({
        id: 7,
        kind: "choice",
        payload: {
          question: "Pick a colour",
          options: [
            { id: "red", title: "Red" },
            { id: "blue", title: "Blue" },
          ],
          allowCustom: false,
        },
      });
      expect(p.kind).toBe("choice");
      expect(p.title).toBe("Pick a colour");
      expect(p.actions.map((a) => ({ id: a.id, kind: a.kind }))).toEqual([
        { id: "red", kind: "custom" },
        { id: "blue", kind: "custom" },
        { id: "cancel", kind: "reject" },
      ]);
    });

    it("stores allowCustom flag in data", () => {
      const p = toApprovalPrompt({
        id: 7,
        kind: "choice",
        payload: { question: "x", options: [], allowCustom: true },
      });
      expect(p.data?.allowCustom).toBe(true);
    });
  });

  describe("unknown kind", () => {
    it("returns a defensive dismiss prompt", () => {
      const p = toApprovalPrompt({ id: 99, kind: "bogus", payload: {} });
      expect(p.tone).toBe("warn");
      expect(p.title).toBe("Unrecognized request");
      expect(p.actions).toEqual([{ id: "deny", label: "Dismiss", kind: "reject" }]);
    });
  });
});

describe("resolveApprovalPrompt", () => {
  describe("shell / path", () => {
    const shellPrompt = toApprovalPrompt({
      id: 1,
      kind: "run_command",
      payload: { command: "npm test" },
    });

    it("resolves run_once", () => {
      expect(resolveApprovalPrompt(shellPrompt, "run_once")).toEqual({ type: "run_once" });
    });

    it("resolves always_allow with prefix from data", () => {
      expect(resolveApprovalPrompt(shellPrompt, "always_allow")).toEqual({
        type: "always_allow",
        prefix: "npm test",
      });
    });

    it("resolves deny with secondaryInput", () => {
      expect(resolveApprovalPrompt(shellPrompt, "deny", "unsafe")).toEqual({
        type: "deny",
        denyContext: "unsafe",
      });
    });

    it("resolves deny without secondaryInput", () => {
      expect(resolveApprovalPrompt(shellPrompt, "deny")).toEqual({
        type: "deny",
        denyContext: undefined,
      });
    });

    it("returns safe default for unknown action", () => {
      expect(resolveApprovalPrompt(shellPrompt, "bogus")).toEqual({ type: "deny" });
    });
  });

  describe("plan", () => {
    const planPrompt = toApprovalPrompt({
      id: 2,
      kind: "plan_proposed",
      payload: { plan: "x" },
    });

    it("resolves approve", () => {
      expect(resolveApprovalPrompt(planPrompt, "approve")).toEqual({ type: "approve" });
    });

    it("resolves refine", () => {
      expect(resolveApprovalPrompt(planPrompt, "refine")).toEqual({ type: "refine" });
    });

    it("defaults to cancel for unknown action", () => {
      expect(resolveApprovalPrompt(planPrompt, "bogus")).toEqual({ type: "cancel" });
    });
  });

  describe("checkpoint", () => {
    const cpPrompt = toApprovalPrompt({
      id: 3,
      kind: "plan_checkpoint",
      payload: { stepId: "s1", result: "OK" },
    });

    it("resolves continue", () => {
      expect(resolveApprovalPrompt(cpPrompt, "continue")).toEqual({ type: "continue" });
    });

    it("resolves revise", () => {
      expect(resolveApprovalPrompt(cpPrompt, "revise")).toEqual({ type: "revise" });
    });

    it("defaults to stop for unknown action", () => {
      expect(resolveApprovalPrompt(cpPrompt, "bogus")).toEqual({ type: "stop" });
    });
  });

  describe("revision", () => {
    const revPrompt = toApprovalPrompt({
      id: 4,
      kind: "plan_revision",
      payload: { reason: "x" },
    });

    it("resolves accept", () => {
      expect(resolveApprovalPrompt(revPrompt, "accept")).toEqual({ type: "accepted" });
    });

    it("resolves reject", () => {
      expect(resolveApprovalPrompt(revPrompt, "reject")).toEqual({ type: "rejected" });
    });

    it("defaults to rejected for unknown action", () => {
      expect(resolveApprovalPrompt(revPrompt, "bogus")).toEqual({ type: "rejected" });
    });
  });

  describe("choice", () => {
    const chPrompt = toApprovalPrompt({
      id: 5,
      kind: "choice",
      payload: { question: "x", options: [{ id: "a", title: "A" }] },
    });

    it("resolves pick with optionId", () => {
      expect(resolveApprovalPrompt(chPrompt, "a")).toEqual({ type: "pick", optionId: "a" });
    });

    it("resolves cancel", () => {
      expect(resolveApprovalPrompt(chPrompt, "cancel")).toEqual({ type: "cancel" });
    });

    it("defaults to cancel for unknown action", () => {
      expect(resolveApprovalPrompt(chPrompt, "bogus")).toEqual({ type: "cancel" });
    });
  });
});
