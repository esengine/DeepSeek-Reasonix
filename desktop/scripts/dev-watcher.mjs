#!/usr/bin/env node

/**
 * dev-watcher.mjs — Wails dev frontend watcher
 *
 * Kills any stale process on the Vite dev port (5173) before starting pnpm dev.
 * Solves the "Port 5173 is already in use" error when a previous `wails dev`
 * session exited without cleaning up the Vite process.
 *
 * This is the `frontend:dev:watcher` entry point in wails.json.
 * It spawns pnpm dev in the same process group so signals propagate correctly.
 */

import { spawn } from "node:child_process";
import { createConnection } from "node:net";
import { cwd, chdir, exit } from "node:process";

const DEV_PORT = 5173;

chdir(new URL("../frontend", import.meta.url).pathname);

try {
  await killPort(DEV_PORT);
} catch {
  // Kill is best-effort; on a fresh start the port is already free, and
  // socket errors on Windows or CI are harmless.
}

const child = spawn("pnpm", ["dev"], {
  stdio: "inherit",
  shell: true,
  env: { ...process.env },
});

// Forward signals so a terminating wails dev cleans up Vite with us.
for (const sig of ["SIGINT", "SIGTERM", "SIGHUP"]) {
  process.on(sig, () => {
    child.kill(sig);
    // Give Vite a moment to release the port, then exit ourselves.
    setTimeout(() => exit(0), 500);
  });
}

child.on("exit", (code) => exit(code ?? 0));

// --- helpers -----------------------------------------------------------------

/**
 * Check if a TCP port is open and, if so, kill the owning process.
 * Uses the OS-native tool (lsof on macOS/Linux, netstat on Windows).
 */
async function killPort(port) {
  if (await isPortInUse(port)) {
    const { execSync } = await import("node:child_process");
    const platform = process.platform;

    if (platform === "win32") {
      execSync(
        `for /f "tokens=5" %p in ('netstat -ano ^| findstr :${port}') do taskkill /F /PID %p 2>nul`,
        { stdio: "ignore" }
      );
    } else {
      execSync(`lsof -ti:${port} | xargs kill -9 2>/dev/null`, {
        stdio: "ignore",
      });
    }
  }
}

/**
 * Probe whether a local TCP port is already in use by attempting a connection.
 */
function isPortInUse(port) {
  return new Promise((resolve) => {
    const sock = createConnection(
      { host: "127.0.0.1", port, allowHalfOpen: false },
      () => {
        sock.destroy();
        resolve(true);
      }
    );
    sock.on("error", () => resolve(false));
    sock.setTimeout(1000, () => {
      sock.destroy();
      resolve(false);
    });
  });
}
