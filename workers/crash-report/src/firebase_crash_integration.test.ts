import { afterEach, describe, expect, it, vi } from "vitest";
// @ts-expect-error Node 22+ provides node:sqlite; Worker production code does not import it.
import { DatabaseSync } from "node:sqlite";
import worker, { drainFirebaseCrashOutbox } from "./index";
import type { Env } from "./env";
import freshSchemaSQL from "../schema.sql?raw";
import { resetFirebaseAuthForTests } from "./firebase_rtdb";
import {
  FIREBASE_ACTIVE_RESERVATION_BYTES,
  FIREBASE_STORAGE_BUDGET_BYTES,
  acquireFirebaseGroupLease,
  dueFirebaseCrashes,
  purgeFirebaseDeliveryState,
  releaseFirebaseGroupLease,
  reserveFirebaseGroup,
} from "./crash_delivery";

const oauthURL = "https://oauth2.googleapis.com/token";

type SQLiteD1Statement = D1PreparedStatement & { execute(): D1Result };

function sqliteD1(db: DatabaseSync): D1Database {
  return {
    prepare(sql: string) {
      let binds: unknown[] = [];
      const statement = {
        bind(...values: unknown[]) { binds = values; return statement; },
        async first<T>() { return (db.prepare(sql).get(...binds) ?? null) as T | null; },
        async all<T>() { return { success: true, results: db.prepare(sql).all(...binds) as T[], meta: {} }; },
        async run() { return statement.execute(); },
        execute() {
          const result = db.prepare(sql).run(...binds);
          return { success: true, results: [], meta: { changes: Number(result.changes) } } as unknown as D1Result;
        },
        raw() { return Promise.resolve([]); },
      } as unknown as SQLiteD1Statement;
      return statement;
    },
    async batch(statements: D1PreparedStatement[]) {
      db.exec("BEGIN IMMEDIATE");
      try {
        const results = statements.map((statement) => (statement as SQLiteD1Statement).execute());
        db.exec("COMMIT");
        return results;
      } catch (error) {
        db.exec("ROLLBACK");
        throw error;
      }
    },
  } as unknown as D1Database;
}

async function privateKeyPEM(): Promise<string> {
  const pair = await crypto.subtle.generateKey(
    { name: "RSASSA-PKCS1-v1_5", modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]), hash: "SHA-256" },
    true,
    ["sign", "verify"],
  ) as CryptoKeyPair;
  const bytes = new Uint8Array(await crypto.subtle.exportKey("pkcs8", pair.privateKey) as ArrayBuffer);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return `-----BEGIN PRIVATE KEY-----\n${btoa(binary).match(/.{1,64}/g)?.join("\n") ?? ""}\n-----END PRIVATE KEY-----`;
}

async function integrationEnv(db: DatabaseSync): Promise<Env> {
  return {
    DB: sqliteD1(db), RATE_LIMITER: { async limit() { return { success: true }; } },
    CRASH_STORAGE_MODE: "firebase",
    FIREBASE_DATABASE_URL: "https://reasonix-test.asia-southeast1.firebasedatabase.app",
    FIREBASE_CLIENT_EMAIL: "crash-writer@example.iam.gserviceaccount.com",
    FIREBASE_PRIVATE_KEY: await privateKeyPEM(),
  } as unknown as Env;
}

function reportRequest(eventId = "a".repeat(32)): Request {
  const body = JSON.stringify({
    eventId, dedupKey: "b".repeat(64), installId: "c".repeat(32), kind: "crash",
    version: "v1.25.0", os: "linux", arch: "amd64",
    message: "panic at /home/alice/project/main.go:12", source: "go", label: "panic",
    errorType: "runtime.error", topFrame: "main.go:12",
  });
  return new Request("https://crash.reasonix.io/v1/report", {
    method: "POST",
    headers: {
      "content-type": "application/json", "content-length": String(new TextEncoder().encode(body).byteLength),
      "cf-connecting-ip": "127.0.0.1",
    },
    body,
  });
}

