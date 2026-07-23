/**
 * Workbench target helpers — dispatch shape only (no live Wails).
 */
import assert from "node:assert/strict";
import { describe, it, beforeEach, afterEach } from "node:test";

const g = globalThis as any;

describe("workbenchTarget", () => {
  beforeEach(() => {
    g.window = {
      go: {
        main: {
          App: {
            WorkbenchActiveTarget: async () => ({ kind: "local", identityGen: 1, requestSeq: 1 }),
            WorkbenchLastRemoteHint: async () => ({ hostId: "lab", workspace: "/w" }),
            RemoteLastWorkspace: async () => "/remembered",
            WorkbenchSwitchLocal: async () => ({ kind: "local", identityGen: 2, requestSeq: 2 }),
            WorkbenchConnectRemote: async () => undefined,
            WorkbenchDisconnectRemote: async () => undefined,
            WorkbenchRemoteRequest: async (_m: unknown, body: unknown) =>
              JSON.stringify({ ok: true, body }),
            WorkbenchResolveProviderTrust: async () => undefined,
            WorkbenchPendingProviderTrust: async () => null,
          },
        },
      },
    };
  });
  afterEach(() => {
    delete g.window;
  });

  it("reads active target and remote hint", async () => {
    const mod = await import("../lib/workbenchTarget");
    const active = await mod.fetchActiveTarget();
    assert.equal(active.kind, "local");
    const hint = await mod.fetchLastRemoteHint();
    assert.equal(hint?.hostId, "lab");
  });

  it("proxies remote request JSON", async () => {
    const mod = await import("../lib/workbenchTarget");
    const res = (await mod.remoteRequest("session/list", { x: 1 })) as { ok: boolean };
    assert.equal(res.ok, true);
  });

  it("prefers the last workspace, then the host default, and otherwise requires selection", async () => {
    const mod = await import("../lib/workbenchTarget");
    assert.equal(mod.resolveRemoteWorkspace(" /last ", "/default"), "/last");
    assert.equal(mod.resolveRemoteWorkspace(" ", " /default "), "/default");
    assert.equal(mod.resolveRemoteWorkspace("", ""), "");
    assert.equal(await mod.preferredRemoteWorkspace("lab", "/default"), "/remembered");

    g.window.go.main.App.RemoteLastWorkspace = async () => {
      throw new Error("preferences unavailable");
    };
    assert.equal(await mod.preferredRemoteWorkspace("lab", "/default"), "/default");
    assert.equal(await mod.preferredRemoteWorkspace("lab", ""), "");
  });

  it("fences the composer only while a candidate target is connecting", async () => {
    const mod = await import("../lib/workbenchTarget");
    assert.equal(mod.workbenchTargetTransitioning({ kind: "local", state: "connecting" }), true);
    assert.equal(mod.workbenchTargetTransitioning({ kind: "local", state: "disconnected" }), false);
    assert.equal(mod.workbenchTargetTransitioning({ kind: "ssh", state: "connected" }), false);
  });
});
