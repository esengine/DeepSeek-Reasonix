import { describe, expect, it } from "vitest";
// @ts-expect-error Node types are intentionally not part of the Worker build.
import { readFileSync } from "node:fs";
import schema from "../schema.sql?raw";
import migration from "../migrate-dashboard-indexes.sql?raw";
import registryMigration from "../registry-migrate-performance-indexes.sql?raw";
import registrySchema from "../registry-schema.sql?raw";
// @ts-expect-error Node types are intentionally not part of the Worker build.
import { DatabaseSync } from "node:sqlite";

const workflow = readFileSync("../../.github/workflows/deploy-crash-worker.yml", "utf8");

describe("crash and registry performance migrations", () => {
  it("keeps crash indexes in fresh and incremental schemas", () => {
    for (const sql of [schema, migration]) {
      expect(sql).toMatch(/CREATE INDEX IF NOT EXISTS report_daily_fingerprint_date\s+ON report_daily \(fingerprint, date\)/);
      expect(sql).toMatch(/CREATE INDEX IF NOT EXISTS firebase_crash_outbox_fingerprint\s+ON firebase_crash_outbox \(fingerprint\)/);
      expect(sql).toMatch(/CREATE INDEX IF NOT EXISTS reports_fingerprint_id\s+ON reports \(fingerprint, id DESC\)/);
    }
  });

  it("keeps registry migrations additive and wired before deployment", () => {
    for (const sql of [registrySchema, registryMigration]) {
      expect(sql).toMatch(/CREATE INDEX IF NOT EXISTS packages_active_created/);
      expect(sql).toMatch(/CREATE INDEX IF NOT EXISTS packages_active_kind_created/);
      expect(sql).toMatch(/CREATE INDEX IF NOT EXISTS packages_active_installs/);
      expect(sql).toMatch(/CREATE TABLE IF NOT EXISTS package_install_daily/);
      expect(sql).toMatch(/CREATE TABLE IF NOT EXISTS registry_migration_meta/);
    }
    expect(registryMigration).not.toMatch(/\b(?:DROP|ALTER)\b/);
    expect(registryMigration).toMatch(/INSERT INTO package_install_daily/);
    expect(registryMigration).toMatch(/ON CONFLICT \(date, package_id\) DO UPDATE/);
    expect(workflow).toContain("npm run migrate:registry-indexes");
    expect(workflow).toContain("npm run migrate:dashboard-indexes");
  });

  it("makes per-group cleanup probes use the new fingerprint indexes", () => {
    const db = new DatabaseSync(":memory:");
    try {
      db.exec(schema);
      db.exec(migration);
      const dailyPlan = db.prepare(
        "EXPLAIN QUERY PLAN DELETE FROM report_daily WHERE fingerprint = 'x'",
      ).all().map((row: Record<string, unknown>) => String(row.detail)).join(" ");
      const outboxPlan = db.prepare(
        "EXPLAIN QUERY PLAN SELECT event_id FROM firebase_crash_outbox WHERE fingerprint = 'x' LIMIT 1",
      ).all().map((row: Record<string, unknown>) => String(row.detail)).join(" ");
      const reportPlan = db.prepare(
        "EXPLAIN QUERY PLAN SELECT id FROM reports INDEXED BY reports_fingerprint_id WHERE fingerprint = 'x' ORDER BY id DESC LIMIT 5",
      ).all().map((row: Record<string, unknown>) => String(row.detail)).join(" ");
      expect(dailyPlan).toContain("USING INDEX report_daily_fingerprint_date");
      expect(outboxPlan).toContain("USING INDEX firebase_crash_outbox_fingerprint");
      expect(reportPlan).toContain("USING COVERING INDEX reports_fingerprint_id");
    } finally {
      db.close();
    }
  });

  it("makes active package ordering use partial indexes", () => {
    const db = new DatabaseSync(":memory:");
    try {
      db.exec(registrySchema);
      db.exec(registryMigration);
      const installsPlan = db.prepare(
        "EXPLAIN QUERY PLAN SELECT id FROM packages WHERE status = 'active' ORDER BY install_count DESC, created_at DESC LIMIT 24",
      ).all().map((row: Record<string, unknown>) => String(row.detail)).join(" ");
      const kindPlan = db.prepare(
        "EXPLAIN QUERY PLAN SELECT id FROM packages WHERE status = 'active' AND kind = 'skill' ORDER BY created_at DESC LIMIT 24",
      ).all().map((row: Record<string, unknown>) => String(row.detail)).join(" ");
      expect(installsPlan).toContain("packages_active_installs");
      expect(installsPlan).not.toContain("USE TEMP B-TREE FOR ORDER BY");
      expect(kindPlan).toContain("packages_active_kind_created");
      expect(kindPlan).not.toContain("USE TEMP B-TREE FOR ORDER BY");
    } finally {
      db.close();
    }
  });

  it("backfills install days exactly once and remains safe to rerun", () => {
    const db = new DatabaseSync(":memory:");
    try {
      db.exec(registrySchema);
      db.exec("INSERT INTO events (type, package_id, created_at) VALUES ('install', 7, '2026-08-20T01:00:00.000Z'), ('install', 7, '2026-08-20T02:00:00.000Z')");
      db.exec(registryMigration);
      db.exec(registryMigration);
      expect(db.prepare("SELECT date, package_id, count FROM package_install_daily").all()).toEqual([
        { date: "2026-08-20", package_id: 7, count: 2 },
      ]);
    } finally {
      db.close();
    }
  });
});
