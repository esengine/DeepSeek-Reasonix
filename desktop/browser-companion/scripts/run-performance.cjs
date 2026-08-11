const { spawn } = require("node:child_process");
const path = require("node:path");

const electron = path.join(__dirname, "..", "node_modules", ".bin", "electron");
const child = spawn(electron, [path.join(__dirname, "performance-probe.cjs")], {
  stdio: ["ignore", "pipe", "pipe"],
  env: { ...process.env, REASONIX_PERF_STARTED_AT: String(Date.now()) },
});
let output = "";
let finished = false;
child.stdout.on("data", (chunk) => { output += chunk; });
child.stderr.on("data", (chunk) => {
  output += chunk;
  if (/PERF_(?:RESULT|FATAL):/.test(output)) child.kill("SIGKILL");
});

const timeout = setTimeout(() => {
  output += "\nPERF_FATAL: wrapper timeout\n";
  child.kill("SIGKILL");
}, 45000);

function finish() {
  if (finished) return;
  finished = true;
  clearTimeout(timeout);
  const match = output.match(/PERF_RESULT: (\{[^\n]+\})/);
  let result;
  try { result = match ? JSON.parse(match[1]) : null; } catch { result = null; }
  const budgets = {
    startupMs: Number(process.env.REASONIX_PERF_STARTUP_MS || 10000),
    idleCPUPercent: Number(process.env.REASONIX_PERF_IDLE_CPU_PERCENT || 20),
    oneTabRSSMiB: Number(process.env.REASONIX_PERF_ONE_TAB_RSS_MIB || 700),
    eightTabRSSMiB: Number(process.env.REASONIX_PERF_EIGHT_TAB_RSS_MIB || 1500),
    incrementalRSSMiB: Number(process.env.REASONIX_PERF_INCREMENTAL_RSS_MIB || 900),
  };
  const failures = [];
  if (!result) failures.push("missing PERF_RESULT");
  if (result) {
    for (const [key, limit] of Object.entries(budgets)) {
      if (!Number.isFinite(result[key]) || result[key] > limit) failures.push(`${key}=${result[key]} > ${limit}`);
    }
    if (result.liveTabs !== 8) failures.push(`liveTabs=${result.liveTabs} != 8`);
  }
  process.stderr.write(output);
  process.stderr.write(`PERF_BUDGETS: ${JSON.stringify(budgets)}\n`);
  process.stderr.write(failures.length === 0 ? "PERF_GATE: PASS\n" : `PERF_GATE: FAIL ${failures.join(", ")}\n`);
  process.exit(failures.length === 0 ? 0 : 1);
}

child.on("close", finish);
child.on("exit", finish);
child.on("error", (err) => { output += `\nPERF_FATAL: ${err.message}\n`; finish(); });
