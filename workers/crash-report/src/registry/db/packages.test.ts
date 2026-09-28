import { describe, expect, it } from "vitest";
// @ts-expect-error Node types are intentionally not part of the Worker build.
import { DatabaseSync } from "node:sqlite";
import type { PackageRow, RegistryUser } from "../types";
import { PublishSchema } from "../lib/validation";
import { PackageRepo } from "./packages";
import { repinReviewedDigest } from "../pin";
import registrySchema from "../../../registry-schema.sql?raw";

const now = "2026-07-22T00:00:00.000Z";
const user: RegistryUser = {
  id: 7,
  handle: "publisher",
  role: "member",
  emailVerified: true,
};

const existing: PackageRow = {
  id: 42,
  kind: "mcp",
  scope_handle: "publisher",
  name: "devkit",
  slug: "publisher/devkit",
  summary: "old",
  description: "",
  source: "https://github.com/o/r",
  install_kind: "auto",
  homepage: "",
  repo_url: "https://github.com/o/r",
  tags: "tool",
  latest_version: "2.7.0",
  status: "pending",
  verified: 0,
  publisher_id: 7,
  install_count: 0,
  star_count: 0,
  up_count: 0,
  down_count: 0,
  rec_score: 0,
  created_at: now,
  updated_at: now,
};

function fakePackageDB(reads: PackageRow[]) {
  const updates: { sql: string; values: unknown[] }[] = [];
  let packageReads = 0;
  const db = {
    prepare(sql: string) {
      let values: unknown[] = [];
      const statement = {
        bind(...bound: unknown[]) {
          values = bound;
          return statement;
        },
        async first<T>() {
          if (sql.startsWith("SELECT * FROM packages")) {
            const row = reads[Math.min(packageReads, reads.length - 1)];
            packageReads += 1;
            return row as T;
          }
          return null;
        },
        async run() {
          if (sql.startsWith("UPDATE packages SET")) updates.push({ sql, values });
          return { meta: { changes: 1 } };
        },
      };
      return statement;
    },
  };
  return { db: db as unknown as D1Database, updates };
}

function pluginInput() {
  return PublishSchema.parse({
    kind: "plugin",
    installKind: "plugin",
    name: "devkit",
    source: "https://github.com/o/r",
    repoUrl: "https://github.com/o/r",
    version: "2.7.1",
  });
}

