import { describe, expect, it } from "vitest";
import {
  groupFingerprintFromPath,
  isDevelopmentReport,
  effectiveGroupSeverity,
  isDevelopmentGroup,
  isKnownNonCrashDiagnostic,
  namespaceReportFingerprint,
  normalizeForFingerprint,
  Ping,
  Metrics,
  CLI_TELEMETRY_SCHEMA_SQL,
  ensureCLITelemetrySchema,
  severityForReport,
  maxSeverity,
  nativeWebRuntimeFingerprintBasis,
  telemetryTableNames,
} from "./index";
import type { Env } from "./env";
import { renderStats } from "./stats";
import clientSurfaceMigrationSQL from "../migrate-client-surface.sql?raw";

const base = {
  kind: "crash",
  source: "frontend.global",
  label: "window.error",
  errorType: "Error",
  errorMessage: "boom",
  topFrame: "at render (assets/index.js:1:2)",
};

describe("metrics compatibility", () => {
  it("defaults old ping and metrics payloads to desktop", () => {
    const ping = Ping.parse({
      installId: "a".repeat(32),
      version: "v1.20.0",
      os: "darwin",
      arch: "arm64",
    });
    const metrics = Metrics.parse({
      version: "v1.20.0",
      os: "darwin",
      counters: [{ signal: "turns", bucket: "count", count: 1 }],
    });
    expect(ping.surface).toBe("desktop");
    expect(metrics.surface).toBe("desktop");
  });

  it("accepts CLI surface and fixed CLI signals", () => {
    const parsed = Metrics.safeParse({
      surface: "cli",
      version: "v1.20.0",
      os: "linux",
      counters: [
        { signal: "cli_mode", bucket: "run", count: 1 },
        { signal: "cli_profile", bucket: "delivery", count: 1 },
        { signal: "cli_turn_latency", bucket: "s_5_15", count: 1 },
        { signal: "cli_exit", bucket: "success", count: 1 },
      ],
    });
    expect(parsed.success).toBe(true);
    if (parsed.success) expect(parsed.data.surface).toBe("cli");
  });

  it("rejects invalid client surfaces", () => {
    expect(
      Metrics.safeParse({
        surface: "server",
        version: "v1.20.0",
        os: "linux",
        counters: [{ signal: "turns", bucket: "count", count: 1 }],
      }).success,
    ).toBe(false);
  });

  it("drops unknown signals without rejecting known counters in the batch", () => {
    const payload = {
      version: "v1.17.16",
      os: "darwin",
      counters: [
        { signal: "settings_auto_plan", bucket: "off", count: 1 },
        { signal: "cache_hit", bucket: "90_100", count: 1 },
      ],
    };

    const parsed = Metrics.safeParse(payload);
    expect(parsed.success).toBe(true);
    if (!parsed.success) return;
    expect(parsed.data.counters).toEqual([{ signal: "cache_hit", bucket: "90_100", count: 1 }]);
  });

  it("accepts an all-unknown batch as an empty no-op", () => {
    const parsed = Metrics.safeParse({
      version: "v1.18.0",
      os: "darwin",
      counters: [{ signal: "future_signal", arbitrary: "future payload" }],
    });

    expect(parsed.success).toBe(true);
    if (!parsed.success) return;
    expect(parsed.data.counters).toEqual([]);
  });

  it("still rejects malformed counters for known signals", () => {
    expect(
      Metrics.safeParse({
        version: "v1.18.0",
        os: "darwin",
        counters: [{ signal: "cache_hit", bucket: "not allowed", count: 1 }],
      }).success,
    ).toBe(false);
  });

  it("accepts desktop lifecycle and Windows diagnostic counters", () => {
    const parsed = Metrics.safeParse({
      version: "v1.19.0",
      os: "windows",
      counters: [
        { signal: "desktop_exit_phase", bucket: "healthy", count: 2 },
        { signal: "desktop_uptime", bucket: "m_2_10", count: 2 },
        { signal: "desktop_webview2_failure", bucket: "gpu_process_exited", count: 1 },
        { signal: "desktop_restore", bucket: "timeout", count: 1 },
      ],
    });

    expect(parsed.success).toBe(true);
    if (!parsed.success) return;
    expect(parsed.data.counters).toHaveLength(4);
  });
});

