// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { classifyHandshakeFailure, failureView, retryAfterSeconds } from "./cloud_failure";
import { RemoteLinkError } from "./cloud_link";
import { remoteHub } from "./cloud_remote";

const grantBody = {
  grant: { ticket: "t".repeat(64) },
  device: { id: "dev-1", publicKey: "", capabilities: ["desktop"], revokedAt: null },
};

class RefusedSocket extends EventTarget {
  static last: RefusedSocket | null = null;
  readyState = 0;
  constructor() {
    super();
    RefusedSocket.last = this;
    queueMicrotask(() => this.dispatchEvent(new Event("error")));
  }
  close() {}
}

function stubNetwork(routes: {
  grant?: () => Response | Promise<Response>;
  health?: () => Response | Promise<Response>;
  probe?: () => Response | Promise<Response>;
  online?: boolean;
}) {
  const calls: string[] = [];
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input instanceof Request ? input.url : input);
    calls.push(`${init?.method ?? "GET"} ${new URL(url).pathname}`);
    if (url.endsWith("/me/remote-grants")) return routes.grant?.() ?? Response.json(grantBody);
    if (url.endsWith("/health")) return routes.health?.() ?? new Response(null, { status: 200 });
    if (url.endsWith("/v1/probe")) return routes.probe?.() ?? Response.json({ ok: true });
    throw new Error(`unexpected ${url}`);
  }));
  vi.stubGlobal("WebSocket", RefusedSocket);
  Object.defineProperty(navigator, "onLine", { configurable: true, get: () => routes.online ?? true });
  return calls;
}

async function failureOf(): Promise<RemoteLinkError> {
  const error = await remoteHub("dev-1").then(() => null, (e: unknown) => e);
  expect(error).toBeInstanceOf(RemoteLinkError);
  return error as RemoteLinkError;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("handshake failure classification", () => {
  it("says offline without touching the network", async () => {
    const calls = stubNetwork({ online: false });
    const found = await classifyHandshakeFailure({ relay: "wss://remote.example", online: () => false, fetch: fetch as typeof globalThis.fetch });
    expect(found).toEqual({ code: "offline" });
    expect(calls).toEqual([]);
  });

  it("tells the gateway's own answers apart, and only then names an origin", async () => {
    const relay = "wss://remote.example";
    const ask = () => classifyHandshakeFailure({ relay, online: () => true, fetch: fetch as typeof globalThis.fetch });
    const down = () => { throw new TypeError("Failed to fetch"); };
    const refusal = () => new Response(JSON.stringify({ error: { code: "origin_rejected" } }), { status: 403 });

    stubNetwork({ probe: down, health: down });
    expect(await ask()).toEqual({ code: "unreachable" });

    stubNetwork({ probe: refusal });
    expect(await ask()).toEqual({ code: "origin_rejected" });

    const calls = stubNetwork({});
    expect(await ask()).toEqual({ code: "refused" });
    expect(calls).toEqual(["GET /v1/probe"]);
  });

  it("does not blame the origin for outages, throttling or unreadable answers", async () => {
    const relay = "wss://remote.example";
    const ask = () => classifyHandshakeFailure({ relay, online: () => true, fetch: fetch as typeof globalThis.fetch });
    const down = () => { throw new TypeError("Failed to fetch"); };

    stubNetwork({ probe: down });
    expect(await ask()).toEqual({ code: "unknown" });
    stubNetwork({ probe: () => new Response("<html>1101</html>", { status: 500 }) });
    expect(await ask()).toEqual({ code: "unreachable" });
    stubNetwork({ probe: () => new Response(null, { status: 502 }) });
    expect(await ask()).toEqual({ code: "unreachable" });
    stubNetwork({ probe: () => new Response("{}", { status: 403 }) });
    expect(await ask()).toEqual({ code: "unknown" });
    stubNetwork({ probe: () => new Response(null, { status: 429, headers: { "retry-after": "20" } }) });
    expect(await ask()).toEqual({ code: "rate_limited", retryAfterS: 20 });
  });

  it("asks again whether the phone is online once a probe has failed", async () => {
    const down = () => { throw new TypeError("Failed to fetch"); };
    stubNetwork({ probe: down, health: down });
    const answers = [true, false];
    const found = await classifyHandshakeFailure({
      relay: "wss://remote.example", online: () => answers.shift() ?? false, fetch: fetch as typeof globalThis.fetch,
    });
    expect(found).toEqual({ code: "offline" });
  });

  it("reads Retry-After only as a positive number of seconds", () => {
    expect(retryAfterSeconds("30")).toBe(30);
    expect(retryAfterSeconds("0")).toBeUndefined();
    expect(retryAfterSeconds("Wed, 21 Oct 2026 07:28:00 GMT")).toBeUndefined();
    expect(retryAfterSeconds(null)).toBeUndefined();
  });

  it("gives every code its own wording and reconnect action, with the wait only for rate limits", () => {
    expect(failureView({ code: "origin_rejected" }).action).toBe("home");
    expect(failureView({ code: "offline" }).retryWhenOnline).toBe(true);
    expect(failureView({ code: "refused" }).retryWhenOnline).toBe(false);
    const codes = ["offline", "unreachable", "origin_rejected", "rate_limited", "refused", "idle", "unknown"] as const;
    const titles = codes.map((code) => failureView({ code }).title);
    expect(new Set(titles).size).toBe(codes.length);
    expect(failureView({ code: "rate_limited", retryAfterS: 42 })).toMatchObject({ waitS: 42, action: "reconnect" });
    expect(failureView({ code: "rate_limited", retryAfterS: 9999 }).waitS).toBe(600);
    expect(failureView({ code: "refused", retryAfterS: 42 }).waitS).toBe(0);
  });
});

describe("remote connect failures", () => {
  it("names an offline phone", async () => {
    stubNetwork({ online: false, health: () => { throw new TypeError("x"); } });
    const error = await failureOf();
    expect(error.failure).toEqual({ code: "offline" });
    expect(error.kind).toBe("transient");
  });

  it("names a gateway the phone cannot reach after the socket errors", async () => {
    stubNetwork({ probe: () => { throw new TypeError("x"); }, health: () => { throw new TypeError("x"); } });
    expect((await failureOf()).failure).toEqual({ code: "unreachable" });
  });

  it("names an origin the gateway rejects", async () => {
    stubNetwork({ probe: () => new Response(JSON.stringify({ error: { code: "origin_rejected" } }), { status: 403 }) });
    expect((await failureOf()).failure).toEqual({ code: "origin_rejected" });
  });

  it("falls back to a refused ticket when the gateway is reachable and the origin allowed", async () => {
    stubNetwork({});
    expect((await failureOf()).failure).toEqual({ code: "refused" });
  });

  it("reports an account-service rate limit with its Retry-After", async () => {
    stubNetwork({ grant: () => new Response(JSON.stringify({ error: { code: "rate_limited" } }), { status: 429, headers: { "retry-after": "45" } }) });
    const error = await failureOf();
    expect(error.failure).toEqual({ code: "rate_limited", retryAfterS: 45 });
  });

  it("reports the account service being unreachable when the grant request throws", async () => {
    stubNetwork({ grant: () => { throw new TypeError("Failed to fetch"); } });
    expect((await failureOf()).failure).toEqual({ code: "unreachable" });
  });
});
