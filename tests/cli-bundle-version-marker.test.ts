import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

describe("copy-dashboard-vendor-css", () => {
  let tmp: string;

  beforeEach(() => {
    tmp = mkdtempSync(join(tmpdir(), "reasonix-copy-vendor-"));
    mkdirSync(join(tmp, "node_modules/highlight.js/styles"), { recursive: true });
    mkdirSync(join(tmp, "node_modules/uplot/dist"), { recursive: true });
    writeFileSync(join(tmp, "node_modules/highlight.js/styles/github-dark.min.css"), "/* hljs */");
    writeFileSync(join(tmp, "node_modules/uplot/dist/uPlot.min.css"), "/* uplot */");
    writeFileSync(
      join(tmp, "package.json"),
      JSON.stringify({ name: "reasonix", version: "9.8.7" }),
    );
  });

  afterEach(() => {
    rmSync(tmp, { recursive: true, force: true });
  });

  it("writes package metadata for the bundled CLI", () => {
    const script = resolve("scripts/copy-dashboard-vendor-css.mjs");
    const run = spawnSync(process.execPath, [script], { cwd: tmp, encoding: "utf8" });

    expect(run.status).toBe(0);
    const marker = JSON.parse(readFileSync(join(tmp, "dist/cli/package.json"), "utf8"));
    expect(marker).toEqual({
      name: "reasonix",
      version: "9.8.7",
      type: "module",
    });
  });
});