describe("telemetry deployment order compatibility", () => {
  it("keeps the released Desktop tables unchanged and isolates CLI rows", () => {
    expect(telemetryTableNames("desktop")).toEqual({
      pings: "pings",
      metrics: "metrics",
    });
    expect(telemetryTableNames("cli")).toEqual({
      pings: "cli_pings",
      metrics: "cli_metrics",
    });
  });

  it("keeps the migration additive when it runs before the released Worker", () => {
    expect(clientSurfaceMigrationSQL).not.toMatch(/\b(?:DROP|ALTER)\b/);
    expect(clientSurfaceMigrationSQL).not.toMatch(
      /CREATE TABLE(?: IF NOT EXISTS)?\s+(?:pings|metrics|metric_users)\b/,
    );
    for (const table of ["cli_pings", "cli_metrics", "cli_metric_users"]) {
      expect(clientSurfaceMigrationSQL).toMatch(
        new RegExp(`CREATE TABLE IF NOT EXISTS\\s+${table}\\b`),
      );
    }
  });

  it("extends the legacy CLI migration with the diagnostics bootstrap columns", () => {
    const runtimeSchema = CLI_TELEMETRY_SCHEMA_SQL.join("\n");
    expect(runtimeSchema).toContain("os_build INTEGER NOT NULL DEFAULT 0");
    expect(runtimeSchema).toContain("os_revision INTEGER NOT NULL DEFAULT 0");
    expect(runtimeSchema).toContain("arch TEXT NOT NULL");
  });

  it("uses additive idempotent DDL when the Worker deploys before the migration", async () => {
    const prepared: string[] = [];
    let batches = 0;
    const db = {
      prepare(sql: string) {
        prepared.push(sql);
        return { sql };
      },
      async batch() {
        batches++;
        return [];
      },
    } as unknown as D1Database;

    await Promise.all([
      ensureCLITelemetrySchema({ DB: db }),
      ensureCLITelemetrySchema({ DB: db }),
    ]);

    expect(batches).toBe(1);
    expect(prepared).toEqual([...CLI_TELEMETRY_SCHEMA_SQL]);
    expect(prepared.every((sql) => /CREATE (?:TABLE|INDEX) IF NOT EXISTS/.test(sql))).toBe(true);
    expect(prepared.join("\n")).not.toMatch(/\b(?:DROP|ALTER)\b/);
  });

  it("retries schema initialization after a transient D1 failure", async () => {
    let batches = 0;
    const db = {
      prepare(sql: string) {
        return { sql };
      },
      async batch() {
        batches++;
        if (batches === 1) throw new Error("temporary D1 failure");
        return [];
      },
    } as unknown as D1Database;

    await expect(ensureCLITelemetrySchema({ DB: db })).rejects.toThrow("temporary D1 failure");
    await expect(ensureCLITelemetrySchema({ DB: db })).resolves.toBeUndefined();
    expect(batches).toBe(2);
  });
});

