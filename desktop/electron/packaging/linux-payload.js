"use strict";
const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

// The files the .deb carries at absolute system paths, which electron-builder
// only places through fpm arguments. electron-builder.yml names them; this puts
// them where it looks.
//
// The helper's destination is not a choice: the polkit action annotates
// org.freedesktop.policykit.exec.path with it, so authorization is refused for
// a helper anywhere else. It is also the path the Wails package owned, which is
// what stops a dpkg upgrade from removing the file the new package needs.
const OUT = path.join(__dirname, "..", "linux");
const HELPER = "reasonix-studio-update-helper";

// Nothing to stage off Linux: no other target reads these, and a helper built
// for the wrong platform would be found by fpm and shipped.
if (process.platform !== "linux") {
  process.exit(0);
}

fs.mkdirSync(OUT, { recursive: true });
const built = spawnSync("go", ["build", "-o", path.join(OUT, HELPER), "../cmd/update-helper"], {
  cwd: path.join(__dirname, ".."),
  stdio: "inherit",
  env: { ...process.env, CGO_ENABLED: "0" },
});
if (built.status !== 0) {
  console.error(`building ${HELPER} failed`);
  process.exit(built.status ?? 1);
}
// pkexec runs it, so it has to be executable before fpm records the mode it
// finds on disk. Ownership is the package's, not this file's: fpm is told root
// explicitly rather than left to record whoever ran the build.
fs.chmodSync(path.join(OUT, HELPER), 0o755);
console.log(`staged ${HELPER}`);
