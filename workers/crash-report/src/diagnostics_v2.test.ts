import { describe, expect, it } from "vitest";
// @ts-expect-error Node 22+ provides node:sqlite; Worker production code does not import it.
import { DatabaseSync } from "node:sqlite";
import worker, {
  CLI_TELEMETRY_SCHEMA_SQL,
  Report,
  isDevelopmentReport,
  newestReleaseVersion,
} from "./index";
import {
  crashGroups,
  diagnosticWindowWhere,
  reportAggregateStatements,
} from "./diagnostics_v2";
import type { Env } from "./env";
import diagnosticsMigrationSQL from "../migrate-diagnostics-v2.sql?raw";
import freshSchemaSQL from "../schema.sql?raw";
import {
  classifyDiagnosticsV2Schema,
  diagnosticsV2SchemaEntries,
  diagnosticsV2SchemaQuery,
  parseWranglerRows,
} from "../scripts/apply-diagnostics-v2.mjs";

const oldReport = {
  kind: "crash",
  version: "v1.19.0",
  os: "windows",
  arch: "amd64",
  message: "legacy payload",
} as const;

describe("diagnostics v2 compatibility and privacy", () => {
  it("fails closed on a partially applied production migration", () => {
    expect(classifyDiagnosticsV2Schema([]).state).toBe("absent");
    const completeRows = (diagnosticsV2SchemaEntries as readonly string[]).map((entry: string) => {
      const split = entry.indexOf(":");
      return { kind: entry.slice(0, split), name: entry.slice(split + 1) };
    });
    expect(classifyDiagnosticsV2Schema(completeRows).state).toBe("complete");
    const partial = classifyDiagnosticsV2Schema(completeRows.slice(1));
    expect(partial.state).toBe("partial");
    expect(partial.missing).toEqual([diagnosticsV2SchemaEntries[0]]);
    expect(parseWranglerRows(JSON.stringify([{ results: completeRows }]))).toEqual(completeRows);
  });

  it("accepts old reports plus Windows and Linux runtime diagnostics", () => {
    expect(Report.safeParse(oldReport).success).toBe(true);
    expect(Report.safeParse({
      ...oldReport,
      installId: "b".repeat(32),
      channel: "stable",
      device: { osVersion: "Windows 10", osBuild: 17763, osRevision: 6293 },
      webview2: {
        kind: "browser_process_exited",
        reason: "integrity_failure",
        exitCode: -1073740760,
        processDescription: "Browser",
        failureSourceModule: "inject.dll",
        runtimeVersion: "132.0.2957.140",
        gpuDisabled: false,
        recovery: "not_applicable",
      },
    }).success).toBe(true);
    expect(Report.safeParse({
      ...oldReport,
      os: "linux",
      device: {
        distroId: "ubuntu", distroVersion: "24.04", kernelVersion: "6.8.0",
        sessionType: "wayland",
      },
      webRuntime: {
        engine: "webkitgtk", kind: "web_process_terminated", reason: "crashed",
        runtimeVersion: "2.44.2", gpuMode: "always", recovery: "reload_succeeded",
      },
    }).success).toBe(true);
  });

  it("rejects full failure-source paths and puts channel=test in development", () => {
    expect(Report.safeParse({
      ...oldReport,
      webview2: {
        kind: "browser_process_exited",
        reason: "integrity_failure",
        failureSourceModule: "C:\\Users\\alice\\inject.dll",
        runtimeVersion: "132",
        gpuDisabled: false,
        recovery: "not_applicable",
      },
    }).success).toBe(false);
    expect(isDevelopmentReport({
      ...oldReport,
      source: "frontend.global",
      label: "window.error",
      errorType: "Error",
      errorMessage: "boom",
      topFrame: "at render (assets/index.js:1:2)",
      version: "v1.23.0",
      channel: "test",
    })).toBe(true);
  });

  it("keeps fresh, migrated, and runtime-bootstrap schemas aligned", () => {
    const legacy = `
      CREATE TABLE reports (id INTEGER PRIMARY KEY);
      CREATE TABLE pings (
        date TEXT NOT NULL, install_id TEXT NOT NULL, version TEXT NOT NULL, os TEXT NOT NULL,
        arch TEXT NOT NULL, os_version TEXT NOT NULL DEFAULT '', opens INTEGER NOT NULL DEFAULT 1,
        PRIMARY KEY (date, install_id)
      );
      CREATE TABLE cli_pings (
        date TEXT NOT NULL, install_id TEXT NOT NULL, version TEXT NOT NULL, os TEXT NOT NULL,
        arch TEXT NOT NULL, os_version TEXT NOT NULL DEFAULT '', opens INTEGER NOT NULL DEFAULT 1,
        PRIMARY KEY (date, install_id)
      );
      CREATE TABLE metric_users (
        date TEXT NOT NULL, signal TEXT NOT NULL, bucket TEXT NOT NULL, install_id TEXT NOT NULL,
        version TEXT NOT NULL, os TEXT NOT NULL, PRIMARY KEY (date, signal, bucket, install_id)
      );
      CREATE TABLE cli_metric_users (
        date TEXT NOT NULL, signal TEXT NOT NULL, bucket TEXT NOT NULL, install_id TEXT NOT NULL,
        version TEXT NOT NULL, os TEXT NOT NULL, PRIMARY KEY (date, signal, bucket, install_id)
      );
    `;
    const columns = (db: DatabaseSync, table: string) =>
      db.prepare(`PRAGMA table_info(${table})`).all().map((row: Record<string, unknown>) => String(row.name));
    const fresh = new DatabaseSync(":memory:");
    const migrated = new DatabaseSync(":memory:");
    const runtimeBootstrap = new DatabaseSync(":memory:");
    try {
      fresh.exec(freshSchemaSQL);
      migrated.exec(legacy);
      migrated.exec(diagnosticsMigrationSQL);
      runtimeBootstrap.exec(CLI_TELEMETRY_SCHEMA_SQL.join(";\n"));
      const additiveColumns: Record<string, string[]> = {
        reports: ["webview2", "web_runtime"],
        pings: ["os_build", "os_revision", "distro_id", "session_type", "runtime_engine", "gpu_mode"],
        cli_pings: ["os_build", "os_revision", "distro_id", "session_type", "runtime_engine", "gpu_mode"],
      };
      for (const [table, expected] of Object.entries(additiveColumns)) {
        expect(columns(migrated, table)).toEqual(expect.arrayContaining(expected));
        expect(columns(fresh, table)).toEqual(expect.arrayContaining(expected));
      }
      expect(columns(fresh, "reports")).not.toContain("install_id");
      for (const table of ["report_daily", "report_installations", "report_event_dimensions", "diagnostics_meta"]) {
        expect(columns(migrated, table)).toEqual(columns(fresh, table));
      }
      for (const table of ["cli_pings"]) {
        expect(columns(runtimeBootstrap, table)).toEqual(columns(fresh, table));
      }
      expect(classifyDiagnosticsV2Schema(
        migrated.prepare(diagnosticsV2SchemaQuery).all(),
      ).state).toBe("complete");
    } finally {
      fresh.close();
      migrated.close();
      runtimeBootstrap.close();
    }
    expect(diagnosticsMigrationSQL).not.toMatch(/\bDROP\b/);
  });

  it("uses a date-leading index for platform impact denominators", () => {
    const db = new DatabaseSync(":memory:");
    try {
      db.exec(freshSchemaSQL);
      const plan = db.prepare(
        "EXPLAIN QUERY PLAN SELECT COUNT(DISTINCT install_id) FROM pings WHERE date >= date('now', '-29 day') AND os = 'linux' AND distro_id = 'ubuntu'",
      ).all().map((row: Record<string, unknown>) => String(row.detail)).join("\n");
      expect(plan).toMatch(/SEARCH pings USING INDEX/);
      expect(plan).not.toMatch(/SCAN pings/);
    } finally {
      db.close();
    }
  });
});

