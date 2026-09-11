#!/usr/bin/env node
// Launches a packaged shell against a disposable data home and proves the
// desktop/hello handshake reached ready, then uses Electron's normal app.quit
// lifecycle through Playwright and checks both Electron and Go are gone.
//
// usage: node desktop/packaging/smoke.mjs <Reasonix.app|app-dir|executable>
//        [--service <reasonix-desktop path>] [--hold <seconds>] [--timeout <seconds>] [--keep-home]
import { spawnSync } from "node:child_process";
import { appendFileSync, existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { basename, join, resolve } from "node:path";
import { isDirectory, PRODUCT } from "./lib.mjs";
import { closeAndVerify, processAlive, sleep, waitForProcessesToExit } from "./smoke-lifecycle.mjs";
import { packagedSmokeEnv } from "./smoke-env.mjs";

// Playwright belongs to the Electron workspace, not the shipped application.
const require = createRequire(new URL("../electron/package.json", import.meta.url));
const { _electron: electron } = require("playwright");

const args = process.argv.slice(2);
const option = (name, fallback) => {
  const index = args.indexOf(name);
  return index >= 0 ? args[index + 1] : fallback;
};
const targetArg = args.find((arg, index) => !arg.startsWith("--") && (index === 0 || !args[index - 1].startsWith("--") || args[index - 1] === "--keep-home"));
if (!targetArg) {
  console.error("usage: smoke.mjs <Reasonix.app|app-dir|executable> [--service <path>] [--hold <seconds>] [--timeout <seconds>] [--keep-home]");
  process.exit(2);
}
const hold = Number(option("--hold", "5")) * 1000;
const timeout = Number(option("--timeout", "60")) * 1000;
const service = option("--service", "");
const keepHome = args.includes("--keep-home");

function executableOf(path) {
  const full = resolve(path);
  if (!isDirectory(full)) return full;
  if (basename(full).endsWith(".app")) return join(full, "Contents", "MacOS", PRODUCT.executable);
  for (const name of [`${PRODUCT.executable}.exe`, PRODUCT.executable]) {
    if (existsSync(join(full, name))) return join(full, name);
  }
  throw new Error(`no ${PRODUCT.executable} executable inside ${full}`);
}

const executable = executableOf(targetArg);
if (!existsSync(executable)) throw new Error(`shell executable is missing: ${executable}`);
const home = mkdtempSync(join(tmpdir(), "reasonix-smoke-"));
const logs = join(home, "desktop-shell", "logs");
const env = packagedSmokeEnv(process.env, home);
if (service !== "") env.REASONIX_DESKTOP_SERVICE = resolve(service);
const stdio = join(home, "smoke-stdio.log");
const started = Date.now();
let child;
let shellPid;
let exit = null;
let ready = null;
const captureOutput = (data) => appendFileSync(stdio, data);

const readLog = (name) => {
  try {
    return readFileSync(join(logs, name), "utf8");
  } catch {
    return "";
  }
};
const tail = (name, lines = 40) => readLog(name).trimEnd().split("\n").slice(-lines).join("\n");
async function cleanupAfterFailure() {
  const pids = [...new Set([child?.pid, shellPid, ready?.pid].filter(Number.isInteger))];
  for (const pid of pids) {
    if (!processAlive(pid)) continue;
    if (process.platform === "win32") {
      const result = spawnSync("taskkill", ["/PID", String(pid), "/T", "/F"], { encoding: "utf8" });
      if (result.error || (result.status !== 0 && processAlive(pid))) {
        throw new Error(`failed cleanup of pid ${pid}: ${result.error ?? result.stderr ?? result.status}`);
      }
    } else {
      process.kill(pid, "SIGKILL");
    }
  }
  await waitForProcessesToExit(pids);
}

try {
  const application = await electron.launch({ executablePath: executable, args: [], env, timeout });
  child = application.process();
  child.on("exit", (code, signal) => { exit = { code, signal }; });
  child.stdout?.on("data", captureOutput);
  child.stderr?.on("data", captureOutput);
  // On Windows Playwright may own a cmd.exe wrapper. Read the Electron main
  // PID itself so a wrapper exit cannot pass the shell-liveness assertion.
  shellPid = await application.evaluate(() => process.pid);
  const identity = await application.evaluate(({ app }) => ({ packaged: app.isPackaged, dev: process.env.REASONIX_DEV ?? "", resourcesPath: process.resourcesPath }));
  if (!identity.packaged || identity.dev !== "") throw new Error("startup smoke must exercise a packaged app without development mode");
  while (!ready) {
    if (exit || !processAlive(shellPid)) throw new Error("shell exited before the handshake");
    if (Date.now() - started > timeout) throw new Error(`no handshake within ${timeout / 1000}s`);
    const log = readLog("shell.log");
    const failed = /desktop service failed: .*/.exec(log);
    if (failed) throw new Error(failed[0]);
    const line = /desktop service ready: generation (\S+), pid (\d+)/.exec(log);
    if (line) ready = { generation: line[1], pid: Number(line[2]), line: line[0] };
    else await sleep(250);
  }
  console.log(`PASS  handshake ready after ${((Date.now() - started) / 1000).toFixed(1)}s: ${ready.line}`);
  const page = await application.firstWindow({ timeout });
  await page.waitForFunction(() => Boolean(window.reasonixDesktop), null, { timeout });
  const version = await page.evaluate(() => window.reasonixDesktop.invoke("Version", []));
  const expected = JSON.parse(readFileSync(join(identity.resourcesPath, "build.json"), "utf8")).version;
  if (version === "dev" || version !== expected) throw new Error(`packaged service version ${version} differs from manifest ${expected}`);
  console.log(`PASS  renderer invokes the production service: Version=${version}`);
  await sleep(hold);
  if (exit || !processAlive(shellPid)) throw new Error(`shell exited during the ${hold / 1000}s hold`);
  if (!processAlive(ready.pid)) throw new Error(`Go service pid ${ready.pid} exited during the hold`);
  console.log(`PASS  shell pid ${shellPid} and Go service pid ${ready.pid} still running after ${hold / 1000}s hold`);

  await closeAndVerify(application, { shellPid, servicePid: ready.pid });
  if (exit?.signal || (exit?.code != null && exit.code !== 0)) {
    throw new Error(`normal app quit failed (code ${exit.code}, signal ${exit.signal})`);
  }
  console.log(`PASS  normal app quit completed; shell pid ${shellPid} exited`);
  console.log(`PASS  Go service pid ${ready.pid} exited with the shell`);
} catch (error) {
  process.exitCode = 1;
  console.error(`FAIL  ${error.message}`);
  console.error(`--- shell.log ---\n${tail("shell.log")}\n--- service.log ---\n${tail("service.log")}\n--- stdio ---\n${tail("../../smoke-stdio.log")}`);
  try { await cleanupAfterFailure(); } catch (cleanupError) { console.error(`FAIL  cleanup: ${cleanupError.message}`); }
} finally {
  child?.stdout?.off("data", captureOutput);
  child?.stderr?.off("data", captureOutput);
  if (keepHome || process.exitCode) console.log(`home kept at ${home}`);
  else rmSync(home, { recursive: true, force: true });
}
