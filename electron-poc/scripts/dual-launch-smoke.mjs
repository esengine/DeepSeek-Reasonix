#!/usr/bin/env node
/**
 * Dual launch smoke for Electron PoC supervision path (without requiring a display).
 * Starts serve twice serially via ServeSupervisor (same code path as Electron main),
 * asserts GET /status on discovered loopback URL with token.
 * If Electron is available and REASONIX_POC_ELECTRON=1, also spawns electron briefly.
 */
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { spawn, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { ServeSupervisor } from "../lib/serveSupervisor.mjs";
import { resolveReasonixBin } from "../lib/resolveReasonixBin.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = path.resolve(__dirname, "..");
const scratch = process.env.REASONIX_POC_SCRATCH || process.env.SCRATCH || "";

function write(name, body) {
  const text = typeof body === "string" ? body : JSON.stringify(body, null, 2);
  if (scratch) {
    fs.mkdirSync(scratch, { recursive: true });
    const p = path.join(scratch, name);
    fs.writeFileSync(p, text.endsWith("\n") ? text : text + "\n");
    process.stdout.write(`wrote ${p}\n`);
  } else {
    process.stdout.write(text + (text.endsWith("\n") ? "" : "\n"));
  }
}

async function oneLaunch(label) {
  const lines = [`=== ${label} ===`];
  let bin;
  try {
    bin = resolveReasonixBin({
      env: process.env,
      repoRoot: path.resolve(packageRoot, ".."),
    });
  } catch (e) {
    lines.push(`UNAVAILABLE: ${e}`);
    return { ok: false, unavailable: true, lines };
  }
  const stateDir = fs.mkdtempSync(path.join(os.tmpdir(), `rx-launch-${label}-`));
  const sup = new ServeSupervisor({
    bin,
    stateDir,
    workspace: process.env.REASONIX_WORKSPACE || process.cwd(),
    crashRestartOnce: false,
  });
  try {
    const info = await sup.start();
    lines.push(`baseUrl=${info.baseUrl}`);
    lines.push(`uiUrl=${info.uiUrl.replace(info.token, "<redacted>")}`);
    lines.push(`loopback=${info.baseUrl.startsWith("http://127.0.0.1:")}`);
    lines.push(`token_on_argv=${info.args.some((a) => a.includes(info.token))}`);
    lines.push(`has_token_file_flag=${info.args.includes("--token-file")}`);

    const statusUrl = `${info.baseUrl}/status?token=${encodeURIComponent(info.token)}`;
    const res = await fetch(statusUrl, {
      headers: { Cookie: `reasonix_token=${info.token}` },
    });
    const body = await res.text();
    lines.push(`GET /status status=${res.status}`);
    lines.push(`GET /status body=${body.slice(0, 400)}`);
    const ok = res.ok && body.length > 0;
    lines.push(`RESULT=${ok ? "OK" : "FAIL"}`);
    return { ok, unavailable: false, lines, info };
  } catch (e) {
    lines.push(`RESULT=FAIL ${e instanceof Error ? e.stack || e.message : e}`);
    return { ok: false, unavailable: false, lines };
  } finally {
    await sup.stop();
    try {
      fs.rmSync(stateDir, { recursive: true, force: true });
    } catch {
      /* ignore */
    }
  }
}

async function tryElectronOnce() {
  const lines = ["=== electron-spawn ==="];
  const electronPkg = path.join(packageRoot, "node_modules", "electron");
  if (!fs.existsSync(electronPkg)) {
    lines.push("UNAVAILABLE: electron package not installed (run pnpm/npm install in electron-poc)");
    return { ok: false, unavailable: true, lines };
  }
  // Headless environments often cannot open a window; still try with short timeout.
  const env = {
    ...process.env,
    ELECTRON_ENABLE_LOGGING: "1",
  };
  // Prefer a dry path: use ServeSupervisor only when DISPLAY missing and skip electron UI
  if (!process.env.DISPLAY && process.platform === "linux" && process.env.REASONIX_POC_ELECTRON !== "force") {
    lines.push("UNAVAILABLE: no DISPLAY for Electron UI on Linux");
    return { ok: false, unavailable: true, lines };
  }
  const child = spawn(
    process.platform === "win32" ? "npx.cmd" : "npx",
    ["electron", packageRoot],
    {
      cwd: packageRoot,
      env,
      stdio: ["ignore", "pipe", "pipe"],
    },
  );
  let out = "";
  child.stdout.on("data", (d) => (out += d));
  child.stderr.on("data", (d) => (out += d));
  const result = await new Promise((resolve) => {
    const t = setTimeout(() => {
      try {
        child.kill("SIGTERM");
      } catch {
        /* ignore */
      }
      setTimeout(() => {
        try {
          child.kill("SIGKILL");
        } catch {
          /* ignore */
        }
        resolve({ timedOut: true });
      }, 2000);
    }, 8000);
    child.on("exit", (code, signal) => {
      clearTimeout(t);
      resolve({ code, signal, timedOut: false });
    });
  });
  lines.push(out.slice(0, 2000));
  lines.push(`exit=${JSON.stringify(result)}`);
  // If serve became ready, main logs "serve ready"
  const ready = out.includes("serve ready") || out.includes("reasonix serve");
  lines.push(`RESULT=${ready ? "OK" : result.timedOut ? "TIMEOUT_PARTIAL" : "FAIL"}`);
  return { ok: ready || result.timedOut, unavailable: false, lines };
}

async function main() {
  const a = await oneLaunch("launch-1");
  write("electron-serve-launch-1.log", a.lines.join("\n"));
  const b = await oneLaunch("launch-2");
  write("electron-serve-launch-2.log", b.lines.join("\n"));

  if (a.unavailable && b.unavailable) {
    write(
      "electron-serve-launch-unavailable.log",
      ["reasonix binary unavailable for dual launch", ...a.lines, ...b.lines].join("\n"),
    );
    process.exitCode = 0;
    return;
  }

  if (process.env.REASONIX_POC_ELECTRON === "1") {
    const el = await tryElectronOnce();
    write(
      el.unavailable ? "electron-serve-launch-unavailable.log" : "electron-ui-spawn.log",
      el.lines.join("\n"),
    );
  }

  if (!a.ok || !b.ok) process.exitCode = 1;
}

main();
