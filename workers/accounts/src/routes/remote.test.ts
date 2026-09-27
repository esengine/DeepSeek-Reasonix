import { describe, expect, it } from "vitest";
import app from "../app";
import type { Bindings } from "../env";

function bindings(overrides: Partial<Bindings> = {}): Bindings {
  return {
    DB: {} as D1Database,
    APP_ORIGIN: "https://reasonix.io",
    ACCOUNT_ORIGIN: "https://id.reasonix.io",
    ALLOWED_ORIGINS: "https://reasonix.io",
    COOKIE_DOMAIN: ".reasonix.io",
    EMAIL_PROVIDER: "stub",
    MAIL_FROM: "Reasonix <test@example.com>",
    ...overrides,
  };
}

describe("remote access HTTP boundaries", () => {
  it("does not expose registered devices without an account session", async () => {
    const response = await app.request("/me/devices", {}, bindings());
    expect(response.status).toBe(401);
    await expect(response.json()).resolves.toMatchObject({ error: { code: "unauthorized" } });
  });

  it("fails closed when the gateway secret is not configured", async () => {
    const response = await app.request("/remote/grants/consume", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ ticket: "a".repeat(64) }),
    }, bindings());
    expect(response.status).toBe(503);
    await expect(response.json()).resolves.toMatchObject({ error: { code: "remote_unavailable" } });
  });

  it("rejects an incorrect gateway secret before reading a grant", async () => {
    const response = await app.request("/remote/grants/consume", {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-reasonix-gateway-token": "wrong-secret",
      },
      body: JSON.stringify({ ticket: "a".repeat(64) }),
    }, bindings({ REMOTE_GATEWAY_TOKEN: "right-secret" }));
    expect(response.status).toBe(401);
    await expect(response.json()).resolves.toMatchObject({ error: { code: "unauthorized_gateway" } });
  });
});
