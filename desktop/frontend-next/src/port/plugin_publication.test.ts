// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { SsePort } from "./sse";
import { SseExtensions } from "./sse_ext";
import type { PluginPlan } from "./plugin";

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

it.each([
  ["Studio", () => new SsePort("/rt/neutral")],
  ["extension port", () => new SseExtensions("/rt/neutral")],
] as const)("preserves publication and content causes in %s", async (_name, make) => {
  const port = make();
  const plan: PluginPlan = {
    ok: false, status: "failed", applied: true,
    actions: [{ kind: "plugin", action: "install_plugin_package", status: "failed", riskLevel: "medium", error: "detail", errorCode: "install.publication_failed" }],
  };
  vi.stubGlobal("fetch", vi.fn().mockImplementation(async () => Response.json(plan)));
  expect((await port.installPlugin({ source: "/neutral", replace: true, planId: "approved" })).actions?.[0].errorCode).toBe("install.publication_failed");
  plan.actions![0].errorCode = "install.digest_mismatch";
  expect((await port.installPlugin({ source: "/neutral", replace: true, planId: "approved" })).actions?.[0].errorCode).toBe("install.digest_mismatch");
  plan.ok = true; plan.status = "planned"; plan.applied = false;
  plan.actions![0].status = "planned"; delete plan.actions![0].error; delete plan.actions![0].errorCode;
  expect((await port.planPlugin({ source: "/neutral" })).status).toBe("planned");
});
