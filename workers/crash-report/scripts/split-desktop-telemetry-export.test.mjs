import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { parseArguments, splitDesktopTelemetryExport } from "./split-desktop-telemetry-export.mjs";

const temporaryDirectories = [];

afterEach(async () => {
  await Promise.all(temporaryDirectories.splice(0).map((path) => rm(path, { recursive: true, force: true })));
});

async function fixture(lines) {
  const root = await mkdtemp(join(tmpdir(), "reasonix-telemetry-split-"));
  temporaryDirectories.push(root);
  const input = join(root, "export.sql");
  const outputDir = join(root, "chunks");
  await writeFile(input, `${lines.join("\n")}\n`);
  return { input, outputDir };
}

describe("desktop telemetry export splitter", () => {
  it("splits by cutoff, chunks output, and makes replay idempotent", async () => {
    const { input, outputDir } = await fixture([
      "PRAGMA defer_foreign_keys=TRUE;",
      'INSERT INTO "pings" ("date","install_id") VALUES(\'2026-09-25\',\'a\');',
      'INSERT INTO "metrics" ("date","version") VALUES(\'2026-09-26\',\'2.0\');',
      'INSERT INTO "pings" ("date","install_id") VALUES(\'2026-09-27\',\'b\');',
    ]);

    const manifest = await splitDesktopTelemetryExport({ input, outputDir, cutoff: "2026-09-26", maxStatements: 1 });

    expect(manifest.history.statements).toBe(1);
    expect(manifest.current.statements).toBe(2);
    expect(manifest.current.files).toHaveLength(2);
    expect(await readFile(manifest.history.files[0].path, "utf8")).toContain("INSERT OR REPLACE INTO");
  });

  it("rejects unknown SQL and removes partial output", async () => {
    const { input, outputDir } = await fixture([
      'INSERT INTO "pings" ("date") VALUES(\'2026-09-25\');',
      "DROP TABLE pings;",
    ]);

    await expect(splitDesktopTelemetryExport({ input, outputDir, cutoff: "2026-09-26", maxStatements: 10 }))
      .rejects.toThrow("Unexpected SQL at line 2");
    await expect(readFile(join(outputDir, "manifest.json"), "utf8")).rejects.toThrow();
  });

  it("validates required CLI arguments", () => {
    expect(() => parseArguments(["--input", "dump.sql"])).toThrow("Required arguments");
    expect(() => parseArguments([
      "--input", "dump.sql", "--output-dir", "chunks", "--cutoff", "09/26/2026",
    ])).toThrow("YYYY-MM-DD");
  });
});
