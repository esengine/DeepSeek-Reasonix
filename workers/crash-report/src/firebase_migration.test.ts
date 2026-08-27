import { describe, expect, it } from "vitest";
import {
  assessMigrationCapacity,
  buildFirebaseGroups,
  canonicalJSONString,
  classifyMigrationGroup,
  contentDigest,
  firebaseOAuthGrantType,
  runWrangler,
  wranglerD1MaxBufferBytes,
} from "../scripts/migrate-firebase-data.mjs";

describe("Firebase retained-sample migration", () => {
  it("uses the OAuth JWT bearer grant expected by Google's token endpoint", () => {
    expect(firebaseOAuthGrantType).toBe("urn:ietf:params:oauth:grant-type:jwt-bearer");
  });

  it("explains active reservations without treating stale open groups as automatically eligible", () => {
    const now = new Date("2026-08-25T00:00:00Z");
    const assessment = assessMigrationCapacity([
      { status: "open", last_seen: "2026-08-20T00:00:00Z", fingerprint: "private", message: "private" },
      { status: "open", last_seen: "2026-07-10T00:00:00Z" },
      { status: "open", last_seen: "2026-06-01T00:00:00Z" },
      { status: "resolved", last_seen: "2026-08-20T00:00:00Z" },
      { status: "resolved", last_seen: "2026-07-10T00:00:00Z" },
      { status: "ignored", last_seen: "2026-06-01T00:00:00Z" },
      { status: "resolved", last_seen: "invalid" },
      { status: "future-status", last_seen: "2026-06-01T00:00:00Z" },
    ], now);

    expect(assessment.statusTotals).toEqual({ open: 3, resolved: 3, ignored: 1, other: 1 });
    expect(assessment.activeReasons).toEqual({
      open: 3,
      otherStatus: 1,
      recentResolvedOrIgnored: 1,
      invalidResolvedOrIgnored: 1,
    });
    expect(assessment.automaticRetention).toEqual({ compacted: 1, archived: 1 });
    expect(assessment.manualReview).toEqual({ open30to59d: 1, open60dPlus: 1, otherStatus30dPlus: 1 });
    expect(JSON.stringify(assessment)).not.toContain("private");
  });

  it("captures a full retained-report page without using Node's default child-process buffer", () => {
    let configuredMaxBuffer = 0;
    const rows = runWrangler("/tmp/crash-worker", "reasonix-crash", "SELECT 1", (
      _command: string,
      _args: string[],
      options: { maxBuffer?: number },
    ) => {
      configuredMaxBuffer = options.maxBuffer ?? 0;
      return { status: 0, stdout: '[{"results":[{"value":1}]}]' };
    });

    expect(rows).toEqual([{ value: 1 }]);
    expect(configuredMaxBuffer).toBe(wranglerD1MaxBufferBytes);
    expect(configuredMaxBuffer).toBeGreaterThan(200 * 6 * 96 * 1024);
  });

  it("keeps the first sample and maps the newest five into absolute ring slots", () => {
    const fingerprint = "a".repeat(64);
    const groups = buildFirebaseGroups([{
      fingerprint,
      kind: "crash",
      count: 8,
      first_seen: "2026-08-01T00:00:00Z",
      last_seen: "2026-08-08T00:00:00Z",
      first_version: "v1",
      last_version: "v2",
      status: "open",
      severity: "high",
    }], [1, 4, 5, 6, 7, 8].map((id) => ({
      id,
      fingerprint,
      kind: "crash",
      version: id === 1 ? "v1" : "v2",
      os: "linux",
      arch: "amd64",
      message: `sample-${id}`,
      created_at: `2026-08-0${id}T00:00:00Z`,
      device: "{}",
      breadcrumbs: "[]",
    })));
    const entry = groups.get(fingerprint) as unknown as { state: string; value: {
      meta: { count: number };
      samples: { first: { message: string; groupCount: number }; latest: Record<string, { message: string; eventId: string; sampleEpoch: number }> };
    } };
    const value = entry.value;
    expect(entry.state).toBe("active");
    expect(value.meta.count).toBe(8);
    expect(value.samples.first.message).toBe("sample-1");
    expect(value.samples.first.groupCount).toBe(1);
    expect(Object.values(value.samples.latest).map((sample) => sample.message).sort()).toEqual([
      "sample-4", "sample-5", "sample-6", "sample-7", "sample-8",
    ]);
    expect(value.samples.latest[2].message).toBe("sample-8");
    expect(value.samples.latest[2].eventId).toMatch(/^[0-9a-f]{32}$/);
    expect(value.samples.latest[2]).toMatchObject({ groupCount: 8, sampleEpoch: 1 });
    expect(JSON.stringify(value)).not.toContain("installId");
  });

  it("classifies resolved retention windows and canonicalizes readback digests", () => {
    const now = new Date("2026-08-25T00:00:00Z");
    expect(classifyMigrationGroup({ status: "open", last_seen: "2020-01-01T00:00:00Z" }, now)).toBe("active");
    expect(classifyMigrationGroup({ status: "resolved", last_seen: "2026-07-10T00:00:00Z" }, now)).toBe("compacted");
    expect(classifyMigrationGroup({ status: "ignored", last_seen: "2026-06-01T00:00:00Z" }, now)).toBe("archived");
    expect(canonicalJSONString({ b: 2, a: { d: 4, c: 3 } })).toBe('{"a":{"c":3,"d":4},"b":2}');
    expect(contentDigest({ b: 2, a: 1 })).toBe(contentDigest({ a: 1, b: 2 }));
  });
});
