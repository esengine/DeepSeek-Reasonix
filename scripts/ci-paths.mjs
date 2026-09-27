#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { appendFileSync, readFileSync, realpathSync } from "node:fs";
import { pathToFileURL } from "node:url";

const ZERO_SHA = /^0{40}$/;
const ROOT_DOC = /^[^/]+\.md$/;
const DESKTOP_DOC = /^(?:desktop\/(?:AGENTS|README)\.md|desktop\/electron\/README\.md|desktop\/third_party\/systray\/PATCHES\.md)$/;
const ROOT_UNRELATED = /^(?:docs\/|site\/|release-notes\/|desktop\/|workers\/)/;
const KNOWN_INDEPENDENT = /^(?:docs\/|site\/|release-notes\/|workers\/|benchmarks\/|npm\/|sdk\/|\.github\/|tools\/|scripts\/)/;
const ROOT_TOOLING = /^(?:Makefile|\.golangci[^/]*)$/;
const FRONTEND = /^desktop\/frontend\//;
const DESKTOP_MANIFEST = /^(?:desktop\/(?:package\.json|pnpm-lock\.yaml|pnpm-workspace\.yaml|\.npmrc)|desktop\/frontend\/(?:package\.json|pnpm-lock\.yaml|vite\.config\.[cm]?[jt]s|tsconfig[^/]*\.json))$/;
const ELECTRON = /^(?:desktop\/electron\/|desktop\/(?:package\.json|pnpm-lock\.yaml|pnpm-workspace\.yaml)$)/;
const PACKAGING = /^(?:desktop\/(?:packaging\/|build\/|internal\/upgradefixture\/|cmd\/windows-upgrade-fixture\/)|scripts\/(?:desktop-build|package-windows-desktop|install-nsis|check-windows-uninstaller|finalize-windows-signed-candidate|test-windows-(?:installer-startup|startup-recovery|upgrade-startup)|windows-(?:upgrade-ui-evidence|acceptance-environment))\b)/;
const RELEASE_CONTROL = /^(?:\.github\/workflows\/release[^/]*\.yml|scripts\/(?:release|resolve-release-candidate|validate-release-candidate|build-release-cli-candidate|publish-homebrew-cask|desktop-release-artifacts|finalize-windows-signed-candidate|verify-release-artifact-archive|verify-release-tag-identity|verify-stable-release-artifacts)[^/]*|npm\/publish(?:-candidate)?(?:\.test)?\.mjs)$/;
const DESKTOP_GO = /^(?:desktop\/(?:[^/]+\.go|go\.(?:mod|sum)|cmd\/|internal\/)|internal\/|cmd\/|go\.(?:mod|sum)$)/;
const SDK = /^(?:sdk\/|internal\/extension\/)/;
const WINDOWS_BUILTIN = /^(?:internal\/(?:tool\/builtin\/|tool\/tool\.go$|sandbox\/|permission\/|permissionpreset\/)|scripts\/windows-pr-contract-tests(?:\.test)?\.mjs$)/;
const CI_CONTROL = /^(?:\.github\/workflows\/ci\.yml|scripts\/ci-paths(?:\.test)?\.mjs)$/;
const MEMORY_CONTROL = /^(?:\.github\/workflows\/app-memory\.yml|scripts\/ci-paths(?:\.test)?\.mjs)$/;
const MEMORY_FULL = /^(?:desktop\/frontend\/(?:bench\/app-(?:memory|browser|page-actions)[^/]*|src\/(?:App(?:Runtime)?\.tsx|app-runtime\/.*|app-shell\/.*|components\/Transcript(?:Cards)?\.tsx|lib\/(?:useController[^/]*|subscriptionScope|useNavigationSurface|navigationSurfaceTransition|keyedResource|fileResource|useWorkspaceChangesResource|mcpServerLifecycle|fileNavigationLifetime|bridge(?:BenchFixtures|HistoryFixtures)?)\.[^/]+))|\.github\/workflows\/app-memory\.yml|scripts\/ci-paths(?:\.test)?\.mjs)$/;