function firebaseStub() {
  const values = new Map<string, unknown>();
  const etags = new Map<string, number>();
  const puts: Array<{ path: string; body: string }> = [];
  let unavailable = false;
  let beforeFirstPut: (() => Promise<void>) | undefined;
  const pathOf = (url: string) => new URL(url).pathname.replace(/^\//, "").replace(/\.json$/, "");
  const fetcher = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === oauthURL) return Response.json({ access_token: "token", expires_in: 3600 });
    if (unavailable) return new Response("unavailable", { status: 503 });
    const path = pathOf(url);
    const version = etags.get(path) ?? 1;
    if ((init?.method ?? "GET") === "GET") {
      return Response.json(values.get(path) ?? null, { headers: { etag: `"${version}"` } });
    }
    if (init?.method === "PUT") {
      if (beforeFirstPut) { const wait = beforeFirstPut; beforeFirstPut = undefined; await wait(); }
      const match = new Headers(init.headers).get("If-Match");
      if (match !== `"${version}"`) return new Response(null, { status: 412 });
      const body = String(init.body);
      puts.push({ path, body });
      values.set(path, JSON.parse(body) as unknown);
      etags.set(path, version + 1);
      return new Response(null, { status: 204 });
    }
    if (init?.method === "DELETE") { values.delete(path); return new Response(null, { status: 204 }); }
    return new Response(null, { status: 400 });
  });
  return {
    values, puts, fetcher,
    setUnavailable(value: boolean) { unavailable = value; },
    blockFirstPut(waiter: () => Promise<void>) { beforeFirstPut = waiter; },
  };
}

afterEach(() => { vi.unstubAllGlobals(); resetFirebaseAuthForTests(); });

