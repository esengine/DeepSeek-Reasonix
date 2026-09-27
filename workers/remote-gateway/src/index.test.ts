import { afterEach, describe, expect, it, vi } from "vitest";
import worker from "./index";
import type { Env } from "./env";

interface ForwardedRequest {
  id: string;
  request: Request;
}

function environment(forwarded: ForwardedRequest[], attachments?: Record<string, unknown>): Env {
  return {
    ACCOUNT_ORIGIN: "https://id.reasonix.io",
    ALLOWED_ORIGINS: "https://reasonix.io,https://www.reasonix.io",
    REMOTE_GATEWAY_TOKEN: "gateway-secret",
    GATEWAY_LIMITER: { limit: vi.fn(async () => ({ success: true })) },
    ATTACHMENTS: {
      put: vi.fn(async () => ({} as R2Object)),
      get: vi.fn(async () => null),
      ...attachments,
    } as unknown as R2Bucket,
    REMOTE_SESSIONS: {
      idFromName(name: string) { return name as unknown as DurableObjectId; },
      get(id: DurableObjectId) {
        return {
          async fetch(request: Request) {
            forwarded.push({ id: id as unknown as string, request });
            return new Response("forwarded");
          },
        } as unknown as DurableObjectStub;
      },
    } as unknown as DurableObjectNamespace,
  };
}

function websocketRequest(path: string, token: string): Request {
  return new Request(`https://remote.reasonix.io${path}`, {
    headers: {
      authorization: `Bearer ${token}`,
      upgrade: "websocket",
      "cf-connecting-ip": "203.0.113.10",
    },
  });
}

function run(request: Request, env: Env): Promise<Response> {
  return Promise.resolve(worker.fetch!(request as never, env, {} as ExecutionContext));
}

afterEach(() => vi.unstubAllGlobals());