const FLAG_NAMES = ["code", "desktop", "desktop_go", "frontend", "browser", "memory", "memory_full", "electron", "native", "packaging", "site", "sdk", "windows_builtin", "release_control", "notes_only"];

function normalized(path) {
  return path.replaceAll("\\", "/").replace(/^\.\//, "");
}

function setReason(reasons, flag, path, reason) {
  if (!reasons[flag]) reasons[flag] = [];
  reasons[flag].push(`${path} (${reason})`);
}

export function classifyPaths(input, { full = false } = {}) {
  const files = [...new Set(input.map(normalized).filter(Boolean))].sort();
  const flags = Object.fromEntries(FLAG_NAMES.map(name => [name, false]));
  const reasons = {};
  const notesOnly = files.length > 0 && files.every(path => path.startsWith("release-notes/"));
  if (full && notesOnly) {
    flags.notes_only = true;
    setReason(reasons, "notes_only", "event", "release-notes-only push exception");
    return { files, flags, reasons, unknown: [] };
  }
  if (full) {
    for (const name of FLAG_NAMES) flags[name] = name !== "notes_only";
    for (const name of FLAG_NAMES.filter(name => name !== "notes_only")) setReason(reasons, name, "event", "full validation");
    return { files, flags, reasons, unknown: [] };
  }
  flags.notes_only = notesOnly;
  const unknown = [];
  for (const path of files) {
    const explicitDoc = ROOT_DOC.test(path) || DESKTOP_DOC.test(path);
    if (path.startsWith("site/")) {
      flags.site = true;
      setReason(reasons, "site", path, "site source");
    }
    const releaseControl = RELEASE_CONTROL.test(path) || /^scripts\/(?:sync-release-site|observe-release-site|fetch-stable-release-manifest|check-release-public-access|test-release-control-contracts|validate-release-control-plane)[^/]*$/.test(path);
    if (releaseControl) {
      flags.release_control = true;
      setReason(reasons, "release_control", path, "release control plane");
    }
    if (SDK.test(path)) {
      flags.sdk = true;
      setReason(reasons, "sdk", path, "SDK or generated protocol source");
    }
    if (WINDOWS_BUILTIN.test(path)) {
      flags.windows_builtin = true;
      setReason(reasons, "windows_builtin", path, "Windows shell, workspace or sandbox contract");
    }
    // site and sdk belong here too: a PR that edits the routing contract must
    // exercise every gate it can change, and without them such a PR skips site
    // and no-ops sdk while the aggregates trivially accept those skips.
    if (CI_CONTROL.test(path)) {
      for (const flag of ["desktop_go", "frontend", "browser", "electron", "native", "packaging", "site", "sdk", "windows_builtin"]) {
        flags[flag] = true;
        setReason(reasons, flag, path, "CI routing contract");
      }
    }
    if (MEMORY_CONTROL.test(path)) {
      for (const flag of ["memory", "memory_full"]) {
        flags[flag] = true;
        setReason(reasons, flag, path, "memory workflow or shared routing contract");
      }
    }
    if (!releaseControl && !ROOT_UNRELATED.test(path) && !ROOT_DOC.test(path)) {
      flags.code = true;
      setReason(reasons, "code", path, "root module input");
    }
    if (explicitDoc || ROOT_TOOLING.test(path) || (KNOWN_INDEPENDENT.test(path) && !PACKAGING.test(path))) continue;

    const frontend = FRONTEND.test(path) || DESKTOP_MANIFEST.test(path);
    const electron = ELECTRON.test(path);
    const packaging = PACKAGING.test(path);
    const desktopGo = DESKTOP_GO.test(path);
    if (frontend) {
      for (const flag of ["frontend", "browser", "memory"]) {
        flags[flag] = true;
        setReason(reasons, flag, path, "frontend build input");
      }
      if (MEMORY_FULL.test(path)) {
        flags.memory_full = true;
        setReason(reasons, "memory_full", path, "App lifecycle or memory screening input");
      }
    }
    if (electron) {
      flags.electron = true;
      setReason(reasons, "electron", path, "Electron shell input");
    }
    if (packaging) {
      flags.packaging = true;
      setReason(reasons, "packaging", path, "packaging input");
    }
    if (desktopGo) {
      flags.desktop_go = true;
      setReason(reasons, "desktop_go", path, "Desktop or shared Go input");
    }
    if (!(frontend || electron || packaging || desktopGo)) unknown.push(path);
  }
  if (unknown.length > 0) {
    for (const flag of ["code", "desktop_go", "frontend", "browser", "memory", "memory_full", "electron", "native", "packaging"]) {
      flags[flag] = true;
      for (const path of unknown) setReason(reasons, flag, path, "unknown path; fail closed");
    }
  }
  flags.native ||= flags.desktop_go || flags.frontend || flags.electron || flags.packaging;
  if (flags.native && !reasons.native) setReason(reasons, "native", "derived", "Desktop integration input");
  flags.desktop = flags.native || flags.browser;
  if (flags.desktop) setReason(reasons, "desktop", "derived", "one or more Desktop surfaces changed");
  return { files, flags, reasons, unknown };
}

export function changedFiles({ base, head = "HEAD", mode = "pull_request", cwd = process.cwd() }) {
  if (!base || ZERO_SHA.test(base) || !head || ZERO_SHA.test(head)) throw new Error("a non-zero base and head SHA are required");
  for (const sha of [base, head]) execFileSync("git", ["cat-file", "-e", `${sha}^{commit}`], { cwd, stdio: "ignore" });
  const range = mode === "pull_request" ? `${base}...${head}` : `${base}..${head}`;
  return execFileSync("git", ["diff", "--name-only", "-z", range], { cwd, encoding: "utf8" }).split("\0").filter(Boolean);
}

function parseArgs(argv) {
  const args = {};
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === "--full") args.full = true;
    else if (argv[i].startsWith("--")) args[argv[i].slice(2).replaceAll("-", "_")] = argv[++i];
    else throw new Error(`unexpected argument ${argv[i]}`);
  }
  return args;
}

