import { describe, expect, it, vi } from "vitest";
import worker from "./index";
import type { Env, RateLimiter } from "./env";

function limiter(success = true): RateLimiter {
  return { limit: vi.fn().mockResolvedValue({ success }) };
}

function env(overrides: Partial<Env> = {}): Env {
  return {
    PING_LIMITER: limiter(),
    METRICS_LIMITER: limiter(),
    TELEMETRY_BUDGET_LIMITER: limiter(),
    TELEMETRY_QUEUE_ENABLED: "true",
    TELEMETRY_QUEUE: { send: vi.fn().mockResolvedValue(undefined) } as unknown as Env["TELEMETRY_QUEUE"],
    ...overrides,
  } as Env;
}

function post(path: string, body: unknown): Request {
  const json = JSON.stringify(body);
  return new Request(`https://crash.reasonix.io${path}`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      "content-length": String(new TextEncoder().encode(json).byteLength),
      "cf-connecting-ip": "203.0.113.10",
    },
    body: json,
  });
}

describe("telemetry admission result", () => {
  it("marks a durably enqueued ping", async () => {
    const bindings = env();
    const response = await worker.fetch(post("/v1/ping", {
      installId: "a".repeat(32),
      version: "capacity-test",
      os: "linux",
      arch: "x64",
      surface: "studio",
    }), bindings);

    expect(response.status).toBe(202);
    expect(response.headers.get("x-reasonix-telemetry-result")).toBe("queued");
    expect(bindings.TELEMETRY_QUEUE?.send).toHaveBeenCalledOnce();
  });

  it("accepts the Chromium renderer the 1.x desktop reports", async () => {
    for (const path of ["/v1/ping", "/v1/metrics"]) {
      const bindings = env();
      const response = await worker.fetch(post(path, {
        installId: "c".repeat(32),
        version: "v1.39.4",
        os: "windows",
        arch: "amd64",
        surface: "desktop",
        runtimeEngine: "chromium",
        counters: [{ signal: "turns", bucket: "1", count: 1 }],
      }), bindings);

      expect(response.status, path).toBe(202);
      expect(bindings.TELEMETRY_QUEUE?.send, path).toHaveBeenCalledOnce();
    }
  });

  it("makes global budget sampling observable without enqueueing", async () => {
    const bindings = env({ TELEMETRY_BUDGET_LIMITER: limiter(false) });
    const response = await worker.fetch(post("/v1/ping", {
      installId: "b".repeat(32),
      version: "capacity-test",
      os: "linux",
      arch: "x64",
      surface: "studio",
    }), bindings);

    expect(response.status).toBe(202);
    expect(response.headers.get("x-reasonix-telemetry-result")).toBe("sampled");
    expect(bindings.TELEMETRY_QUEUE?.send).not.toHaveBeenCalled();
  });

  it("marks an empty compatible metrics batch as ignored", async () => {
    const bindings = env();
    const response = await worker.fetch(post("/v1/metrics", {
      version: "capacity-test",
      os: "linux",
      surface: "studio",
      counters: [{ signal: "future_signal", bucket: "future", count: 1 }],
    }), bindings);

    expect(response.status).toBe(202);
    expect(response.headers.get("x-reasonix-telemetry-result")).toBe("ignored");
    expect(bindings.TELEMETRY_QUEUE?.send).not.toHaveBeenCalled();
  });
});

