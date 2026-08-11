// Build a normalized component tree from an official Electron distribution.
// The Electron executables are copied byte-for-byte (preserving upstream
// Authenticode on Windows); only Resources/app is replaced with Reasonix code.

import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const args = process.argv.slice(2).filter((arg) => arg !== "--");
if (args.length !== 2) throw new Error("usage: package-component.mjs <electron-dist> <output-browser-component>");
const source = path.resolve(args[0] ?? "");
const output = path.resolve(args[1] ?? "");
if (!source || !fs.statSync(source).isDirectory()) throw new Error("Electron distribution directory is required");
if (path.basename(output) !== "browser-component") throw new Error("output must end in browser-component");

const here = path.dirname(fileURLToPath(import.meta.url));
const project = path.resolve(here, "..");
const pkg = JSON.parse(fs.readFileSync(path.join(project, "package.json"), "utf8"));
const version = pkg.version;
const browser = path.join(output, version, "browser");
fs.rmSync(output, { recursive: true, force: true });
fs.mkdirSync(browser, { recursive: true });

if (process.platform === "darwin") {
  const sourceApp = path.join(source, "Electron.app");
  const targetApp = path.join(browser, "Reasonix Browser.app");
  fs.cpSync(sourceApp, targetApp, { recursive: true, verbatimSymlinks: true });
  installApp(path.join(targetApp, "Contents", "Resources"));
} else {
  fs.cpSync(source, browser, { recursive: true, verbatimSymlinks: true });
  const original = path.join(browser, process.platform === "win32" ? "electron.exe" : "electron");
  const executable = path.join(browser, process.platform === "win32" ? "reasonix-browser-companion.exe" : "reasonix-browser-companion");
  fs.renameSync(original, executable);
  if (process.platform !== "win32") fs.chmodSync(executable, 0o755);
  installApp(path.join(browser, "resources"));
}

fs.writeFileSync(path.join(output, "current.json"), JSON.stringify({ version }, null, 2) + "\n");
fs.writeFileSync(path.join(output, version, "component.json"), JSON.stringify({
  format: "reasonix.browser.component.v1",
  version,
  electronVersion: pkg.devDependencies.electron,
  protocolVersion: 1,
}, null, 2) + "\n");

function installApp(resources) {
  fs.rmSync(path.join(resources, "default_app.asar"), { force: true });
  const app = path.join(resources, "app");
  fs.rmSync(app, { recursive: true, force: true });
  fs.mkdirSync(app, { recursive: true });
  fs.cpSync(path.join(project, "dist"), path.join(app, "dist"), { recursive: true });
  fs.copyFileSync(path.join(project, "package.json"), path.join(app, "package.json"));
}