describe("PackageRepo.publish", () => {
  it("persists a kind change when an owned pending package is republished as a plugin", async () => {
    const updated: PackageRow = { ...existing, kind: "plugin", install_kind: "plugin", latest_version: "2.7.1" };
    const { db, updates } = fakePackageDB([existing, updated]);
    const result = await new PackageRepo(db).publish(user, pluginInput(), now);

    expect(result.created).toBe(false);
    expect(result.row.kind).toBe("plugin");
    expect(updates).toHaveLength(1);
    expect(updates[0].sql).toContain("SET kind = ?1");
    expect(updates[0].values[0]).toBe("plugin");
    expect(updates[0].values[4]).toBe("plugin");
    expect(updates[0].values[10]).toBe("pending");
    expect(updates[0].values[11]).toBe(0);
    expect(updates[0].values[12]).toBe(existing.id);
  });

  it("returns an active verified package to review when its kind changes", async () => {
    const active: PackageRow = { ...existing, status: "active", verified: 1 };
    const requeued: PackageRow = {
      ...active,
      kind: "plugin",
      install_kind: "plugin",
      latest_version: "2.7.1",
      status: "pending",
      verified: 0,
    };
    const { db, updates } = fakePackageDB([active, requeued]);

    const result = await new PackageRepo(db).publish(user, pluginInput(), now);

    expect(result.row.status).toBe("pending");
    expect(result.row.verified).toBe(0);
    expect(updates[0].values[10]).toBe("pending");
    expect(updates[0].values[11]).toBe(0);
  });

  it("returns a same-kind active package update to review", async () => {
    const active: PackageRow = { ...existing, status: "active", verified: 1 };
    const updated: PackageRow = {
      ...active,
      summary: "new summary",
      source: "https://github.com/o/r2",
      repo_url: "https://github.com/o/r2",
      install_kind: "mcp",
      latest_version: "2.7.1",
      status: "pending",
      verified: 0,
    };
    const { db, updates } = fakePackageDB([active, updated]);
    const input = PublishSchema.parse({
      kind: "mcp",
      name: "devkit",
      summary: "new summary",
      source: "https://github.com/o/r2",
      repoUrl: "https://github.com/o/r2",
      version: "2.7.1",
    });

    const result = await new PackageRepo(db).publish(user, input, now);

    expect(result.row.status).toBe("pending");
    expect(result.row.verified).toBe(0);
    expect(updates[0].values[4]).toBe("mcp");
    expect(updates[0].values[10]).toBe("pending");
    expect(updates[0].values[11]).toBe(0);
  });

  it("returns a hidden package update to review and clears verification", async () => {
    const hidden: PackageRow = { ...existing, status: "hidden", verified: 1 };
    const updated: PackageRow = {
      ...hidden,
      kind: "plugin",
      install_kind: "plugin",
      latest_version: "2.7.1",
      status: "pending",
      verified: 0,
    };
    const { db, updates } = fakePackageDB([hidden, updated]);

    const result = await new PackageRepo(db).publish(user, pluginInput(), now);

    expect(result.row.status).toBe("pending");
    expect(result.row.verified).toBe(0);
    expect(updates[0].values[10]).toBe("pending");
    expect(updates[0].values[11]).toBe(0);
  });

  it("returns a rejected package update to review", async () => {
    const rejected: PackageRow = { ...existing, status: "rejected", verified: 0 };
    const requeued: PackageRow = { ...rejected, latest_version: "2.7.1", status: "pending" };
    const { db, updates } = fakePackageDB([rejected, requeued]);
    const input = PublishSchema.parse({
      kind: "mcp",
      name: "devkit",
      source: "https://github.com/o/r",
      repoUrl: "https://github.com/o/r",
      version: "2.7.1",
    });

    const result = await new PackageRepo(db).publish(user, input, now);

    expect(result.row.status).toBe("pending");
    expect(updates[0].values[10]).toBe("pending");
    expect(updates[0].values[11]).toBe(0);
  });

  it("preserves status and verification for trusted admin updates", async () => {
    const admin: RegistryUser = { ...user, role: "admin" };
    const active: PackageRow = { ...existing, status: "active", verified: 1 };
    const updated: PackageRow = { ...active, install_kind: "mcp", latest_version: "2.7.1" };
    const { db, updates } = fakePackageDB([active, updated]);
    const input = PublishSchema.parse({
      kind: "mcp",
      name: "devkit",
      source: "https://github.com/o/r",
      repoUrl: "https://github.com/o/r",
      version: "2.7.1",
    });

    const result = await new PackageRepo(db).publish(admin, input, now);

    expect(result.row.status).toBe("active");
    expect(result.row.verified).toBe(1);
    expect(updates[0].values[10]).toBe("active");
    expect(updates[0].values[11]).toBe(1);
  });
});

