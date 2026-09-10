#!/usr/bin/env node
// Launches the real shell against a built Go service in a disposable data
// home, proves the handshake, the renderer bridge, a business command and a
// clean exit, and writes the evidence to artifacts/smoke/. No mock is
// involved: a missing service or a failed hello fails the run.
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const { _electron: electron } = require("playwright");

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const artifacts = resolve(root, "artifacts", "smoke");
mkdirSync(artifacts, { recursive: true });
const service = process.env.REASONIX_DESKTOP_SERVICE
  || resolve(root, "../build/bin", process.platform === "win32" ? "reasonix-desktop-service.exe" : "reasonix-desktop-service");
if (!existsSync(service)) {
  console.error(`desktop service binary not found: ${service}`);
  process.exit(2);
}
if (!existsSync(resolve(root, "dist/main.cjs"))) {
  console.error("shell not built: run pnpm build first");
  process.exit(2);
}

const home = mkdtempSync(join(tmpdir(), "reasonix-electron-smoke-"));
const checks = [];
const check = (name, ok, detail = "") => {
  checks.push({ name, ok: Boolean(ok), detail });
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? `  (${detail})` : ""}`);
};

function processTree(pid) {
  if (process.platform === "win32") return [];
  const rows = execFileSync("ps", ["-axo", "pid=,ppid=,comm="], { encoding: "utf8" }).split("\n");
  const children = new Map();
  for (const row of rows) {
    const [p, pp, ...comm] = row.trim().split(/\s+/);
    if (!p) continue;
    const list = children.get(Number(pp)) ?? [];
    list.push({ pid: Number(p), comm: comm.join(" ") });
    children.set(Number(pp), list);
  }
  const out = [];
  const walk = (parent) => {
    for (const child of children.get(parent) ?? []) {
      out.push(child);
      walk(child.pid);
    }
  };
  walk(pid);
  return out;
}

function alive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

const started = Date.now();
const app = await electron.launch({
  args: [root],
  env: {
    ...process.env,
    REASONIX_HOME: home,
    REASONIX_STATE_HOME: home,
    REASONIX_CACHE_HOME: join(home, "cache"),
    REASONIX_DEV: "1",
    REASONIX_DESKTOP_SERVICE: service,
  },
  timeout: 60_000,
});
const shellPid = app.process().pid;
let exitCode = 1;
try {
  const page = await app.firstWindow({ timeout: 60_000 });
  await page.waitForFunction(() => Boolean(window.reasonixDesktop), null, { timeout: 30_000 });
  const contract = await page.evaluate(() => ({
    digest: window.reasonixDesktop.contract.digest,
    commands: window.reasonixDesktop.contract.commands.length,
    protocolVersion: window.reasonixDesktop.contract.protocolVersion,
  }));
  check("preload exposes the contract", contract.digest.startsWith("sha256:") && contract.commands > 500, `${contract.commands} commands, ${contract.digest.slice(0, 19)}`);

  await page.waitForFunction(() => !document.querySelector(".boot-shell"), null, { timeout: 60_000 });
  const readyMs = Date.now() - started;
  check("React replaced the boot shell", true, `${readyMs} ms after launch`);

  const state = await page.evaluate(() => new Promise((resolveState) => {
    const off = window.reasonixDesktop.native.onServiceState((s) => {
      if (s.phase === "ready" || s.phase === "failed" || s.phase === "exited") {
        queueMicrotask(() => { off(); resolveState(s); });
      }
    });
    setTimeout(() => resolveState({ phase: "timeout" }), 10_000);
  }));
  check("late subscribers receive the service state", state.phase === "ready", `phase=${state.phase} generation=${state.generation ?? ""}`);

  const version = await page.evaluate(() => window.reasonixDesktop.invoke("Version", []));
  const platform = await page.evaluate(() => window.reasonixDesktop.invoke("Platform", []));
  check("desktop/invoke round-trips business commands", typeof version === "string" && typeof platform === "string", `Version=${version} Platform=${platform}`);

  const unknown = await page.evaluate(() => window.reasonixDesktop.invoke("NoSuchCommand", []).then(() => "resolved", (e) => String(e.message)));
  check("unknown commands are rejected before reaching Go", unknown.includes("-32601"), unknown);

  const shellStatus = await page.evaluate(() => window.reasonixDesktop.invoke("GetDesktopShellStatus", []));
  check("shell status is served by Go", shellStatus && typeof shellStatus === "object", JSON.stringify(shellStatus).slice(0, 120));

  const bounds = await page.evaluate(() => window.reasonixDesktop.native.window.getBounds());
  check("native window bounds are readable", bounds.width >= 760 && bounds.height >= 480, `${bounds.width}x${bounds.height} at ${bounds.x},${bounds.y}`);

  const leaked = await page.evaluate(() => ({ go: typeof window.go, runtime: typeof window.runtime, require: typeof window.require, process: typeof window.process }));
  check("renderer has no Wails or Node globals", Object.values(leaked).every((t) => t === "undefined"), JSON.stringify(leaked));

  const errors = await page.evaluate(() => document.querySelector(".error-boundary, [data-crash-overlay]") !== null);
  check("no crash overlay is showing", !errors);

  const tab = await page.evaluate(() => window.reasonixDesktop.browser.open("example.com", { temporary: true }));
  check("browser opens a website view", typeof tab.id === "string" && new URL(tab.url).origin === "https://example.com", `${tab.id} ${tab.url}`);
  const title = await page.evaluate(async (tabId) => {
    for (let attempt = 0; attempt < 50; attempt += 1) {
      const tabs = await window.reasonixDesktop.browser.list();
      const found = tabs.find((entry) => entry.id === tabId);
      if (found && found.title !== "") return found.title;
      await new Promise((resolveWait) => setTimeout(resolveWait, 200));
    }
    return "";
  }, tab.id);
  check("the website view loads example.com", title === "Example Domain", `title=${JSON.stringify(title)}`);
  await page.evaluate((tabId) => window.reasonixDesktop.browser.close(tabId), tab.id);
  const remaining = await page.evaluate(() => window.reasonixDesktop.browser.list());
  check("browser tab closes cleanly", remaining.every((entry) => entry.id !== tab.id), `${remaining.length} tabs left`);

  await page.screenshot({ path: join(artifacts, "main-window.png") });
  const tree = processTree(shellPid);
  const servicePids = tree.filter((p) => p.comm.includes("reasonix-desktop")).map((p) => p.pid);
  check("Go service runs as a child of the shell", servicePids.length === 1, `pids=${servicePids.join(",")} tree=${tree.length}`);

  await app.close();
  await new Promise((r) => setTimeout(r, 1500));
  check("shell exited", !alive(shellPid));
  check("Go service exited with the shell", servicePids.every((pid) => !alive(pid)));
  exitCode = checks.every((c) => c.ok) ? 0 : 1;
} catch (error) {
  check("smoke run completed", false, String(error?.message ?? error));
  try {
    await app.close();
  } catch {
    // the shell may already be gone
  }
} finally {
  const shellLog = join(home, "desktop-shell", "logs", "shell.log");
  if (existsSync(shellLog)) writeFileSync(join(artifacts, "shell.log"), readFileSync(shellLog));
  const serviceLog = join(home, "desktop-shell", "logs", "service.log");
  if (existsSync(serviceLog)) writeFileSync(join(artifacts, "service.log"), readFileSync(serviceLog));
  writeFileSync(join(artifacts, "smoke.json"), JSON.stringify({ at: new Date().toISOString(), platform: process.platform, arch: process.arch, electron: require("electron/package.json").version, checks }, null, 2) + "\n");
  rmSync(home, { recursive: true, force: true });
}
process.exit(exitCode);