describe("diagnostic classification", () => {

  it("keeps native runtime fingerprints independent from recovery outcomes", () => {
    const failure = { engine: "webview2", kind: "render_process_exited", reason: "crashed", exitCode: 1 };
    expect(nativeWebRuntimeFingerprintBasis(failure)).toBe(
      nativeWebRuntimeFingerprintBasis({ ...failure }),
    );
    expect(nativeWebRuntimeFingerprintBasis({ ...failure, reason: "out_of_memory" })).not.toBe(
      nativeWebRuntimeFingerprintBasis(failure),
    );
  });

  it("bounds unknown runtime buckets and WebView2 unresponsive exit code", () => {
    expect(nativeWebRuntimeFingerprintBasis({
      engine: "webkitgtk", kind: "random_kind_123", reason: "random_reason_456",
    })).toBe("webkitgtk\nunknown\nunknown\nunknown");
    expect(nativeWebRuntimeFingerprintBasis({
      engine: "webview2", kind: "render_process_unresponsive", reason: "unresponsive", exitCode: 259,
    })).toBe("webview2\nrender_process_unresponsive\nunresponsive\nunknown");
  });

  it("only upgrades aggregate severity", () => {
    expect(maxSeverity("high", "low")).toBe("high");
    expect(maxSeverity("low", "high")).toBe("high");
    expect(maxSeverity("critical", "high")).toBe("critical");
  });
  it("keeps development reports out of release crash priority", () => {
    expect(isDevelopmentReport({ ...base, version: "dev-32bit" })).toBe(true);
    expect(isDevelopmentReport({ ...base, version: "v1.40.0", channel: "dev" })).toBe(true);
    expect(severityForReport({ ...base, version: "dev" })).toBe("low");
  });

  it("downranks browser notices and recovered React renders", () => {
    expect(
      isKnownNonCrashDiagnostic({ ...base, errorMessage: "ResizeObserver loop limit exceeded" }),
    ).toBe(true);
    expect(
      isKnownNonCrashDiagnostic({ ...base, errorMessage: "Minified React error #520; recovered" }),
    ).toBe(true);
    expect(
      severityForReport({ ...base, errorMessage: "additional File object is not a file on the disk" }),
    ).toBe("low");
  });

  it("keeps actionable release crashes high", () => {
    expect(severityForReport({ ...base, version: "v1.40.0", channel: "stable" })).toBe("high");
  });

  it("reclassifies historical groups before dashboard prioritization", () => {
    expect(
      effectiveGroupSeverity({
        fingerprint: "a".repeat(64),
        severity: "high",
        title: "[window.error] ResizeObserver loop limit exceeded",
      }),
    ).toBe("low");
    expect(
      effectiveGroupSeverity({
        fingerprint: `dev:${"b".repeat(64)}`,
        severity: "critical",
        title: "[window.error] ResizeObserver loop limit exceeded",
      }),
    ).toBe("critical");
  });

  it("keeps ambiguous legacy history out of the development-only lane", () => {
    const fingerprint = "c".repeat(64);
    const stableThenDevelopment = {
      fingerprint,
      severity: "high",
      title: "[window.error] actionable release crash",
      first_version: "v1.17.15",
      last_version: "dev-32bit",
      last_channel: "dev",
    };
    const developmentThenStable = {
      ...stableThenDevelopment,
      first_version: "dev-32bit",
      last_version: "v1.17.15",
      last_channel: "stable",
    };
    // A retained first/last summary cannot distinguish a dev-only group from
    // dev -> stable -> dev once the middle release sample has been pruned.
    const developmentAroundStable = {
      ...stableThenDevelopment,
      first_version: "dev-32bit",
      last_version: "dev-32bit",
      last_channel: "dev",
    };
    expect(isDevelopmentGroup(stableThenDevelopment)).toBe(false);
    expect(effectiveGroupSeverity(stableThenDevelopment)).toBe("high");
    expect(isDevelopmentGroup(developmentThenStable)).toBe(false);
    expect(effectiveGroupSeverity(developmentThenStable)).toBe("high");
    expect(isDevelopmentGroup(developmentAroundStable)).toBe(false);
    expect(effectiveGroupSeverity(developmentAroundStable)).toBe("high");
  });
});

describe("development fingerprint namespace", () => {
  const hash = "d".repeat(64);

  it("preserves stable fingerprints and isolates development reports", () => {
    expect(namespaceReportFingerprint(hash, false)).toBe(hash);
    expect(namespaceReportFingerprint(hash, true)).toBe(`dev:${hash}`);
  });

  it("recognizes namespaced development groups independently of version labels", () => {
    expect(
      isDevelopmentGroup({
        fingerprint: `dev:${hash}`,
      }),
    ).toBe(true);
  });

  it("keeps namespaced fingerprints reachable from dashboard links", () => {
    expect(groupFingerprintFromPath(`/stats/group/dev:${hash}`)).toBe(`dev:${hash}`);
    expect(groupFingerprintFromPath(`/stats/group/${hash}`)).toBe(hash);
    expect(groupFingerprintFromPath("/stats/group/dev:not-a-hash")).toBe(null);
  });
});

