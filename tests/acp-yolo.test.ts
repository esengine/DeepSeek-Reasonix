import { describe, expect, it } from "vitest";
import { PauseGate } from "../src/core/pause-gate.js";
import { autoResolveVerdict } from "../src/core/pause-policy.js";

// Mirrors the listener body in src/cli/commands/acp.ts.
function makeListener(opts: { yolo?: boolean }, configEditMode: "review" | "auto" | "yolo") {
  return (gate: PauseGate, onBridge: (reqId: number) => void) => {
    gate.on((req) => {
      const editMode = opts.yolo ? "yolo" : configEditMode;
      const auto = autoResolveVerdict(req, editMode);
      if (auto !== null) {
        gate.resolve(req.id, auto as never);
        return;
      }
      onBridge(req.id);
    });
  };
}

describe("acp --yolo", () => {
  it("auto-continues plan_checkpoint when opts.yolo is true even if config says review", async () => {
    const gate = new PauseGate();
    let bridged = false;
    makeListener({ yolo: true }, "review")(gate, () => {
      bridged = true;
    });

    const promise = gate.ask({
      kind: "plan_checkpoint",
      payload: { stepId: "s1", result: "done" },
    });

    await expect(promise).resolves.toEqual({ type: "continue" });
    expect(bridged).toBe(false);
  });

  it("bridges plan_checkpoint to the client when opts.yolo is false and config is review", async () => {
    const gate = new PauseGate();
    let bridgedReqId: number | null = null;
    makeListener({ yolo: false }, "review")(gate, (id) => {
      bridgedReqId = id;
      gate.resolve(id, { type: "continue" } as never);
    });

    await gate.ask({ kind: "plan_checkpoint", payload: { stepId: "s1", result: "done" } });
    expect(bridgedReqId).not.toBeNull();
  });

  it("falls back to config editMode when opts.yolo is undefined", async () => {
    const gate = new PauseGate();
    let bridged = false;
    makeListener({}, "auto")(gate, () => {
      bridged = true;
    });

    const promise = gate.ask({
      kind: "plan_checkpoint",
      payload: { stepId: "s1", result: "done" },
    });
    await expect(promise).resolves.toEqual({ type: "continue" });
    expect(bridged).toBe(false);
  });

  it("does NOT auto-resolve run_command requests even with --yolo (shell.ts handles that via allowAll)", async () => {
    const gate = new PauseGate();
    let bridgedReqId: number | null = null;
    makeListener({ yolo: true }, "review")(gate, (id) => {
      bridgedReqId = id;
      gate.resolve(id, { type: "run_once" } as never);
    });

    await gate.ask({ kind: "run_command", payload: { command: "rm -rf /" } });
    expect(bridgedReqId).not.toBeNull();
  });

  it("auto-allows path_access (run_once) when yolo — mirrors shell.ts allowAll bypass", async () => {
    const gate = new PauseGate();
    let bridged = false;
    makeListener({ yolo: true }, "review")(gate, () => {
      bridged = true;
    });

    const promise = gate.ask({
      kind: "path_access",
      payload: {
        path: "/tmp/foo",
        intent: "read",
        toolName: "read_file",
        sandboxRoot: "/work",
        allowPrefix: "/tmp",
      },
    });
    await expect(promise).resolves.toEqual({ type: "run_once" });
    expect(bridged).toBe(false);
  });

  it("bridges path_access to the client in auto mode (only yolo bypasses)", async () => {
    const gate = new PauseGate();
    let bridgedReqId: number | null = null;
    makeListener({ yolo: false }, "auto")(gate, (id) => {
      bridgedReqId = id;
      gate.resolve(id, { type: "deny" } as never);
    });

    await gate.ask({
      kind: "path_access",
      payload: {
        path: "/etc/passwd",
        intent: "read",
        toolName: "read_file",
        sandboxRoot: "/work",
        allowPrefix: "/etc",
      },
    });
    expect(bridgedReqId).not.toBeNull();
  });
});
