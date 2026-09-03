import { describe, expect, it } from "vitest";
// @ts-expect-error Node 22+ provides node:sqlite; Worker production code does not import it.
import { DatabaseSync } from "node:sqlite";
import { groupReportsStatement, loadD1GroupReports, loadGroupDiagnostics } from "./group_queries";
import { renderGroup, type Group } from "./group";
import freshSchemaSQL from "../schema.sql?raw";

describe("group report query bounds", () => {
  it("returns only the first retained sample and the latest five reports", async () => {
    const sqlite = new DatabaseSync(":memory:");
    sqlite.exec(freshSchemaSQL);
    const fingerprint = "a".repeat(64);
    for (let id = 1; id <= 35_000; id++) {
      sqlite.prepare(
        `INSERT INTO reports (fingerprint, kind, version, arch, os, message, created_at)
         VALUES (?1, 'crash', 'v1.0.0', 'amd64', 'windows', ?2, ?3)`,
      ).run(fingerprint, `sample-${id}`, `2026-09-03T00:00:${String(id).padStart(5, "0")}Z`);
    }
    sqlite.prepare(
      `INSERT INTO reports (fingerprint, kind, version, arch, os, message, created_at)
       VALUES (?1, 'crash', 'v1.0.0', 'amd64', 'windows', 'other-group', '2026-09-30T00:00:00.000Z')`,
    ).run("b".repeat(64));

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
    try {
      const rows = await groupReportsStatement(db, fingerprint, 5).all();
      expect(rows.results.map((row) => String((row as { message: string }).message))).toEqual([
        "sample-35000", "sample-34999", "sample-34998", "sample-34997", "sample-34996", "sample-1",
      ]);
    } finally {
      sqlite.close();
    }
  });

  it("renders a graceful notice when a detail query is unavailable", () => {
    const group: Group = {
      fingerprint: "c".repeat(64), kind: "crash", count: 35_000,
      first_seen: "2026-01-01T00:00:00.000Z", last_seen: "2026-09-03T00:00:00.000Z",
      first_version: "v1.0.0", last_version: "v1.36.0", status: "open", note: "",
      title: "worker failure", source: "native.lifecycle", label: "", error_type: "Error", severity: "high",
      top_frame: "", last_os: "windows", last_arch: "amd64", last_build_commit: "",
      last_channel: "stable", resolved_in: "", resolved_at: "", regressed_at: "",
    };
    const html = renderGroup(
      group,
      [],
      { id: 1, email: "admin@example.com", role: "admin", created_at: "", approved_at: null },
      undefined,
      undefined,
      { samplesUnavailable: true, diagnosticsUnavailable: true },
    );
    expect(html).toContain("原始样本");
    expect(html).toContain("技术分布");
    expect(html).toContain("分组核心汇总仍可用");
  });

  it("turns D1 detail failures into bounded degradation instead of throwing", async () => {
    const db = { prepare() { throw new Error("simulated D1 failure"); } } as unknown as D1Database;
    const env = { DB: db } as any;
    const observe = () => {};
    await expect(loadD1GroupReports(env, "a".repeat(64), 5, observe)).resolves.toEqual({ reports: [], unavailable: true });
    await expect(loadGroupDiagnostics(env, "a".repeat(64), observe)).resolves.toEqual({ unavailable: true });
  });
});