describe("PackageRepo.setStatusIfCurrent", () => {
  it("approves only the exact package revision the admin reviewed", async () => {
    const approvedAt = "2026-07-22T01:00:00.000Z";
    const reviewed = "sha256:" + "ab".repeat(32);
    const approved: PackageRow = { ...existing, status: "active", updated_at: approvedAt };
    const statements: { sql: string; values: unknown[] }[] = [];
    const db = {
      prepare(sql: string) {
        let values: unknown[] = [];
        const statement = {
          bind(...bound: unknown[]) {
            values = bound;
            return statement;
          },
          async first<T>() {
            statements.push({ sql, values });
            return approved as T;
          },
          async run() {
            statements.push({ sql, values });
            return { meta: { changes: 1 } };
          },
        };
        return statement;
      },
    } as unknown as D1Database;

    const row = await new PackageRepo(db).setStatusIfCurrent(
      existing.slug,
      "active",
      existing.latest_version,
      existing.updated_at,
      existing.status,
      approvedAt,
      reviewed,
    );

    expect(row).toEqual(approved);
    // The reviewer's digest lands on the reviewed row before it goes public,
    // fenced by the same revision the status change is.
    expect(statements[0].sql).toContain("UPDATE package_versions SET content_hash = ?1");
    expect(statements[0].sql).toContain("latest_version = ?2 AND updated_at = ?4 AND status = ?5");
    expect(statements[0].values).toEqual([reviewed, existing.latest_version, existing.slug, existing.updated_at, existing.status]);
    expect(statements[1].sql).toContain("latest_version = ?4 AND updated_at = ?5 AND status = ?6");
    expect(statements[1].sql).toContain("RETURNING *");
    expect(statements[1].values).toEqual([
      "active",
      approvedAt,
      existing.slug,
      existing.latest_version,
      existing.updated_at,
      existing.status,
    ]);
  });

  it("returns null when a newer package revision no longer matches", async () => {
    const statements: { sql: string; values: unknown[] }[] = [];
    const db = {
      prepare(sql: string) {
        let values: unknown[] = [];
        const statement = {
          bind(...bound: unknown[]) {
            values = bound;
            return statement;
          },
          async first<T>() {
            statements.push({ sql, values });
            return null as T | null;
          },
          async run() {
            statements.push({ sql, values });
            return { meta: { changes: 0 } };
          },
        };
        return statement;
      },
    } as unknown as D1Database;

    const row = await new PackageRepo(db).setStatusIfCurrent(
      existing.slug,
      "active",
      existing.latest_version,
      existing.updated_at,
      existing.status,
      "2026-07-22T01:00:00.000Z",
    );

    expect(row).toBeNull();
    // Without a reviewed digest the claimed one is cleared, never promoted.
    expect(statements[0].values[0]).toBe("");
    expect(statements).toHaveLength(2);
  });
});

describe("PackageRepo.versions", () => {
  it("returns a bounded page and a stable cursor for older versions", async () => {
    let sql = "";
    const rows = [
      { id: 3, version: "0.3.0", source: "s3", content_hash: "h3", risk_level: "", created_at: "2026-07-24T00:00:00.000Z" },
      { id: 2, version: "0.2.0", source: "s2", content_hash: "h2", risk_level: "", created_at: "2026-07-23T00:00:00.000Z" },
      { id: 1, version: "0.1.0", source: "s1", content_hash: "h1", risk_level: "", created_at: "2026-07-22T00:00:00.000Z" },
    ];
    const db = {
      prepare(query: string) {
        sql = query;
        const statement = {
          bind() { return statement; },
          async all<T>() { return { results: rows as T[] }; },
        };
        return statement;
      },
    } as unknown as D1Database;
    const result = await new PackageRepo(db).versions(7, { limit: 2, before: "2026-07-25T00:00:00.000Z", beforeId: 4 });
    expect(result.versions).toHaveLength(2);
    expect(result.pageInfo).toEqual({ limit: 2, hasMore: true, nextBefore: rows[1].created_at, nextBeforeId: rows[1].id });
    expect(sql).toContain("created_at < ?2");
    expect(sql).toContain("ORDER BY created_at DESC, id DESC LIMIT ?4");
  });
});