describe("Firebase-primary crash ingest", () => {
  it("fences stale lease release with an increasing generation", async () => {
    const db = new DatabaseSync(":memory:");
    db.exec(freshSchemaSQL);
    const env = await integrationEnv(db);
    const fingerprint = "9".repeat(64);
    db.prepare(`INSERT INTO firebase_crash_group_state (
      fingerprint, reserved_bytes, last_seen
    ) VALUES (?, ?, ?)`).run(fingerprint, FIREBASE_ACTIVE_RESERVATION_BYTES, "2026-08-25T00:00:00Z");
    try {
      const base = new Date("2026-08-25T00:00:00Z").getTime();
      for (let iteration = 0; iteration < 100; iteration++) {
        const started = new Date(base + iteration * 180_000);
        const first = await acquireFirebaseGroupLease(env, fingerprint, started);
        expect(first?.generation).toBe(iteration * 2 + 1);
        expect(await acquireFirebaseGroupLease(env, fingerprint, new Date(started.getTime() + 30_000))).toBeNull();
        const replacement = await acquireFirebaseGroupLease(env, fingerprint, new Date(started.getTime() + 61_000));
        expect(replacement?.generation).toBe(iteration * 2 + 2);
        await releaseFirebaseGroupLease(env, fingerprint, first!);
        expect(db.prepare("SELECT lease_owner FROM firebase_crash_group_state").get())
          .toEqual({ lease_owner: replacement!.owner });
        await releaseFirebaseGroupLease(env, fingerprint, replacement!);
      }
      expect(db.prepare("SELECT lease_owner, lease_generation FROM firebase_crash_group_state").get())
        .toEqual({ lease_owner: "", lease_generation: 200 });
    } finally { db.close(); }
  });

  it("reclaims old delivery rows and expired generation leases", async () => {
    const db = new DatabaseSync(":memory:");
    db.exec(freshSchemaSQL);
    const env = await integrationEnv(db);
    try {
      db.prepare(`INSERT INTO firebase_crash_outbox (
        event_id, fingerprint, payload, state, attempts, next_attempt_at, created_at, updated_at
      ) VALUES (?, ?, '{}', 'processing', 0, ?, ?, ?)`).run(
        "8".repeat(32), "7".repeat(64), "2000-01-01T00:00:00Z", "2000-01-01T00:00:00Z", "2000-01-01T00:00:00Z",
      );
      db.prepare(`INSERT INTO firebase_crash_receipts (
        event_id, projected_at, group_count, latest_slot, first_sample
      ) VALUES (?, ?, 1, 0, 1)`).run("6".repeat(32), "2000-01-01T00:00:00Z");
      db.prepare(`INSERT INTO firebase_crash_group_state (
        fingerprint, reserved_bytes, last_seen, lease_owner, lease_generation, lease_expires_at
      ) VALUES (?, ?, ?, 'expired', 4, ?)`).run(
        "5".repeat(64), FIREBASE_ACTIVE_RESERVATION_BYTES, "2000-01-01T00:00:00Z", "2000-01-01T00:00:00Z",
      );
      expect(await dueFirebaseCrashes(env)).toHaveLength(1);
      await purgeFirebaseDeliveryState(env);
      expect(db.prepare("SELECT COUNT(*) AS count FROM firebase_crash_outbox").get()).toEqual({ count: 0 });
      expect(db.prepare("SELECT COUNT(*) AS count FROM firebase_crash_receipts").get()).toEqual({ count: 0 });
      expect(db.prepare("SELECT lease_owner, lease_generation FROM firebase_crash_group_state").get())
        .toEqual({ lease_owner: "", lease_generation: 4 });
    } finally { db.close(); }
  });

  it("stores no long-lived D1 sample and deduplicates a repeated eventId", async () => {
    const db = new DatabaseSync(":memory:");
    db.exec(freshSchemaSQL);
    const env = await integrationEnv(db);
    const firebase = firebaseStub();
    vi.stubGlobal("fetch", firebase.fetcher);
    try {
      expect((await worker.fetch(reportRequest(), env)).status).toBe(202);
      expect((await worker.fetch(reportRequest(), env)).status).toBe(202);
      expect(db.prepare("SELECT count FROM groups").get()).toEqual({ count: 1 });
      expect(db.prepare("SELECT COUNT(*) AS count FROM reports").get()).toEqual({ count: 0 });
      expect(db.prepare("SELECT events FROM report_daily").get()).toEqual({ events: 1 });
      expect(db.prepare("SELECT COUNT(*) AS count FROM firebase_crash_outbox").get()).toEqual({ count: 0 });
      expect(db.prepare("SELECT group_count, latest_slot, first_sample FROM firebase_crash_receipts").get())
        .toEqual({ group_count: 1, latest_slot: 0, first_sample: 1 });
      expect(firebase.puts).toHaveLength(3);
      const bodies = firebase.puts.map((write) => write.body).join("\n");
      expect(bodies).toContain("/home/_/project/main.go:12");
      expect(bodies).not.toContain("installId");
      expect(bodies).not.toContain("alice");
    } finally { db.close(); }
  });

  it("buffers Firebase failure and retries without double-counting D1", async () => {
    const db = new DatabaseSync(":memory:");
    db.exec(freshSchemaSQL);
    const env = await integrationEnv(db);
    const firebase = firebaseStub();
    firebase.setUnavailable(true);
    vi.stubGlobal("fetch", firebase.fetcher);
    try {
      expect((await worker.fetch(reportRequest("d".repeat(32)), env)).status).toBe(202);
      expect(db.prepare("SELECT state FROM firebase_crash_outbox").get()).toEqual({ state: "projected" });
      firebase.setUnavailable(false);
      db.prepare("UPDATE firebase_crash_outbox SET next_attempt_at = '2000-01-01T00:00:00Z'").run();
      await drainFirebaseCrashOutbox(env);
      expect(db.prepare("SELECT COUNT(*) AS count FROM firebase_crash_outbox").get()).toEqual({ count: 0 });
      expect(db.prepare("SELECT count FROM groups").get()).toEqual({ count: 1 });
    } finally { db.close(); }
  });

  it("queues a same-group report while the current generation is writing", async () => {
    const db = new DatabaseSync(":memory:");
    db.exec(freshSchemaSQL);
    const env = await integrationEnv(db);
    const firebase = firebaseStub();
    let release!: () => void;
    const released = new Promise<void>((resolve) => { release = resolve; });
    let started!: () => void;
    const firstPut = new Promise<void>((resolve) => { started = resolve; });
    firebase.blockFirstPut(async () => { started(); await released; });
    vi.stubGlobal("fetch", firebase.fetcher);
    try {
      const first = worker.fetch(reportRequest("1".repeat(32)), env);
      await firstPut;
      expect((await worker.fetch(reportRequest("2".repeat(32)), env)).status).toBe(202);
      expect(db.prepare("SELECT state FROM firebase_crash_outbox WHERE event_id = ?").get("2".repeat(32)))
        .toEqual({ state: "queued" });
      release();
      expect((await first).status).toBe(202);
      await drainFirebaseCrashOutbox(env);
      expect(db.prepare("SELECT count FROM groups").get()).toEqual({ count: 2 });
      expect(db.prepare("SELECT COUNT(*) AS count FROM firebase_crash_outbox").get()).toEqual({ count: 0 });
    } finally { release(); db.close(); }
  });

  it("returns 503 at the outbox cap and reclaims only the unused new reservation", async () => {
    const db = new DatabaseSync(":memory:");
    db.exec(freshSchemaSQL);
    db.exec(`WITH RECURSIVE n(value) AS (
      SELECT 1 UNION ALL SELECT value + 1 FROM n WHERE value < 5000
    ) INSERT INTO firebase_crash_outbox (
      event_id, fingerprint, payload, state, attempts, next_attempt_at, created_at, updated_at
    ) SELECT printf('%032x', value), '${"e".repeat(64)}', '{}', 'queued', 0,
      '2026-08-25T00:00:00Z', '2026-08-25T00:00:00Z', '2026-08-25T00:00:00Z' FROM n`);
    const env = await integrationEnv(db);
    try {
      expect((await worker.fetch(reportRequest("f".repeat(32)), env)).status).toBe(503);
      expect(db.prepare("SELECT COUNT(*) AS count FROM firebase_crash_group_state").get()).toEqual({ count: 0 });
    } finally { db.close(); }
  });

  it("enforces the 700 MiB reservation in the atomic INSERT/UPDATE statement", async () => {
    const db = new DatabaseSync(":memory:");
    db.exec(freshSchemaSQL);
    const env = await integrationEnv(db);
    const padding = FIREBASE_STORAGE_BUDGET_BYTES - FIREBASE_ACTIVE_RESERVATION_BYTES + 1;
    db.prepare(`INSERT INTO firebase_crash_group_state (
      fingerprint, reserved_bytes, last_seen
    ) VALUES (?, ?, ?)`).run("1".repeat(64), padding, "2026-08-25T00:00:00Z");
    try {
      expect(await reserveFirebaseGroup(env, "2".repeat(64), "2026-08-25T00:00:00Z")).toBe("full");
      expect(db.prepare("SELECT SUM(reserved_bytes) AS total FROM firebase_crash_group_state").get())
        .toEqual({ total: padding });
      db.prepare(`INSERT INTO groups (
        fingerprint, kind, count, first_seen, last_seen, last_version
      ) VALUES (?, 'crash', 1, ?, ?, 'v1')`).run(
        "3".repeat(64), "2026-08-25T00:00:00Z", "2026-08-25T00:00:00Z",
      );
      expect(await reserveFirebaseGroup(env, "3".repeat(64), "2026-08-25T00:00:00Z")).toBe("full");
    } finally { db.close(); }
  });
});
