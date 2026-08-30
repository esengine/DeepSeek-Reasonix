import { afterEach, describe, expect, it, vi } from "vitest";
// @ts-expect-error Node 22+ provides node:sqlite; Worker production code does not import it.
import { DatabaseSync } from "node:sqlite";
import freshSchemaSQL from "../schema.sql?raw";
import type { Env } from "./env";
import {
  FIREBASE_ACTIVE_RESERVATION_BYTES,
  FIREBASE_ARCHIVING_RESERVATION_BYTES,
  FIREBASE_COMPACTED_RESERVATION_BYTES,
  reserveFirebaseGroup,
} from "./crash_delivery";
import { runFirebaseCrashLifecycle } from "./firebase_lifecycle";
import { resetFirebaseAuthForTests } from "./firebase_rtdb";

type SQLiteStatement = D1PreparedStatement & { execute(): D1Result };

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
      } as unknown as SQLiteStatement;
      return statement;
    },
    async batch(statements: D1PreparedStatement[]) {
      db.exec("BEGIN IMMEDIATE");
      try {
        const results = statements.map((statement) => (statement as SQLiteStatement).execute());
        db.exec("COMMIT");
        return results;
      } catch (error) { db.exec("ROLLBACK"); throw error; }
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

async function envFor(db: DatabaseSync): Promise<Env> {
  return {
    DB: sqliteD1(db), CRASH_STORAGE_MODE: "firebase",
    FIREBASE_DATABASE_URL: "https://reasonix-test.asia-southeast1.firebasedatabase.app",
    FIREBASE_CLIENT_EMAIL: "writer@example.iam.gserviceaccount.com",
    FIREBASE_PRIVATE_KEY: await privateKeyPEM(),
  } as Env;
}

function firebaseStore(initial: Record<string, unknown>) {
  const values = new Map(Object.entries(initial));
  const versions = new Map<string, number>();
  const pathOf = (url: string) => new URL(url).pathname.replace(/^\//, "").replace(/\.json$/, "");
  const fetcher = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "https://oauth2.googleapis.com/token") {
      return Response.json({ access_token: "token", expires_in: 3600 });
    }
    const path = pathOf(url);
    const method = init?.method ?? "GET";
    const version = versions.get(path) ?? 1;
    if (method === "GET") return Response.json(values.get(path) ?? null, { headers: { etag: `"${version}"` } });
    if (new Headers(init?.headers).get("If-Match") !== `"${version}"`) return new Response(null, { status: 412 });
    if (method === "DELETE") {
      for (const key of [...values.keys()]) if (key === path || key.startsWith(`${path}/`)) values.delete(key);
    } else {
      values.set(path, JSON.parse(String(init?.body)) as unknown);
    }
    versions.set(path, version + 1);
    return new Response(null, { status: 204 });
  });
  return { values, fetcher };
}

function insertGroup(db: DatabaseSync, fingerprint: string, status: string, lastSeen: string) {
  db.prepare(`INSERT INTO groups (
    fingerprint, kind, count, first_seen, last_seen, first_version, last_version,
    status, title, source, severity
  ) VALUES (?, 'crash', 8, '2026-01-01T00:00:00Z', ?, 'v1', 'v2', ?, 'boom', 'go', 'high')`)
    .run(fingerprint, lastSeen, status);
  db.prepare(`INSERT INTO firebase_crash_group_state (
    fingerprint, reserved_bytes, last_seen
  ) VALUES (?, ?, ?)`).run(fingerprint, FIREBASE_ACTIVE_RESERVATION_BYTES, lastSeen);
}

afterEach(() => { vi.unstubAllGlobals(); resetFirebaseAuthForTests(); });