describe("PackageRepo.list", () => {
  it("orders recommended by the materialized score", async () => {
    let sql = "";
    const db = {
      prepare(query: string) {
        sql = query;
        const statement = {
          bind() { return statement; },
          async all() { return { results: [] }; },
        };
        return statement;
      },
    } as unknown as D1Database;
    await new PackageRepo(db).list({ kind: "all", q: "", sort: "recommended", pinned: false, limit: 24, offset: 0, now });
    expect(sql).toContain("ORDER BY p.rec_score DESC, p.install_count DESC, p.created_at DESC, p.id DESC");
  });

  it("uses the daily install rollup for trending", async () => {
    let sql = "";
    const db = {
      prepare(query: string) {
        sql = query;
        const statement = {
          bind() { return statement; },
          async all() { return { results: [] }; },
        };
        return statement;
      },
    } as unknown as D1Database;
    await new PackageRepo(db).list({ kind: "all", q: "", sort: "trending", pinned: false, limit: 24, offset: 0, now });
    expect(sql).toContain("FROM package_install_daily");
    expect(sql).not.toContain("FROM events");
    expect(sql).toContain("SUM(count)");
  });
});

describe("PackageRepo.list pinned", () => {
  const digest = `sha256:${"a".repeat(64)}`;
  function seeded() {
    const sqlite = new DatabaseSync(":memory:");
    sqlite.exec(registrySchema);
    const pkg = sqlite.prepare(
      `INSERT INTO packages (kind, scope_handle, name, slug, source, latest_version, status, publisher_id, created_at, updated_at)
       VALUES ('skill', 'pub', ?1, 'pub/' || ?1, 'https://github.com/o/r', '1.0.0', 'active', 7, ?2, ?2) RETURNING id`,
    );
    const ver = sqlite.prepare(
      `INSERT INTO package_versions (package_id, version, content_hash, created_at) VALUES (?1, ?2, ?3, ?4)`,
    );
    const add = (name: string, at: string, versions: [string, string][]) => {
      const { id } = pkg.get(name, at) as { id: number };
      for (const [v, hash] of versions) ver.run(id, v, hash, at);
    };
    add("pinned", "2026-07-01T00:00:00.000Z", [["1.0.0", digest]]);
    add("empty", "2026-07-02T00:00:00.000Z", [["1.0.0", ""]]);
    add("upper", "2026-07-03T00:00:00.000Z", [["1.0.0", digest.toUpperCase().replace("SHA256", "sha256")]]);
    add("stale", "2026-07-04T00:00:00.000Z", [["0.9.0", digest], ["1.0.0", ""]]);
    const db = {
      prepare(sql: string) {
        const statement = sqlite.prepare(sql);
        const wrapper: any = {
          values: [] as unknown[],
          bind(...values: unknown[]) { wrapper.values = values; return wrapper; },
          async all() { return { results: statement.all(...wrapper.values) }; },
        };
        return wrapper;
      },
    } as unknown as D1Database;
    return { sqlite, repo: new PackageRepo(db) };
  }

  it("marks only a latest version holding a lowercase sha256 digest", async () => {
    const { sqlite, repo } = seeded();
    try {
      const rows = await repo.list({ kind: "all", q: "", sort: "new", pinned: false, limit: 24, offset: 0, now });
      expect(Object.fromEntries(rows.map((r) => [r.name, r.pinned]))).toEqual({ pinned: 1, empty: 0, upper: 0, stale: 0 });
    } finally {
      sqlite.close();
    }
  });

  it("filters in the query so a page holds only pinned packages", async () => {
    const { sqlite, repo } = seeded();
    try {
      for (const sort of ["recommended", "new", "installs", "trending"] as const) {
        const rows = await repo.list({ kind: "all", q: "", sort, pinned: true, limit: 1, offset: 0, now });
        expect(rows.map((r) => r.name)).toEqual(["pinned"]);
      }
      const next = await repo.list({ kind: "all", q: "", sort: "new", pinned: true, limit: 1, offset: 1, now });
      expect(next).toEqual([]);
    } finally {
      sqlite.close();
    }
  });
});

