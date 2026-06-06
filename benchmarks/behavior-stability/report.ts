/** Render behavior-stability results.json → markdown report. */

import { readFileSync, writeFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import type { EvalReport } from "./types.js";

interface CliArgs {
  input: string;
  outPath: string;
}

function parseArgs(argv: string[]): CliArgs {
  const out: CliArgs = { input: "", outPath: "benchmarks/behavior-stability/report.md" };
  const positional: string[] = [];
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--out") out.outPath = argv[++i] ?? out.outPath;
    else if (a && !a.startsWith("--")) positional.push(a);
  }
  out.input = positional[0] ?? "";
  if (!out.input) {
    throw new Error(
      "usage: npx tsx benchmarks/behavior-stability/report.ts <results.json> [--out report.md]",
    );
  }
  return out;
}

function renderHeader(report: EvalReport): string {
  return `# Reasonix Behavior-Stability Evaluation

**Date:** ${report.meta.date}
**Version:** ${report.meta.reasonixVersion}
`;
}

function renderSummary(report: EvalReport): string {
  const passed = report.results.filter((r) => r.pass).length;
  const total = report.results.length;
  const pct = total > 0 ? ((passed / total) * 100).toFixed(0) : "0";

  const rows = report.results.map((r) => {
    const icon = r.pass ? "✅" : "❌";
    return `| ${icon} | \`${r.scenarioId}\` | ${r.durationMs}ms | ${r.details} |`;
  });

  return `## Summary

**${passed}/${total} passed (${pct}%)**

| status | scenario | time | details |
|---|---:|---:|:---|
${rows.join("\n")}
`;
}

function render(report: EvalReport): string {
  return [renderHeader(report), renderSummary(report)].join("\n\n");
}

function isMain(): boolean {
  const entry = process.argv[1];
  if (!entry) return false;
  return import.meta.url === pathToFileURL(entry).href;
}

async function main(): Promise<void> {
  const args = parseArgs(process.argv.slice(2));
  const raw = readFileSync(args.input, "utf8");
  const report = JSON.parse(raw) as EvalReport;
  const md = render(report);
  writeFileSync(args.outPath, md, "utf8");
  console.log(`Wrote ${args.outPath} (${report.results.length} results)`);
}

if (isMain()) {
  main().catch((err) => {
    console.error(err);
    process.exit(1);
  });
}
