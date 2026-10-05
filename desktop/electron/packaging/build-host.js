"use strict";
const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

// The kernel the shell spawns, built into the directory electron-builder.yml
// lists as an extra resource.
//
// On macOS it has to be one universal binary rather than the host's own
// architecture. --universal packages an x64 app and an arm64 app and merges
// them, and @electron/universal reads each Mach-O it finds in both: already
// universal on both sides is accepted and left alone, differing slices are
// lipo'd together, and byte-identical thin slices are refused outright. A
// single-architecture kernel copied into both legs is that third case.
const OUT = path.join(__dirname, "..", "bin");
// The host is a main-module package, so it is built from the main module: run
// from here and Go resolves it against desktop/go.mod, whose go.sum carries
// only what the desktop module's own code imports.
const ROOT = path.join(__dirname, "..", "..", "..");
const PKG = "./cmd/reasonix-studio-host";
const CWD = ROOT;

// The symbol table and DWARF are 28% of this binary and nothing in a shipped
// build reads them: a Go panic's traceback comes from the pclntab, which -w
// leaves alone.
//
// The version has to be injected here as well as into the Electron metadata.
// This binary is what opens a workspace on another machine, and the routes that
// install a kernel over there name a release by it; left at "dev" they refuse
// before reaching the network, and npm is all that is left. A build without it
// still works — it just cannot install a remote kernel from a release.
const VERSION = (process.env.REASONIX_VERSION || "").trim();
const LDFLAGS = VERSION ? `-s -w -X main.version=${VERSION}` : "-s -w";

function go(args, env) {
  const run = spawnSync("go", args, { cwd: CWD, stdio: "inherit", env: { ...process.env, ...env } });
  if (run.status !== 0) {
    console.error(`go ${args.join(" ")} failed`);
    process.exit(run.status ?? 1);
  }
}

// The Windows helper is C++ against the system's own UI Automation, input and
// imaging APIs, built with whichever MSVC vswhere finds. The CRT is linked in
// statically so the helper needs no redistributable on the person's machine.
const VS_INSTALLER = path.join(process.env["ProgramFiles(x86)"] || "C:\\Program Files (x86)", "Microsoft Visual Studio", "Installer");

function msvcVars() {
  const vswhere = path.join(VS_INSTALLER, "vswhere.exe");
  if (!fs.existsSync(vswhere)) return "";
  const found = spawnSync(vswhere, ["-latest", "-products", "*", "-requires", "Microsoft.VisualStudio.Component.VC.Tools.x86.x64", "-property", "installationPath"], { encoding: "utf8" });
  const root = (found.stdout || "").trim().split(/\r?\n/)[0];
  const vars = root ? path.join(root, "VC", "Auxiliary", "Build", "vcvars64.bat") : "";
  return vars && fs.existsSync(vars) ? vars : "";
}

function buildWindowsHelper() {
  const vars = msvcVars();
  if (!vars) {
    // A release without the helper would ship computer use switched off with
    // nothing saying why, so CI refuses; a dev build only loses the feature.
    if (process.env.CI) {
      console.error("MSVC with the x64 C++ tools is required to build reasonix-computer-helper.exe");
      process.exit(1);
    }
    console.warn("MSVC not found: building without reasonix-computer-helper.exe, so computer use is unavailable in this build");
    return;
  }
  const src = path.join(ROOT, "desktop", "computer-helper", "windows");
  const objs = fs.mkdtempSync(path.join(require("node:os").tmpdir(), "reasonix-helper-"));
  const out = path.join(OUT, "reasonix-computer-helper.exe");
  const libs = "user32.lib gdi32.lib ole32.lib oleaut32.lib uiautomationcore.lib windowscodecs.lib dwmapi.lib version.lib advapi32.lib uuid.lib gdiplus.lib";
  const script = path.join(objs, "build.cmd");
  fs.writeFileSync(script, [
    "@echo off",
    // vcvars looks for vswhere on PATH and complains when it is not there.
    `set "PATH=%PATH%;${VS_INSTALLER}"`,
    `call "${vars}" >nul || exit /b 1`,
    `cd /d "${src}"`,
    `cl /nologo /std:c++17 /EHsc /O2 /MT /utf-8 /W4 /DUNICODE /D_UNICODE /Fo"${objs}\\\\" /Fe"${out}" *.cpp /link ${libs}`,
  ].join("\r\n"));
  const built = spawnSync("cmd.exe", ["/d", "/c", script], { stdio: "inherit", env: { ...process.env, VSLANG: "1033" } });
  fs.rmSync(objs, { recursive: true, force: true });
  if (built.status !== 0) {
    console.error("building reasonix-computer-helper.exe failed");
    process.exit(built.status ?? 1);
  }
  console.log("built reasonix-computer-helper.exe");
}

if (process.platform !== "darwin") {
  // go build names the binary after the package and adds .exe where it belongs.
  // Without cgo, because with it the Linux kernel inherits the runner's glibc.
  go(["build", "-ldflags", LDFLAGS, "-o", OUT + path.sep, PKG], { CGO_ENABLED: "0" });
  console.log("built reasonix-studio-host");
  if (process.platform === "win32") buildWindowsHelper();
  process.exit(0);
}

// CGO_ENABLED=0 because one of these two is always a cross-compile, and the
// host is pure Go — studio.yml holds it to that.
const slices = ["amd64", "arm64"].map((arch) => {
  const out = path.join(OUT, `reasonix-studio-host-${arch}`);
  go(["build", "-ldflags", LDFLAGS, "-o", out, PKG], { CGO_ENABLED: "0", GOOS: "darwin", GOARCH: arch });
  return out;
});

const universal = path.join(OUT, "reasonix-studio-host");
const merged = spawnSync("lipo", ["-create", "-output", universal, ...slices], { stdio: "inherit" });
if (merged.status !== 0) {
  console.error("lipo failed");
  process.exit(merged.status ?? 1);
}
for (const slice of slices) {
  fs.rmSync(slice, { force: true });
}
console.log("built reasonix-studio-host (universal)");

// The helper that operates other applications, universal for the same reason.
// It targets macOS 12, the oldest this app runs on; what needs a later system
// checks at run time and answers computer.unsupported.
const HELPER = path.join(ROOT, "desktop", "computer-helper", "Sources");
const sources = fs.readdirSync(HELPER).filter((f) => f.endsWith(".swift")).map((f) => path.join(HELPER, f));
const helperSlices = [["x86_64", "amd64"], ["arm64", "arm64"]].map(([target, arch]) => {
  const out = path.join(OUT, `reasonix-computer-helper-${arch}`);
  const built = spawnSync("swiftc", ["-O", "-target", `${target}-apple-macos12`, ...sources, "-o", out], { stdio: "inherit" });
  if (built.status !== 0) {
    console.error(`swiftc for ${target} failed`);
    process.exit(built.status ?? 1);
  }
  return out;
});
const helper = path.join(OUT, "reasonix-computer-helper");
fs.rmSync(helper, { force: true });
const helperMerged = spawnSync("lipo", ["-create", "-output", helper, ...helperSlices], { stdio: "inherit" });
if (helperMerged.status !== 0) {
  console.error("lipo failed for the computer-use helper");
  process.exit(helperMerged.status ?? 1);
}
for (const slice of helperSlices) {
  fs.rmSync(slice, { force: true });
}
console.log("built reasonix-computer-helper (universal)");