function summary(result) {
  const lines = ["## CI path decision", "", "| Surface | Run | Trigger |", "| --- | --- | --- |"];
  for (const name of FLAG_NAMES) lines.push(`| ${name} | ${result.flags[name]} | ${(result.reasons[name] ?? ["none"]).join("<br>")} |`);
  lines.push("", `Changed files: ${result.files.length}`, "", ...result.files.map(path => `- \`${path}\``));
  return lines.join("\n") + "\n";
}

function isMainModule() {
  // stdin/eval hosts can provide a sentinel or a nonexistent argv[1]. Importing
  // this module must not require that host argument to name a filesystem entry.
  if (!process.argv[1] || process.argv[1] === "-") return false;
  try {
    return import.meta.url === pathToFileURL(realpathSync(process.argv[1])).href;
  } catch {
    return false;
  }
}

if (isMainModule()) {
  try {
    const args = parseArgs(process.argv.slice(2));
    let files;
    if (args.files) files = readFileSync(args.files, "utf8").split("\0").filter(Boolean);
    else if (args.full && !args.base) files = [];
    else files = changedFiles({ base: args.base, head: args.head, mode: args.mode });
    const result = classifyPaths(files, { full: args.full });
    const output = Object.entries(result.flags).map(([key, value]) => `${key}=${value}\n`).join("");
    if (args.github_output) appendFileSync(args.github_output, output);
    else process.stdout.write(output);
    if (args.summary) appendFileSync(args.summary, summary(result));
  } catch (error) {
    console.error(`ci-paths: ${error.message}`);
    process.exitCode = 1;
  }
}
