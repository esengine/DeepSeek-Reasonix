import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Env } from "./env";
import {
  FirebaseFenceError,
  deleteFirebaseCrashGroup,
  readFirebaseCrashGroup,
  resetFirebaseAuthForTests,
  writeFirebaseCrashGroup,
  writeFirebaseGroupMeta,
  type FirebaseCrashGroupMeta,
} from "./firebase_rtdb";

const oauthURL = "https://oauth2.googleapis.com/token";
const databaseHost = "reasonix-test.asia-southeast1.firebasedatabase.app";

async function privateKeyPEM(): Promise<string> {
  const pair = await crypto.subtle.generateKey(
    { name: "RSASSA-PKCS1-v1_5", modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]), hash: "SHA-256" },
    true,
    ["sign", "verify"],
  ) as CryptoKeyPair;
  const bytes = new Uint8Array(await crypto.subtle.exportKey("pkcs8", pair.privateKey) as ArrayBuffer);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  const encoded = btoa(binary).match(/.{1,64}/g)?.join("\n") ?? "";
  return `-----BEGIN PRIVATE KEY-----\n${encoded}\n-----END PRIVATE KEY-----`;
}

const meta: FirebaseCrashGroupMeta = {
  fingerprint: "a".repeat(64), kind: "crash", count: 1,
  firstSeen: "2026-08-25T00:00:00.000Z", lastSeen: "2026-08-25T00:00:00.000Z",
  firstVersion: "v1.0.0", lastVersion: "v1.0.0", status: "open", title: "boom",
  source: "go", label: "panic", errorType: "error", topFrame: "main.go:<n>", severity: "high",
  lastOS: "linux", lastArch: "amd64", lastBuildCommit: "", lastChannel: "stable", regressedAt: "",
};

async function firebaseEnv(): Promise<Env> {
  return {
    FIREBASE_DATABASE_URL: `https://${databaseHost}`,
    FIREBASE_CLIENT_EMAIL: "crash-writer@example.iam.gserviceaccount.com",
    FIREBASE_PRIVATE_KEY: await privateKeyPEM(),
  } as Env;
}

type Call = { url: string; init?: RequestInit };

