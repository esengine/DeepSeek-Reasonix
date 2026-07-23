import { describe, expect, it } from "vitest";
import {
  groupFingerprintFromPath,
  isDevelopmentReport,
  effectiveGroupSeverity,
  isDevelopmentGroup,
  isKnownNonCrashDiagnostic,
  namespaceReportFingerprint,
  normalizeForFingerprint,
  Metrics,
  severityForReport,
} from "./index";
import { renderStats } from "./stats";

const base = {
  kind: "crash",
  source: "frontend.global",
  label: "window.error",
  errorType: "Error",
  errorMessage: "boom",
  topFrame: "at render (assets/index.js:1:2)",
};

describe("metrics compatibility", () => {
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
});

describe("diagnostic classification", () => {
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
      metricUsers: [],
      sources: [],
      overview: { latestAdoptionPct: null, openReports: 4, newLatestReports: 0, regressedReports: 0, criticalOpenReports: 1 },
      latestVersion: "v1.40.0",
      filters: {
        status: "",
        source: "",
        version: "",
        os: "",
        platform: "",
        newLatest: false,
        regressed: false,
        windowDays: 30,
        preferenceMode: "users",
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
});
