import { describe, expect, it } from "vitest";
import type { Env } from "./env";
import worker from "./index";
import { workersDevGate } from "./workersdev_gate";
import toml from "../wrangler.toml?raw";

const DEV = "https://reasonix-crash-report.reasonix.workers.dev";
const CUSTOM = "https://crash.reasonix.io";
const R = "FB-7K3M-9QX2";
const req = (base: string, path: string, method = "GET") => new Request(base + path, { method });

const allowed: [string, string][] = [
  ["GET", "/v1/admin/feedback/pending?limit=20"],
  ["GET", "/v1/admin/feedback/open"],
  ["GET", "/v1/admin/feedback/replies/pending?limit=5"],
  ["POST", `/v1/admin/feedback/${R}/recorded`],
  ["POST", `/v1/admin/feedback/${R}/status`],
  ["POST", "/v1/admin/feedback/replies/abc_123-X/ack"],
];

const denied: [string, string][] = [
  ["POST", "/v1/feedback"],
  ["GET", "/v1/feedback/mine"],
  ["POST", `/v1/feedback/${R}/reply`],
  ["GET", "/v1/feedback/attachments/abc"],
  ["GET", "/v1/admin/feedback/held"],
  ["GET", "/v1/admin/feedback/blocks"],
  ["POST", "/v1/admin/feedback/block"],
  ["DELETE", "/v1/admin/feedback/block"],
  ["GET", "/v1/admin/feedback/cap"],
  ["POST", "/v1/admin/feedback/cap"],
  ["GET", "/v1/admin/feedback/replies/triage"],
  ["POST", `/v1/admin/feedback/${R}/release`],
  ["POST", `/v1/admin/feedback/${R}/reject`],
  ["POST", `/v1/admin/feedback/${R}/answer`],
  ["POST", `/v1/admin/feedback/${R}/ask`],
  ["POST", `/v1/admin/feedback/${R}/reply`],
  ["POST", `/v1/admin/feedback/${R}/takedown`],
  ["POST", `/v1/admin/feedback/${R}/link`],
  ["GET", `/v1/admin/feedback/${R}/attachments/abcdefghijklmnop`],
  ["POST", "/v1/admin/feedback/pending"],
  ["GET", `/v1/admin/feedback/${R}/recorded`],
  ["GET", `/v1/admin/feedback/${R}/status`],
  ["GET", "/v1/admin/feedback/replies/abc/ack"],
  ["POST", "/v1/admin/feedback/open"],
  ["GET", "/v1/admin/feedback/pending/"],
  ["GET", "//v1/admin/feedback/pending"],
  ["GET", "/v1/admin/packages"],
  ["GET", "/v1/admin/users"],
  ["POST", "/v1/report"],
  ["POST", "/v1/ping"],
  ["POST", "/v1/metrics"],
  ["GET", "/stats"],
  ["GET", "/admin"],
  ["GET", "/"],
  ["GET", "/v1/packages"],
  ["GET", "/v1/me/x"],
];

describe("workers.dev host gate", () => {
  it.each(allowed)("lets %s %s through on workers.dev", (method, path) => {
    expect(workersDevGate(req(DEV, path, method))).toBeNull();
  });

  it.each(denied)("answers 404 for %s %s on workers.dev", async (method, path) => {
    const res = workersDevGate(req(DEV, path, method));
    expect(res?.status).toBe(404);
    expect(await res?.text()).toBe("not found");
  });

  it("gates any workers.dev host, case and trailing dot included", () => {
    expect(workersDevGate(req("https://x.y.workers.dev", "/v1/report", "POST"))?.status).toBe(404);
    expect(workersDevGate(req("https://X.WORKERS.DEV", "/v1/report", "POST"))?.status).toBe(404);
    expect(workersDevGate(req("https://x.workers.dev.", "/v1/report", "POST"))?.status).toBe(404);
  });

  it("leaves the custom domain and lookalike hosts untouched", () => {
    for (const [method, path] of [...allowed, ...denied]) expect(workersDevGate(req(CUSTOM, path, method))).toBeNull();
    expect(workersDevGate(req("https://evilworkers.dev", "/v1/report", "POST"))).toBeNull();
    expect(workersDevGate(req("https://workers.dev.evil.com", "/v1/report", "POST"))).toBeNull();
  });
});

describe("fetch handler host gate", () => {
  const quiet = { bind: () => ({ first: async () => null, run: async () => ({}) }) };
  const env = { FEEDBACK_ENABLED: "true", DB: { prepare: () => quiet } } as unknown as Env;

  it("reaches admin auth for an allowed converter call on workers.dev", async () => {
    const res = await worker.fetch(req(DEV, "/v1/admin/feedback/open"), env);
    expect(res.status).not.toBe(404);
  });

  it("404s a public feedback route on workers.dev before any routing", async () => {
    expect((await worker.fetch(req(DEV, "/v1/feedback/mine"), env)).status).toBe(404);
  });

  it("keeps the same public route reachable on the custom domain", async () => {
    const res = await worker.fetch(req(CUSTOM, "/v1/feedback/mine"), env);
    expect(await res.text()).not.toBe("not found");
  });
});

describe("wrangler config", () => {
  it("serves workers.dev without version preview urls", () => {
    expect(toml).toMatch(/^workers_dev = true$/m);
    expect(toml).toMatch(/^preview_urls = false$/m);
  });
});