describe("stats window and release baseline", () => {
  it("uses an inclusive calendar window for diagnostic groups", () => {
    expect(diagnosticWindowWhere(7)).toBe("date(last_seen) >= date('now', '-6 day')");
    expect(diagnosticWindowWhere(30)).toBe("date(last_seen) >= date('now', '-29 day')");
  });

  it("does not promote prerelease or synthetic non-semver labels", () => {
    expect(newestReleaseVersion(["v1.19.4", "v1.20.0-beta.1", "dev", "v9.9.9-test"])).toBe("v1.19.4");
  });
});

describe("diagnostics v2 storage consistency", () => {
  it("commits every report write through one D1 batch", async () => {
    let batchCalls = 0;
    let directRuns = 0;
    let committed: Array<{ sql: string }> = [];
    const db = {
      prepare(sql: string) {
        const statement = {
          sql,
          bind() { return statement; },
          async first() { return null; },
          async run() { directRuns++; return {}; },
        };
        return statement;
      },
      async batch(statements: Array<{ sql: string }>) {
        batchCalls++;
        committed = statements;
        return [];
      },
    } as unknown as D1Database;
    const env = {
      DB: db,
      RATE_LIMITER: { async limit() { return { success: true }; } },
    } as unknown as Env;
    const body = JSON.stringify({
      installId: "a".repeat(32), kind: "crash", version: "v1.23.0",
      os: "windows", arch: "amd64", message: "browser process exited",
    });
    const response = await worker.fetch(new Request("https://crash.reasonix.io/v1/report", {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "content-length": String(new TextEncoder().encode(body).byteLength),
        "cf-connecting-ip": "127.0.0.1",
      },
      body,
    }), env);
    expect(response.status).toBe(202);
    expect(batchCalls).toBe(1);
    expect(directRuns).toBe(0);
    expect(committed.map((statement) => statement.sql)).toEqual([
      expect.stringContaining("INSERT INTO groups"),
      expect.stringContaining("INSERT INTO reports"),
      expect.stringContaining("INSERT INTO report_daily"),
      expect.stringContaining("INSERT INTO report_installations"),
      expect.stringContaining("INSERT INTO report_event_dimensions"),
      expect.stringContaining("DELETE FROM reports"),
    ]);
  });

  it("preserves separate GPU and runtime event dimensions for one installation", () => {
    type BoundStatement = { sql: string; binds: unknown[] };
    const statements: BoundStatement[] = [];
    const d1 = {
      prepare(sql: string) {
        const statement = {
          sql,
          binds: [] as unknown[],
          bind(...binds: unknown[]) { statement.binds = binds; statements.push(statement); return statement; },
        };
        return statement;
      },
    } as unknown as D1Database;
    const report = {
      installId: "a".repeat(32), version: "v1.23.0", os: "windows", arch: "amd64",
      device: { osBuild: 17763, osRevision: 6293 },
    };
    const webview = {
      engine: "webview2",
      runtimeVersion: "132", kind: "gpu_process_exited", reason: "unexpected",
      exitCode: 1, recovery: "not_applicable", gpuMode: "enabled",
    };
    reportAggregateStatements(d1, report, "f".repeat(64), "stable", webview);
    reportAggregateStatements(d1, report, "f".repeat(64), "stable", {
      ...webview, runtimeVersion: "133", gpuMode: "disabled",
    });
    const facts = statements.filter((statement) => statement.sql.includes("INSERT INTO report_event_dimensions"));
    const db = new DatabaseSync(":memory:");
    try {
      db.exec(freshSchemaSQL);
      for (const fact of facts) db.prepare(fact.sql).run(...fact.binds as []);
      expect(db.prepare(
        "SELECT runtime_version, gpu_mode, events FROM report_event_dimensions ORDER BY runtime_version",
      ).all()).toEqual([
        { runtime_version: "132", gpu_mode: "enabled", events: 1 },
        { runtime_version: "133", gpu_mode: "disabled", events: 1 },
      ]);
    } finally {
      db.close();
    }
  });

  it("preserves unidentified event dimensions without inventing an affected installation", () => {
    type BoundStatement = { sql: string; binds: unknown[] };
    const statements: BoundStatement[] = [];
    const d1 = {
      prepare(sql: string) {
        const statement = {
          sql,
          binds: [] as unknown[],
          bind(...binds: unknown[]) { statement.binds = binds; statements.push(statement); return statement; },
        };
        return statement;
      },
    } as unknown as D1Database;
    reportAggregateStatements(d1, {
      version: "v1.23.0", os: "linux", arch: "amd64",
      device: { distroId: "ubuntu", distroVersion: "24.04", sessionType: "wayland" },
    }, "f".repeat(64), "stable", {
      engine: "webkitgtk", runtimeVersion: "2.44", kind: "web_process_terminated", reason: "crashed",
      recovery: "reload_failed", gpuMode: "always",
    });
    const db = new DatabaseSync(":memory:");
    try {
      db.exec(freshSchemaSQL);
      for (const statement of statements) db.prepare(statement.sql).run(...statement.binds as []);
      expect(db.prepare("SELECT events, identified_events FROM report_daily").get()).toEqual({
        events: 1, identified_events: 0,
      });
      expect(db.prepare(
        "SELECT install_id, distro_id, recovery, events FROM report_event_dimensions",
      ).get()).toEqual({ install_id: "", distro_id: "ubuntu", recovery: "reload_failed", events: 1 });
      expect(db.prepare("SELECT COUNT(*) AS count FROM report_installations").get()).toEqual({ count: 0 });
    } finally {
      db.close();
    }
  });

  it("uses the same dimensions for filtered events, identity coverage, and installations", async () => {
    let querySQL = "";
    let queryBinds: unknown[] = [];
    const row = {
      fingerprint: "f".repeat(64), status: "open", severity: "high", regressed_at: "",
      first_version: "v1.23.0", count: 2, seen: "2026-08-10", title: "renderer exited",
      last_version: "v1.23.0", last_channel: "stable", affected_installs: 1,
      window_events: 2, identified_events: 1, active_build_installs: 10,
      dimension_base_installs: 10, dimension_covered_installs: 10, kind: "exception",
      source: "web.runtime.native", label: "renderer_process_exited", error_type: "", top_frame: "",
      last_os: "windows", last_arch: "amd64",
    };
    const db = {
      prepare(sql: string) {
        querySQL = sql;
        const statement = {
          bind(...binds: unknown[]) { queryBinds = binds; return statement; },
          async all() { return { results: [row] }; },
        };
        return statement;
      },
    } as unknown as D1Database;
    const result = await crashGroups({ DB: db } as unknown as Env, {
      status: "", source: "", version: "", os: "", platform: "", osBuild: "17763", arch: "",
      channel: "", runtimeVersion: "", failureKind: "", failureReason: "", recovery: "", gpu: "",
      newLatest: false, regressed: false, windowDays: 7,
    }, "");
    expect(querySQL).toContain("COUNT(DISTINCT NULLIF(install_id, '')) AS affected_installs");
    expect(querySQL).toContain("SUM(events) AS window_events");
    expect(querySQL).toContain("SUM(CASE WHEN install_id <> '' THEN events ELSE 0 END) AS identified_events");
    expect(querySQL).toContain("os_build = ?");
    expect(querySQL).toContain("COALESCE(diagnostics.window_events, 0) > 0");
    expect(querySQL).not.toContain("FROM report_daily WHERE");
    expect(queryBinds).toContain(17763);
    expect(result.results[0]).toMatchObject({ identity_coverage: 0.5, impact_rate: 0.1 });
  });

  it("orders the SQL limit and returned groups by affected installations first", async () => {
    let querySQL = "";
    const row = (fingerprint: string, severity: string, affectedInstalls: number) => ({
      fingerprint, status: "open", severity, regressed_at: "", first_version: "v1.23.0",
      count: 100, seen: "2026-08-10", title: "browser process exited", last_version: "v1.23.0",
      last_channel: "stable", affected_installs: affectedInstalls, window_events: 100,
      identified_events: 100, active_build_installs: 0, kind: "crash", source: "desktop.webview2",
      label: "browser_process_exited", error_type: "", top_frame: "", last_os: "windows", last_arch: "amd64",
    });
    const db = {
      prepare(sql: string) {
        querySQL = sql;
        return { async all() { return { results: [row("b".repeat(64), "critical", 1), row("a".repeat(64), "low", 20)] }; } };
      },
    } as unknown as D1Database;
    const result = await crashGroups({ DB: db } as unknown as Env, {
      status: "", source: "", version: "", os: "", platform: "", osBuild: "", arch: "", channel: "",
      runtimeVersion: "", failureKind: "", failureReason: "", recovery: "", gpu: "",
      newLatest: false, regressed: false, windowDays: 7,
    }, "");
    expect(querySQL).toContain("FROM report_event_dimensions");
    expect(querySQL.indexOf("affected_installs DESC")).toBeLessThan(querySQL.indexOf("CASE WHEN status = 'open'"));
    expect(result.results.map((group) => group.fingerprint)).toEqual(["a".repeat(64), "b".repeat(64)]);
  });
});