function storeFetcher(initial: Record<string, unknown> = {}) {
  const values = new Map(Object.entries(initial));
  const versions = new Map<string, number>();
  const calls: Call[] = [];
  const pathOf = (url: string) => new URL(url).pathname.replace(/^\//, "").replace(/\.json$/, "");
  const fetcher: typeof fetch = async (input, init) => {
    const url = String(input);
    calls.push({ url, init });
    if (url === oauthURL) return Response.json({ access_token: "token", expires_in: 3600 });
    const path = pathOf(url);
    const method = init?.method ?? "GET";
    const version = versions.get(path) ?? 1;
    if (method === "GET") {
      return Response.json(values.get(path) ?? null, { headers: { etag: `"${version}"` } });
    }
    const headers = new Headers(init?.headers);
    if (headers.has("If-Match") && headers.get("If-Match") !== `"${version}"`) {
      return Response.json(values.get(path) ?? null, { status: 412, headers: { etag: `"${version}"` } });
    }
    if (method === "DELETE") values.delete(path);
    else values.set(path, JSON.parse(String(init?.body)) as unknown);
    versions.set(path, version + 1);
    return new Response(null, { status: 204 });
  };
  return { fetcher, calls, values, versions };
}

describe("Firebase Realtime Database conditional delivery", () => {
  beforeEach(() => resetFirebaseAuthForTests());
  afterEach(() => { vi.useRealTimers(); resetFirebaseAuthForTests(); });

  it("coalesces OAuth and writes only meta, first, and one ring slot with ETags", async () => {
    const env = await firebaseEnv();
    const store = storeFetcher();
    const sample = { eventId: "b".repeat(32), receivedAt: meta.firstSeen, message: "sanitized" };
    await Promise.all([
      writeFirebaseCrashGroup(env, meta, sample, 0, true, 1, 1, undefined, store.fetcher),
      writeFirebaseCrashGroup(env, { ...meta, count: 2 }, sample, 1, false, 2, 1, undefined, store.fetcher),
    ]);
    expect(store.calls.filter((call) => call.url === oauthURL)).toHaveLength(1);
    const databaseCalls = store.calls.filter((call) => new URL(call.url).hostname === databaseHost);
    expect(databaseCalls.every((call) => !call.url.includes(`/groups/${meta.fingerprint}.json`))).toBe(true);
    const puts = databaseCalls.filter((call) => call.init?.method === "PUT");
    expect(puts.length).toBeGreaterThanOrEqual(4);
    expect(puts.every((call) => new Headers(call.init?.headers).has("If-Match"))).toBe(true);
    expect(puts.every((call) => call.url.endsWith("?print=silent"))).toBe(true);
    expect(JSON.stringify([...store.values.values()])).not.toContain("installId");
  });

  it("retries a 412 at most through a fresh ETag and fences a stale generation", async () => {
    const env = await firebaseEnv();
    const store = storeFetcher();
    let injected = false;
    const fetcher: typeof fetch = async (input, init) => {
      if (String(input) !== oauthURL && init?.method === "PUT" && !injected) {
        injected = true;
        return Response.json({ writerGeneration: 0 }, { status: 412, headers: { etag: '"2"' } });
      }
      return store.fetcher(input, init);
    };
    await writeFirebaseCrashGroup(
      env, meta, { eventId: "c".repeat(32), receivedAt: meta.firstSeen }, 0, true, 2, 1, undefined, fetcher,
    );
    expect(injected).toBe(true);

    resetFirebaseAuthForTests();
    const metaPath = `groups/${meta.fingerprint}/meta`;
    const fenced = storeFetcher({ [metaPath]: { ...meta, writerGeneration: 11, sampleEpoch: 1 } });
    await expect(writeFirebaseCrashGroup(
      env, meta, { eventId: "d".repeat(32), receivedAt: meta.firstSeen }, 0, false, 10, 1, undefined,
      fenced.fetcher,
    )).rejects.toBeInstanceOf(FirebaseFenceError);
    expect(fenced.calls.filter((call) => call.init?.method === "PUT")).toHaveLength(0);
  });

  it("refreshes once after 401 and never includes credential bodies in errors", async () => {
    const env = await firebaseEnv();
    let tokenRequests = 0;
    const store = storeFetcher();
    let rejected = false;
    const fetcher: typeof fetch = async (input, init) => {
      if (String(input) === oauthURL) {
        tokenRequests++;
        return Response.json({ access_token: `token-${tokenRequests}`, expires_in: 3600 });
      }
      if (!rejected) { rejected = true; return new Response("expired", { status: 401 }); }
      return store.fetcher(input, init);
    };
    await writeFirebaseCrashGroup(
      env, meta, { eventId: "e".repeat(32), receivedAt: meta.firstSeen }, 0, true, 1, 1, undefined, fetcher,
    );
    expect(tokenRequests).toBe(2);

    resetFirebaseAuthForTests();
    const secret = "private-key-material-must-not-leak";
    const failing: typeof fetch = async () => new Response(secret, { status: 403 });
    await expect(writeFirebaseCrashGroup(
      env, meta, { eventId: "f".repeat(32), receivedAt: meta.firstSeen }, 0, true, 1, 1, undefined, failing,
    )).rejects.not.toThrow(secret);
  });

  it("bounds OAuth and rejects non-Firebase hosts before sample transmission", async () => {
    vi.useFakeTimers();
    const env = await firebaseEnv();
    let started!: () => void;
    const requestStarted = new Promise<void>((resolve) => { started = resolve; });
    const hanging: typeof fetch = async (_input, init) => {
      started();
      return new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
      });
    };
    const pending = writeFirebaseCrashGroup(
      env, meta, { eventId: "1".repeat(32), receivedAt: meta.firstSeen }, 0, true, 1, 1, undefined, hanging,
    );
    const rejection = expect(pending).rejects.toThrow("aborted");
    await requestStarted;
    await vi.advanceTimersByTimeAsync(5_001);
    await rejection;
    vi.useRealTimers();

    resetFirebaseAuthForTests();
    const invalid = { ...await firebaseEnv(), FIREBASE_DATABASE_URL: "https://example.com" } as Env;
    let databaseCalls = 0;
    const fetcher: typeof fetch = async (input) => {
      if (String(input) === oauthURL) return Response.json({ access_token: "token", expires_in: 3600 });
      databaseCalls++;
      return Response.json(null);
    };
    await expect(writeFirebaseCrashGroup(
      invalid, meta, { eventId: "2".repeat(32), receivedAt: meta.firstSeen }, 0, true, 1, 1, undefined, fetcher,
    )).rejects.toThrow("approved Realtime Database host");
    expect(databaseCalls).toBe(0);
  });

  it("reads groups and conditionally replaces metadata while legacy delete remains available", async () => {
    const env = await firebaseEnv();
    const root = `groups/${meta.fingerprint}`;
    const sample = { eventId: "3".repeat(32), receivedAt: meta.firstSeen, groupCount: 1, writerGeneration: 1, sampleEpoch: 1 };
    const store = storeFetcher({ [root]: { meta, samples: { first: sample, latest: { 0: sample } } } });
    expect((await readFirebaseCrashGroup(env, meta.fingerprint, store.fetcher))?.samples?.first).toEqual(sample);
    await writeFirebaseGroupMeta(env, meta.fingerprint, { ...meta, severity: "critical" }, 2, 1, "active", undefined, store.fetcher);
    await deleteFirebaseCrashGroup(env, meta.fingerprint, store.fetcher);
    expect(store.calls.map((call) => call.init?.method ?? "GET")).toEqual([
      "POST", "GET", "GET", "PUT", "DELETE",
    ]);
  });
});