describe("remote gateway admission", () => {
  it("serves health without opening a session", async () => {
    const response = await run(new Request("https://remote.reasonix.io/health"), environment([]));
    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({ ok: true, service: "reasonix-remote-gateway" });
  });

  it("authenticates a device and strips its credential before Durable Object admission", async () => {
    const forwarded: ForwardedRequest[] = [];
    const deviceId = "a".repeat(64);
    const credential = "b".repeat(64);
    const accountFetch = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => Response.json({
      userId: 7,
      device: { id: deviceId, publicKey: "A".repeat(43), capabilities: ["terminal", "logs"] },
    }));
    vi.stubGlobal("fetch", accountFetch);

    const response = await run(
      websocketRequest(`/v1/devices/${deviceId}/connect`, credential),
      environment(forwarded),
    );

    expect(response.status).toBe(200);
    expect(accountFetch).toHaveBeenCalledOnce();
    const accountRequest = accountFetch.mock.calls[0]?.[1];
    expect(accountRequest?.headers).toMatchObject({ "x-reasonix-gateway-token": "gateway-secret" });
    expect(forwarded[0]?.id).toBe(deviceId);
    expect(forwarded[0]?.request.headers.get("authorization")).toBeNull();
    expect(forwarded[0]?.request.headers.get("x-reasonix-role")).toBe("device");
    expect(Number(forwarded[0]?.request.headers.get("x-reasonix-expires-at"))).toBeGreaterThan(Date.now());
  });

  it("consumes a controller grant and routes it to the target device room", async () => {
    const forwarded: ForwardedRequest[] = [];
    const targetDeviceId = "c".repeat(64);
    vi.stubGlobal("fetch", vi.fn(async () => Response.json({
      grant: { userId: 9, targetDeviceId, scopes: ["terminal"] },
    })));

    const response = await run(
      websocketRequest("/v1/sessions/connect", "d".repeat(64)),
      environment(forwarded),
    );

    expect(response.status).toBe(200);
    expect(forwarded[0]?.id).toBe(targetDeviceId);
    expect(forwarded[0]?.request.headers.get("x-reasonix-role")).toBe("controller");
    expect(forwarded[0]?.request.headers.get("x-reasonix-scopes")).toBe("terminal");
  });

  it("rejects malformed credentials before contacting the account service", async () => {
    const accountFetch = vi.fn();
    vi.stubGlobal("fetch", accountFetch);
    const response = await run(
      websocketRequest("/v1/sessions/connect", "short"),
      environment([]),
    );
    expect(response.status).toBe(401);
    expect(accountFetch).not.toHaveBeenCalled();
  });

  it("rejects browser requests from websites outside the allowlist", async () => {
    const accountFetch = vi.fn();
    vi.stubGlobal("fetch", accountFetch);
    const response = await run(new Request(`https://remote.reasonix.io/v1/attachments/${"a".repeat(64)}`, {
      headers: {
        authorization: `Bearer ${"b".repeat(64)}`,
        origin: "https://attacker.example",
      },
    }), environment([]));

    expect(response.status).toBe(403);
    expect(accountFetch).not.toHaveBeenCalled();
  });

  it("answers attachment preflight only for an allowed website", async () => {
    const response = await run(new Request(`https://remote.reasonix.io/v1/attachments/${"a".repeat(64)}`, {
      method: "OPTIONS",
      headers: { origin: "https://reasonix.io" },
    }), environment([]));

    expect(response.status).toBe(204);
    expect(response.headers.get("access-control-allow-origin")).toBe("https://reasonix.io");
    expect(response.headers.get("access-control-allow-methods")).toContain("PUT");
  });

  it("streams authorized ciphertext to R2 with hash enforcement", async () => {
    const objectId = "e".repeat(64);
    const ticket = "f".repeat(64);
    const hash = "a".repeat(64);
    const put = vi.fn(async (_key: string, _value: unknown, _options: R2PutOptions) => ({} as R2Object));
    vi.stubGlobal("fetch", vi.fn(async () => Response.json({
      attachment: {
        objectId,
        userId: 9,
        targetDeviceId: "b".repeat(64),
        maxBytes: 4,
        ciphertextBytes: 4,
        ciphertextSha256: hash,
        expiresAt: "2026-09-28T00:00:00.000Z",
      },
    })));

    const response = await run(new Request(`https://remote.reasonix.io/v1/attachments/${objectId}`, {
      method: "PUT",
      headers: {
        authorization: `Bearer ${ticket}`,
        "content-length": "4",
        "x-reasonix-ciphertext-sha256": hash,
      },
      body: "data",
    }), environment([], { put }));

    expect(response.status).toBe(201);
    expect(put).toHaveBeenCalledOnce();
    expect(put.mock.calls[0]?.[0]).toBe(`encrypted/${objectId}`);
    expect(put.mock.calls[0]?.[2]).toMatchObject({
      sha256: hash,
      httpMetadata: { contentType: "application/octet-stream" },
    });
  });

  it("downloads only authorized opaque bytes with browser sniffing disabled", async () => {
    const objectId = "1".repeat(64);
    const ticket = "2".repeat(64);
    const hash = "3".repeat(64);
    vi.stubGlobal("fetch", vi.fn(async () => Response.json({
      attachment: {
        objectId,
        userId: 9,
        targetDeviceId: "4".repeat(64),
        maxBytes: 6,
        ciphertextBytes: 6,
        ciphertextSha256: hash,
        expiresAt: "2026-09-28T00:00:00.000Z",
      },
    })));
    const get = vi.fn(async () => ({
      body: new Blob(["cipher"]),
      size: 6,
    }) as unknown as R2ObjectBody);

    const response = await run(new Request(`https://remote.reasonix.io/v1/attachments/${objectId}`, {
      headers: { authorization: `Bearer ${ticket}` },
    }), environment([], { get }));

    expect(response.status).toBe(200);
    expect(response.headers.get("content-type")).toBe("application/octet-stream");
    expect(response.headers.get("x-content-type-options")).toBe("nosniff");
    expect(response.headers.get("cache-control")).toContain("no-store");
    await expect(response.text()).resolves.toBe("cipher");
  });
});
