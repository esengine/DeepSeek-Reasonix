import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import { closeSync, mkdtempSync, openSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { discoveredTests, testPlan } from "./ci-test-plan.mjs";

process.chdir(path.resolve(path.dirname(fileURLToPath(import.meta.url)), ".."));
const scripts = JSON.parse(readFileSync("package.json", "utf8")).scripts;
const plan = testPlan(scripts, discoveredTests());
if (process.argv.includes("--list")) {
  console.log(JSON.stringify(plan, null, 2));
} else {
  const concurrency = Number(process.env.REASONIX_TEST_CONCURRENCY ?? 2);
  if (!Number.isInteger(concurrency) || concurrency < 1 || concurrency > 4) throw new Error("test concurrency must be 1 to 4");
  const require = createRequire(import.meta.url);
  const commands = { tsx: require.resolve("tsx/cli"), tsc: require.resolve("typescript/bin/tsc") };
  const logs = mkdtempSync(path.join(tmpdir(), "reasonix-ci-tests-"));
  let failed = false;
  let completed = 0;
  const children = new Set();
  for (const signal of ["SIGTERM", "SIGINT"]) process.on(signal, () => {
    failed = true;
    for (const child of children) child.kill(signal);
    process.exitCode = 1;
  });
  async function run(entry) {
    const started = performance.now();
    const log = path.join(logs, `${plan.indexOf(entry)}.log`);
    const fd = openSync(log, "w");
    const [command, ...args] = entry.args;
    const result = await new Promise(resolve => {
      const child = spawn(process.execPath, command === "node" ? args : [commands[command], ...args], {
        stdio: ["ignore", fd, fd], timeout: 10 * 60 * 1000,
        env: { ...process.env, LANG: "en_US.UTF-8", LC_ALL: "en_US.UTF-8" },
      });
      children.add(child);
      child.once("error", error => resolve({ error }));
      child.once("close", (code, signal) => { children.delete(child); resolve({ code, signal }); });
    });
    closeSync(fd);
    completed++;
    const passed = result.code === 0 && !result.error;
    console.log(`${passed ? "PASS" : "FAIL"} ${entry.key} (${Math.round(performance.now() - started)}ms)`);
    if (!passed) {
      failed = true;
      console.error(result.error ?? result.signal ?? `exit ${result.code}`);
      console.error(readFileSync(log, "utf8"));
    }
  }
  try {
    const queue = plan.filter(entry => !entry.serial);
    console.log(`CI: ${plan.length} unique commands; concurrency=${concurrency}; performance benchmark runs alone`);
    await Promise.all(Array.from({ length: concurrency }, async () => {
      while (queue.length && !failed) await run(queue.shift());
    }));
    for (const entry of plan.filter(entry => entry.serial)) if (!failed) await run(entry);
  } finally {
    rmSync(logs, { recursive: true, force: true });
  }
  console.log(`CI: ${completed}/${plan.length} commands completed`);
  if (failed || completed !== plan.length) process.exitCode = 1;
}