describe("telemetry queue database batching", () => {
  it("persists an entire queue delivery in one D1 data batch", async () => {
    const dataBatches: unknown[][] = [];
    const db = {
      prepare(sql: string) {
        return {
          sql,
          bind(...bindings: unknown[]) {
            return { sql, bindings };
          },
        };
      },
      async batch(statements: unknown[]) {
        dataBatches.push(statements);
        return [];
      },
    } as unknown as D1Database;
    const queued = [
      {
        version: 1 as const,
        eventId: crypto.randomUUID(),
        receivedAt: "2026-09-27T06:00:00.000Z",
        kind: "ping" as const,
        payload: {
          installId: "c".repeat(32), version: "capacity-test", os: "linux", arch: "x64", surface: "desktop",
        },
      },
      {
        version: 1 as const,
        eventId: crypto.randomUUID(),
        receivedAt: "2026-09-27T06:00:00.000Z",
        kind: "metrics" as const,
        payload: {
          version: "capacity-test",
          os: "linux",
          surface: "desktop",
          counters: [
            { signal: "client_surface", bucket: "desktop", count: 1 },
            { signal: "client_version", bucket: "capacity_test", count: 1 },
          ],
        },
      },
    ];
    const messages = queued.map((body) => ({ body, ack: vi.fn(), retry: vi.fn() }));
    const bindings = {
      DB: db,
      TELEMETRY_RAW: { put: vi.fn().mockResolvedValue(undefined) },
    } as unknown as Env;

    await worker.queue({ messages } as unknown as MessageBatch<(typeof queued)[number]>, bindings);

    expect(dataBatches).toHaveLength(3);
    expect(dataBatches[0]).toHaveLength(2); // idempotency schema guard
    expect(dataBatches[1]).toHaveLength(2); // desktop schema guard
    expect(dataBatches[2]).toHaveLength(4); // two statements per envelope
    expect(JSON.stringify(dataBatches[2])).toContain("json_each");
    for (const message of messages) {
      expect(message.ack).toHaveBeenCalledOnce();
      expect(message.retry).not.toHaveBeenCalled();
    }
  });

  it("dual-writes Studio deliveries before the database cutover", async () => {
    const makeDB = () => {
      const batches: unknown[][] = [];
      const db = {
        prepare(sql: string) {
          return { sql, bind: (...bindings: unknown[]) => ({ sql, bindings }) };
        },
        async batch(statements: unknown[]) {
          batches.push(statements);
          return [];
        },
      } as unknown as D1Database;
      return { db, batches };
    };
    const legacy = makeDB();
    const isolated = makeDB();
    const message = {
      body: {
        version: 1 as const,
        eventId: crypto.randomUUID(),
        receivedAt: "2026-09-27T06:00:00.000Z",
        kind: "ping" as const,
        payload: {
          installId: "d".repeat(32), version: "2.20.2", os: "linux", arch: "x64", surface: "studio",
        },
      },
      ack: vi.fn(),
      retry: vi.fn(),
    };
    const bindings = {
      DB: legacy.db,
      TELEMETRY_DB: isolated.db,
      TELEMETRY_DB_MODE: "dual",
      TELEMETRY_RAW: { put: vi.fn().mockResolvedValue(undefined) },
    } as unknown as Env;

    await worker.queue({ messages: [message] } as unknown as MessageBatch<typeof message.body>, bindings);

    expect(legacy.batches.at(-1)).toHaveLength(2);
    expect(isolated.batches.at(-1)).toHaveLength(2);
    expect(JSON.stringify(legacy.batches.at(-1))).toContain("studio_pings");
    expect(JSON.stringify(isolated.batches.at(-1))).toContain("studio_pings");
    expect(message.ack).toHaveBeenCalledOnce();
  });

  it("dual-writes Desktop deliveries without changing the product cutover", async () => {
    const makeDB = () => {
      const batches: unknown[][] = [];
      const db = {
        prepare: (sql: string) => ({ sql, bind: (...bindings: unknown[]) => ({ sql, bindings }) }),
        batch: async (statements: unknown[]) => { batches.push(statements); return []; },
      } as unknown as D1Database;
      return { db, batches };
    };
    const legacy = makeDB();
    const isolated = makeDB();
    const message = {
      body: {
        version: 1 as const,
        eventId: crypto.randomUUID(),
        receivedAt: "2026-09-27T07:00:00.000Z",
        kind: "ping" as const,
        payload: {
          installId: "e".repeat(32), version: "v1.23.0", os: "linux", arch: "x64", surface: "desktop",
        },
      },
      ack: vi.fn(),
      retry: vi.fn(),
    };
    const bindings = {
      DB: legacy.db,
      TELEMETRY_DB: isolated.db,
      TELEMETRY_DB_MODE: "isolated",
      DESKTOP_TELEMETRY_DB_MODE: "dual",
      TELEMETRY_RAW: { put: vi.fn().mockResolvedValue(undefined) },
    } as unknown as Env;

    await worker.queue({ messages: [message] } as unknown as MessageBatch<typeof message.body>, bindings);

    expect(JSON.stringify(legacy.batches.at(-1))).toContain("INSERT INTO pings");
    expect(JSON.stringify(isolated.batches.at(-1))).toContain("INSERT INTO pings");
    expect(message.ack).toHaveBeenCalledOnce();
  });
});