function sqliteD1(sqlite: any): D1Database {
  return {
    prepare(sql: string) {
      const statement = sqlite.prepare(sql);
      const wrapper: any = {
        bind(...values: unknown[]) { wrapper.values = values; return wrapper; },
        values: [] as unknown[],
        async first() { return statement.get(...wrapper.values) ?? null; },
        async all() { return { results: statement.all(...wrapper.values) }; },
        async run() { return { meta: { changes: Number(statement.run(...wrapper.values).changes) } }; },
      };
      return wrapper;
    },
  } as unknown as D1Database;
}

describe("repinReviewedDigest", () => {
  const liveAt = "2026-07-22T00:30:00.000Z";
  const digest = "sha256:" + "0f".repeat(32);

  function seed(status = "active") {
    const sqlite = new DatabaseSync(":memory:");
    sqlite.exec(registrySchema);
    sqlite.prepare(
      `INSERT INTO packages (kind, scope_handle, name, slug, source, latest_version, status, publisher_id, created_at, updated_at)
       VALUES ('skill', 'publisher', 'devkit', 'publisher/devkit', 'https://github.com/o/r', '0.2.0', ?1, 7, ?2, ?3)`,
    ).run(status, now, liveAt);
    for (const version of ["0.1.0", "0.2.0"]) {
      sqlite.prepare(
        `INSERT INTO package_versions (package_id, version, source, manifest, content_hash, risk_level, created_at)
         VALUES (1, ?1, 'https://github.com/o/r', '', '', '', ?2)`,
      ).run(version, now);
    }
    return sqlite;
  }

  const request = (over: Partial<Parameters<typeof repinReviewedDigest>[1]> = {}) => ({
    slug: "publisher/devkit",
    expectedVersion: "0.2.0",
    expectedUpdatedAt: liveAt,
    contentHash: digest,
    actor: "admin",
    now: "2026-07-23T00:00:00.000Z",
    ...over,
  });

  it("binds the digest to the live version and logs who changed it from what", async () => {
    const sqlite = seed();
    try {
      const result = await repinReviewedDigest(sqliteD1(sqlite), request());
      expect(result?.previous).toBe("");
      expect(sqlite.prepare("SELECT version, content_hash FROM package_versions ORDER BY version").all()).toEqual([
        { version: "0.1.0", content_hash: "" },
        { version: "0.2.0", content_hash: digest },
      ]);
      expect(sqlite.prepare("SELECT status, updated_at FROM packages").get()).toEqual({ status: "active", updated_at: liveAt });
      expect(sqlite.prepare("SELECT type, package_id, actor_handle, summary, created_at FROM events").all()).toEqual([
        { type: "pin", package_id: 1, actor_handle: "admin", summary: `0.2.0 unpinned -> ${digest}`, created_at: "2026-07-23T00:00:00.000Z" },
      ]);

      const next = "sha256:" + "1e".repeat(32);
      const repinned = await repinReviewedDigest(sqliteD1(sqlite), request({ contentHash: next }));
      expect(repinned?.previous).toBe(digest);
      expect(sqlite.prepare("SELECT summary FROM events ORDER BY id DESC LIMIT 1").get()).toEqual({
        summary: `0.2.0 ${digest} -> ${next}`,
      });
    } finally {
      sqlite.close();
    }
  });

  it("refuses a revision the admin did not see, and a package that is not live", async () => {
    for (const [status, over] of [
      ["active", { expectedUpdatedAt: "2026-07-22T00:45:00.000Z" }],
      ["active", { expectedVersion: "0.1.0" }],
      ["pending", {}],
      ["hidden", {}],
    ] as const) {
      const sqlite = seed(status);
      try {
        expect(await repinReviewedDigest(sqliteD1(sqlite), request(over))).toBeNull();
        expect(sqlite.prepare("SELECT COUNT(*) AS n FROM package_versions WHERE content_hash != ''").get()).toEqual({ n: 0 });
        expect(sqlite.prepare("SELECT COUNT(*) AS n FROM events").get()).toEqual({ n: 0 });
      } finally {
        sqlite.close();
      }
    }
  });
});
