#!/usr/bin/env node
// Verify, notarize and staple an already-signed app or disk image.
import { spawnSync } from "node:child_process";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

export function notarizeDesktop({ archive, target, kind, diagnosticsDir, env = process.env,
  run = (command, args) => spawnSync(command, args, {
    encoding: "utf8", stdio: ["ignore", "pipe", "inherit"], maxBuffer: 16 * 1024 * 1024,
  }), warn = console.warn }) {
  if (!["app", "dmg"].includes(kind)) throw new Error("Expected notarization kind app or dmg");
  for (const name of ["APPLE_API_KEY_PATH", "APPLE_API_KEY_ID", "APPLE_API_ISSUER_ID"]) {
    if (!env[name]) throw new Error(`Missing ${name}`);
  }
  mkdirSync(diagnosticsDir, { recursive: true });
  for (const suffix of ["submission", "notary-log"]) {
    rmSync(join(diagnosticsDir, `${kind}-${suffix}.json`), { force: true });
  }
  const auth = ["--key", env.APPLE_API_KEY_PATH, "--key-id", env.APPLE_API_KEY_ID,
    "--issuer", env.APPLE_API_ISSUER_ID];
  const checked = (command, args) => {
    const result = run(command, args);
    if (result.stdout) process.stdout.write(result.stdout);
    if (result.status !== 0 || result.error) throw new Error(`${command} ${args[0]} failed`);
    return result;
  };
  checked("codesign", ["--verify", ...(kind === "app" ? ["--deep"] : []), "--strict", "--verbose=4", target]);
  console.log(`==> notarytool submit (${kind})`);
  const submission = run("xcrun", ["notarytool", "submit", archive, ...auth, "--wait", "--output-format", "json"]);
  let response;
  try { response = JSON.parse(submission.stdout); } catch { /* Fail closed below. */ }
  const id = typeof response?.id === "string" && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(response.id)
    ? response.id : null;
  const status = typeof response?.status === "string" ? response.status : null;
  // Do not archive command arguments, credentials or machine-local upload paths.
  writeFileSync(join(diagnosticsDir, `${kind}-submission.json`), JSON.stringify({
    id, status, exitCode: submission.status, signal: submission.signal ?? null,
  }, null, 2) + "\n");
  console.log(`==> notarization (${kind}): ${status ?? "unknown"}; submission: ${id ?? "unavailable"}`);
  if (id) {
    const log = run("xcrun", ["notarytool", "log", id, ...auth]);
    if (log.status === 0 && !log.error) {
      try {
        const report = JSON.parse(log.stdout);
        writeFileSync(join(diagnosticsDir, `${kind}-notary-log.json`), JSON.stringify(report, null, 2) + "\n");
      } catch {
        warn(`Could not decode notarization log for ${id}; retrieve it with notarytool log.`);
      }
    } else {
      warn(`Could not fetch notarization log for ${id}; retrieve it with notarytool log.`);
    }
  }
  // notarytool can exit successfully after Apple rejects the submission.
  if (submission.status !== 0 || submission.error || !id || status !== "Accepted") {
    throw new Error(`Notarization ${status ?? "unknown"} (${id ?? "no submission ID"}); see ${kind}-submission.json and the notary log`);
  }
  checked("xcrun", ["stapler", "staple", target]);
  checked("xcrun", ["stapler", "validate", target]);
  // Gatekeeper requires notarization; it is not a pre-submission signature test.
  checked("spctl", ["--assess", "--verbose=4", "--type", kind === "app" ? "exec" : "open",
    ...(kind === "dmg" ? ["--context", "context:primary-signature"] : []), target]);
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const [archive, target, kind, diagnosticsDir] = process.argv.slice(2);
  if (!archive || !target || !kind || !diagnosticsDir) {
    console.error("usage: notarize-desktop.mjs <archive> <target> <app|dmg> <diagnostics-directory>");
    process.exitCode = 2;
  } else {
    try { notarizeDesktop({ archive, target, kind, diagnosticsDir }); }
    catch (error) { console.error(error.message); process.exitCode = 1; }
  }
}
