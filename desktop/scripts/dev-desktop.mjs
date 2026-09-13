#!/usr/bin/env node
import { spawn } from "node:child_process";
import { mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const desktopRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const pnpm = process.platform === "win32" ? "pnpm.cmd" : "pnpm";
const serviceName = process.platform === "win32"
  ? "reasonix-desktop-service.exe"
  : "reasonix-desktop-service";
const servicePath = resolve(desktopRoot, "build/bin", serviceName);
const port = process.env.REASONIX_DESKTOP_VITE_PORT || "5173";
const devUrl = process.env.REASONIX_ELECTRON_DEV_URL || `http://127.0.0.1:${port}`;

if (process.argv.includes("--help")) {
  console.log("Build the Reasonix service, start Vite, and launch the Electron development shell.");
  console.log("Usage: pnpm dev:desktop");
  process.exit(0);
}

function start(command, args, env = process.env) {
  return spawn(command, args, { cwd: desktopRoot, env, stdio: "inherit" });
}

function waitForExit(child, label) {
  return new Promise((resolveExit, rejectExit) => {
    child.once("error", rejectExit);
    child.once("exit", (code, signal) => {
      if (code === 0) resolveExit();
      else rejectExit(new Error(`${label} exited with ${code ?? signal ?? "an unknown error"}`));
    });
  });
}

async function servesReasonix(url) {
  try {
    const response = await fetch(url);
    if (!response.ok) return false;
    const html = await response.text();
    return /<title>\s*Reasonix\s*<\/title>/i.test(html);
  } catch {
    return false;
  }
}

async function waitForVite(frontend) {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    if (frontend.exitCode !== null || frontend.signalCode) {
      throw new Error(`Vite exited with ${frontend.exitCode ?? frontend.signalCode}`);
    }
    if (await servesReasonix(devUrl)) return;
    await new Promise(resolveWait => setTimeout(resolveWait, 150));
  }
  throw new Error(`Vite did not become ready at ${devUrl}`);
}

let frontend;
let electron;
let exiting = false;

function stopChildren() {
  if (electron?.exitCode === null) electron.kill("SIGTERM");
  if (frontend?.exitCode === null) frontend.kill("SIGTERM");
}

function fail(error) {
  console.error(error instanceof Error ? error.message : error);
  stopChildren();
  process.exitCode = 1;
}

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.once(signal, () => {
    exiting = true;
    stopChildren();
  });
}

try {
  await waitForExit(start(process.execPath, ["scripts/build-remote-cli.mjs"]), "Remote CLI build");
  await mkdir(dirname(servicePath), { recursive: true });
  console.log("==> Building Reasonix desktop service");
  await waitForExit(start("go", ["build", "-o", servicePath, "."]), "Go build");

  if (await servesReasonix(devUrl)) {
    console.log(`==> Reusing Reasonix Vite server at ${devUrl}`);
  } else {
    console.log(`==> Starting Reasonix Vite server at ${devUrl}`);
    frontend = start(pnpm, ["--dir", "frontend", "dev", "--host", "127.0.0.1", "--port", port, "--strictPort"]);
    await waitForVite(frontend);
  }

  console.log("==> Launching Reasonix Electron development shell");
  electron = start(pnpm, ["--dir", "electron", "dev"], {
    ...process.env,
    REASONIX_DESKTOP_SERVICE: servicePath,
    REASONIX_ELECTRON_DEV_URL: devUrl,
  });
  const electronExit = waitForExit(electron, "Electron");
  const frontendExit = frontend
    ? waitForExit(frontend, "Vite").then(() => { throw new Error("Vite stopped before Electron"); })
    : new Promise(() => {});
  await Promise.race([electronExit, frontendExit]);
  if (frontend?.exitCode === null) frontend.kill("SIGTERM");
} catch (error) {
  if (!exiting) fail(error);
}