describe("opaque crash fingerprints", () => {
  const opaque = {
    kind: "crash",
    source: "frontend.global",
    label: "window.error",
    errorType: "string",
    errorMessage: "Script error.",
    message: "[window.error]\n\nScript error.",
    topFrame: "",
  };

  it("splits locationless Script error reports by safe context hint", () => {
    const startup = normalizeForFingerprint({ ...opaque, fingerprintHint: "build:abc|view:app://reasonix/|cats:startup>tabs" });
    const markdown = normalizeForFingerprint({ ...opaque, fingerprintHint: "build:abc|view:app://reasonix/|cats:render>markdown" });
    expect(startup).not.toBe(markdown);
  });

  it("preserves grouping when old clients omit the optional hint", () => {
    expect(normalizeForFingerprint(opaque)).toBe(normalizeForFingerprint({ ...opaque, fingerprintHint: "" }));
    expect(normalizeForFingerprint(opaque)).toBe(
      "crash\nfrontend.global\nwindow.error\nstring\n\nScript error.",
    );
  });
});

describe("diagnostics dashboard lanes", () => {
  it("keeps release, performance, development, and notices out of one another's priority lists", () => {
    type StatsData = Parameters<typeof renderStats>[0];
    const row = {
      fingerprint: "fingerprint",
      kind: "crash",
      count: 1,
      first_version: "v1.40.0",
      last_version: "v1.40.0",
      seen: "2026-07-19",
      status: "open",
      title: "release-actionable",
      source: "frontend.global",
      label: "window.error",
      error_type: "Error",
      top_frame: "at render",
      severity: "high",
      last_os: "windows",
      last_arch: "amd64",
      last_channel: "stable",
      regressed_at: "",
    };
    const data: StatsData = {
      daily: [],
      versions: [],
      platforms: [],
      crashes: [
        row,
        { ...row, fingerprint: "perf", kind: "performance", title: "performance-only", severity: "medium" },
        {
          ...row,
          fingerprint: `dev:${"e".repeat(64)}`,
          title: "development-only",
          last_version: "dev-32bit",
          last_channel: "DEV",
          severity: "low",
          development: true,
        },
        { ...row, fingerprint: "notice", title: "browser-notice-only", severity: "low" },
      ],
      metrics: [],
      previousMetrics: [],
      sources: [],
      overview: { latestAdoptionPct: null, openReports: 4, newLatestReports: 0, regressedReports: 0, criticalOpenReports: 1 },
      latestVersion: "v1.40.0",
      filters: {
        surface: "desktop",
        status: "",
        source: "",
        version: "",
        os: "",
        platform: "",
        newLatest: false,
        regressed: false,
        windowDays: 30,
      },
    };

    const html = renderStats(
      data,
      { id: 1, email: "admin@example.com", role: "admin", created_at: "", approved_at: "" },
      "diagnostics",
    );
    const releaseLane = html.slice(html.indexOf("Needs attention"), html.indexOf("Performance signals"));
    const performanceLane = html.slice(html.indexOf("Performance signals"), html.indexOf("Development diagnostics"));
    const developmentLane = html.slice(html.indexOf("Development diagnostics"), html.indexOf("Report filters"));

    expect(releaseLane).toContain("release-actionable");
    expect(releaseLane).not.toContain("performance-only");
    expect(releaseLane).not.toContain("development-only");
    expect(releaseLane).not.toContain("browser-notice-only");
    expect(performanceLane).toContain("performance-only");
    expect(performanceLane).not.toContain("development-only");
    expect(developmentLane).toContain("development-only");
  });

  it("preserves the CLI surface in dashboard navigation and filters", () => {
    type StatsData = Parameters<typeof renderStats>[0];
    const data: StatsData = {
      daily: [],
      versions: [],
      platforms: [],
      crashes: [],
      metrics: [],
      previousMetrics: [],
      sources: [],
      overview: { latestAdoptionPct: null, openReports: 0, newLatestReports: 0, regressedReports: 0, criticalOpenReports: 0 },
      latestVersion: "",
      filters: {
        surface: "cli",
        status: "",
        source: "",
        version: "",
        os: "",
        platform: "",
        newLatest: false,
        regressed: false,
        windowDays: 30,
      },
    };
    const html = renderStats(
      data,
      { id: 1, email: "viewer@example.com", role: "viewer", created_at: "", approved_at: "" },
      "usage",
    );
    expect(html).toContain("surface=cli");
    expect(html).toContain('aria-label="Client surface"');
    expect(html).toContain('href="/stats"');
  });

});