describe("Firebase sample lifecycle", () => {
  it("compacts at 30 days, tombstones at 60 days, and deletes after 24 hours", async () => {
    const db = new DatabaseSync(":memory:");
    db.exec(freshSchemaSQL);
    const fingerprint = "a".repeat(64);
    insertGroup(db, fingerprint, "resolved", "2026-07-10T00:00:00Z");
    const root = `groups/${fingerprint}`;
    const old = { eventId: "1".repeat(32), receivedAt: "2026-01-01T00:00:00Z", groupCount: 1, writerGeneration: 0, sampleEpoch: 1 };
    const firebase = firebaseStore({
      [`${root}/meta`]: { count: 8, writerGeneration: 0, sampleEpoch: 1 },
      [`${root}/samples/first`]: old,
      ...Object.fromEntries(Array.from({ length: 5 }, (_, slot) => [`${root}/samples/latest/${slot}`, old])),
      [root]: { present: true },
    });
    vi.stubGlobal("fetch", firebase.fetcher);
    const env = await envFor(db);
    try {
      await runFirebaseCrashLifecycle(env, new Date("2026-08-25T00:00:00Z"));
      expect(db.prepare("SELECT sample_state, reserved_bytes FROM firebase_crash_group_state").get())
        .toEqual({ sample_state: "compacted", reserved_bytes: FIREBASE_COMPACTED_RESERVATION_BYTES });
      expect(firebase.values.get(`${root}/samples/latest/0`)).toMatchObject({ marker: "compacted" });
      expect(firebase.values.get(`${root}/samples/first`)).toEqual(old);

      db.prepare("UPDATE groups SET last_seen = '2026-06-01T00:00:00Z' WHERE fingerprint = ?").run(fingerprint);
      await runFirebaseCrashLifecycle(env, new Date("2026-08-25T01:00:00Z"));
      expect(db.prepare("SELECT sample_state, reserved_bytes FROM firebase_crash_group_state").get())
        .toEqual({ sample_state: "archiving", reserved_bytes: FIREBASE_ARCHIVING_RESERVATION_BYTES });
      expect(firebase.values.get(`${root}/samples/first`)).toMatchObject({ marker: "archiving" });

      await runFirebaseCrashLifecycle(env, new Date("2026-08-26T02:00:00Z"));
      expect(db.prepare("SELECT sample_state, reserved_bytes FROM firebase_crash_group_state").get())
        .toEqual({ sample_state: "archived", reserved_bytes: 0 });
      expect([...firebase.values.keys()].some((key) => key === root || key.startsWith(`${root}/`))).toBe(false);
    } finally { db.close(); }
  });

  it("never cleans open groups or groups with pending outbox, and reactivates an archived epoch", async () => {
    const db = new DatabaseSync(":memory:");
    db.exec(freshSchemaSQL);
    const open = "b".repeat(64);
    const pending = "c".repeat(64);
    insertGroup(db, open, "open", "2020-01-01T00:00:00Z");
    insertGroup(db, pending, "ignored", "2020-01-01T00:00:00Z");
    db.prepare(`INSERT INTO firebase_crash_outbox (
      event_id, fingerprint, payload, state, attempts, next_attempt_at, created_at, updated_at
    ) VALUES (?, ?, '{}', 'queued', 0, ?, ?, ?)`).run(
      "9".repeat(32), pending, "2026-08-25T00:00:00Z", "2026-08-25T00:00:00Z", "2026-08-25T00:00:00Z",
    );
    const env = await envFor(db);
    vi.stubGlobal("fetch", firebaseStore({}).fetcher);
    try {
      await runFirebaseCrashLifecycle(env, new Date("2026-08-25T00:00:00Z"));
      expect(db.prepare("SELECT COUNT(*) AS count FROM firebase_crash_group_state WHERE sample_state = 'active'").get())
        .toEqual({ count: 2 });
      db.prepare(`UPDATE firebase_crash_group_state SET
        sample_state = 'archived', reserved_bytes = 0, sample_epoch = 3
        WHERE fingerprint = ?`).run(open);
      expect(await reserveFirebaseGroup(env, open, "2026-08-25T01:00:00Z")).toBe("reserved");
      expect(db.prepare("SELECT sample_state, sample_epoch, reserved_bytes FROM firebase_crash_group_state WHERE fingerprint = ?").get(open))
        .toEqual({ sample_state: "active", sample_epoch: 4, reserved_bytes: FIREBASE_ACTIVE_RESERVATION_BYTES });
    } finally { db.close(); }
  });
});
