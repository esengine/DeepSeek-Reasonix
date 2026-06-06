/** CLI runner for behavior-stability evaluation. */

import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";
import { loadDotenv } from "../../src/index.js";
import { ALL_SCENARIOS, API_SCENARIOS, LOCAL_SCENARIOS } from "./scenarios.js";
import { runScenarios } from "./harness.js";
import type { EvalScenario } from "./types.js";

loadDotenv();

interface CliArgs {
  local: boolean;
  api: boolean;
  outPath: string | null;
}

function parseArgs(argv: string[]): CliArgs {
  const out: CliArgs = { local: false, api: false, outPath: null };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--local") out.local = true;
    else if (a === "--api") out.api = true;
    else if (a === "--out") out.outPath = argv[++i] ?? null;
  }
  // Default to local-only when neither flag is provided.
  if (!out.local && !out.api) out.local = true;
  return out;
}

function pickScenarios(args: CliArgs): EvalScenario[] {
  const scenarios: EvalScenario[] = [];
  if (args.local) scenarios.push(...LOCAL_SCENARIOS);
  if (args.api) scenarios.push(...API_SCENARIOS);
  return scenarios;
}

function isMain(): boolean {
  const entry = process.argv[1];
  if (!entry) return false;
  return import.meta.url === pathToFileURL(entry).href;
}

async function main(): Promise<void> {
  const args = parseArgs(process.argv.slice(2));
  const scenarios = pickScenarios(args);

  if (scenarios.length === 0) {
    console.log("No scenarios selected. Use --local, --api, or both.");
    process.exit(0);
  }

  const categories = new Set(scenarios.map((s) => s.category));
  console.log(`Running ${scenarios.length} scenario(s) [${[...categories].join(", ")}] …\n`);

  const report = await runScenarios(scenarios, {
    onResult: (r) => {
      const icon = r.pass ? "✅" : "❌";
      console.log(`${icon} ${r.scenarioId} (${r.durationMs}ms)`);
      console.log(`   ${r.details}\n`);
    },
  });

  const passed = report.results.filter((r) => r.pass).length;
  const total = report.results.length;
  console.log(`────────────────────────────────────────`);
  console.log(`Results: ${passed}/${total} passed`);
  console.log(`Version: ${report.meta.reasonixVersion}`);
  console.log(`Date:    ${report.meta.date}`);

  if (args.outPath) {
    const dir = dirname(args.outPath);
    if (dir !== ".") mkdirSync(dir, { recursive: true });
    writeFileSync(args.outPath, `${JSON.stringify(report, null, 2)}\n`, "utf8");
    console.log(`Wrote ${args.outPath}`);
  }

  if (passed < total) process.exit(1);
}

if (isMain()) {
  main().catch((err) => {
    console.error(err);
    process.exit(1);
  });
}
