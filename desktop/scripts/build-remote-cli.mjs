#!/usr/bin/env node
import { spawn } from "node:child_process";
import { mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const targets = process.argv.slice(2);
if (targets.length === 0) {
  targets.push(...(process.env.REASONIX_DEV_REMOTE_TARGETS || "linux/amd64").split(","));
}
for (const target of targets) {
  if (!/^(linux|darwin)\/(amd64|arm64)$/.test(target)) {
    throw new Error(`Unsupported remote CLI target: ${target}`);
  }
}
for (const target of targets) {
  const [goos, goarch] = target.split("/");
  const output = resolve(root, "desktop/build/bin/remote-cli", `${goos}-${goarch}`, "reasonix");
  await mkdir(dirname(output), { recursive: true });
  console.log(`==> Building development remote CLI for ${target}`);
  await new Promise((resolveExit, rejectExit) => {
    const child = spawn("go", ["build", "-o", output, "./cmd/reasonix"], {
      cwd: root,
      env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: "0" },
      stdio: "inherit",
    });
    child.once("error", rejectExit);
    child.once("exit", (code, signal) => {
      if (code === 0) resolveExit();
      else rejectExit(new Error(`Remote CLI build failed: ${code ?? signal}`));
    });
  });
}
