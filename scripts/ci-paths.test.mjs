import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";
import { changedFiles, classifyPaths } from "./ci-paths.mjs";

test("explicit documentation skips build surfaces", () => {
  for (const path of ["README.md", "docs/guide.md", "desktop/AGENTS.md", "desktop/README.md", "desktop/electron/README.md"]) {
    const { flags, unknown } = classifyPaths([path]);
    assert.equal(flags.desktop, false, path);
    assert.equal(flags.memory, false, path);
    assert.deepEqual(unknown, [], path);
  }
});

test("frontend inputs select frontend, browser, memory and native validation", () => {
  for (const path of ["desktop/frontend/src/components/Button.tsx", "desktop/frontend/public/help.md", "desktop/frontend/vite.config.ts", "desktop/pnpm-lock.yaml"]) {
    const { flags } = classifyPaths([path]);
    for (const name of ["desktop", "frontend", "browser", "memory", "native"]) assert.equal(flags[name], true, `${path}: ${name}`);
    assert.equal(flags.memory_full, false, path);
  }
});

test("App lifecycle and memory protocol inputs select the full memory screen", () => {
  for (const path of [
    "desktop/frontend/src/App.tsx",
    "desktop/frontend/src/AppRuntime.tsx",
    "desktop/frontend/src/app-runtime/AppRuntime.tsx",
    "desktop/frontend/src/app-shell/AppShell.tsx",
    "desktop/frontend/src/components/Transcript.tsx",
    "desktop/frontend/src/components/TranscriptCards.tsx",
    "desktop/frontend/src/lib/useController.ts",
    "desktop/frontend/src/lib/useControllerProfileCommands.ts",
    "desktop/frontend/src/lib/subscriptionScope.ts",
    "desktop/frontend/src/lib/useNavigationSurface.ts",
    "desktop/frontend/src/lib/navigationSurfaceTransition.ts",
    "desktop/frontend/src/lib/keyedResource.ts",
    "desktop/frontend/src/lib/fileResource.ts",
    "desktop/frontend/src/lib/useWorkspaceChangesResource.ts",
    "desktop/frontend/src/lib/mcpServerLifecycle.ts",
    "desktop/frontend/src/lib/fileNavigationLifetime.ts",
    "desktop/frontend/src/lib/bridge.ts",
    "desktop/frontend/src/lib/bridgeBenchFixtures.ts",
    "desktop/frontend/src/lib/bridgeHistoryFixtures.ts",
    "desktop/frontend/bench/app-memory.mjs",
    "desktop/frontend/bench/app-browser.mjs",
    "desktop/frontend/bench/app-page-actions.mjs",
  ]) {
    const { flags } = classifyPaths([path]);
    assert.equal(flags.memory, true, path);
    assert.equal(flags.memory_full, true, path);
  }
});

test("Go, Electron and packaging inputs stay on their owning surfaces", () => {
	const uninstaller = classifyPaths(["scripts/check-windows-uninstaller.mjs"]).flags;
	assert.equal(uninstaller.packaging, true);
	assert.equal(uninstaller.native, true);
	assert.equal(uninstaller.memory, false);
  let flags = classifyPaths(["internal/control/controller.go"]).flags;
  assert.equal(flags.code, true);
  assert.equal(flags.desktop_go, true);
  assert.equal(flags.frontend, false);
  assert.equal(flags.memory, false);
  flags = classifyPaths(["desktop/electron/src/main/window.ts"]).flags;
  assert.equal(flags.electron, true);
  assert.equal(flags.frontend, false);
  flags = classifyPaths(["desktop/packaging/package.mjs"]).flags;
  assert.equal(flags.packaging, true);
  assert.equal(flags.browser, false);
  for (const path of ["scripts/test-windows-upgrade-startup.ps1", "scripts/windows-upgrade-ui-evidence.ps1", "desktop/internal/upgradefixture/fixture.go", "desktop/cmd/windows-upgrade-fixture/main.go"]) {
    assert.equal(classifyPaths([path]).flags.packaging, true, path);
  }
});

test("Windows shell ownership paths select the full builtin package", () => {
  for (const path of ["internal/tool/builtin/workspace.go", "internal/tool/tool.go", "internal/sandbox/sandbox.go", "internal/permission/permission.go", "internal/permissionpreset/preset.go"]) {
    assert.equal(classifyPaths([path]).flags.windows_builtin, true, path);
  }
  assert.equal(classifyPaths(["internal/acp/e2e_test.go"]).flags.windows_builtin, false);
});

test("mixed changes cannot hide affected work and unknown paths fail closed", () => {
  let result = classifyPaths(["README.md", "desktop/frontend/src/App.tsx"]);
  assert.equal(result.flags.memory, true);
  result = classifyPaths(["shared/new-loader.js"]);
  assert.deepEqual(result.unknown, ["shared/new-loader.js"]);
  for (const name of ["code", "desktop", "frontend", "browser", "memory", "memory_full", "electron", "native", "packaging"])
    assert.equal(result.flags[name], true, name);
});

test("shared Node runtime changes validate every frontend artifact surface", () => {
  const { flags } = classifyPaths([".node-version"]);
  for (const name of ["code", "desktop", "frontend", "browser", "memory", "memory_full", "electron", "native", "packaging"])
    assert.equal(flags[name], true, name);
});

test("release notes and full events are deterministic", () => {
  assert.equal(classifyPaths(["release-notes/v1.md"]).flags.notes_only, true);
  const notesPush = classifyPaths(["release-notes/v1.md"], { full: true }).flags;
  assert.equal(notesPush.notes_only, true);
  assert.equal(notesPush.desktop, false);
  assert.equal(classifyPaths([]).flags.desktop, false);
  const full = classifyPaths([], { full: true }).flags;
  assert.equal(full.notes_only, false);
  for (const name of ["code", "desktop", "frontend", "browser", "memory", "memory_full", "electron", "native", "packaging", "site", "sdk", "windows_builtin"])
    assert.equal(full[name], true, name);
});

test("push and merge-base diffs retain deletions and renames", t => {
  const root = mkdtempSync(path.join(os.tmpdir(), "reasonix-ci-paths-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(path.join(root, "desktop/frontend/src"), { recursive: true });
  writeFileSync(path.join(root, "desktop/frontend/src/old.ts"), "old");
  writeFileSync(path.join(root, "desktop/frontend/src/delete.ts"), "delete");
  execFileSync("git", ["init"], { cwd: root });
  execFileSync("git", ["add", "."], { cwd: root });
  execFileSync("git", ["-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "base"], { cwd: root });
  const base = execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim();
  execFileSync("git", ["mv", "desktop/frontend/src/old.ts", "desktop/frontend/src/new.ts"], { cwd: root });
  execFileSync("git", ["rm", "desktop/frontend/src/delete.ts"], { cwd: root });
  execFileSync("git", ["-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-am", "change"], { cwd: root });
  const head = execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim();
  const push = changedFiles({ base, head, mode: "push", cwd: root });
  const pull = changedFiles({ base, head, mode: "pull_request", cwd: root });
  for (const files of [push, pull]) {
    assert.ok(files.includes("desktop/frontend/src/delete.ts"));
    assert.ok(files.includes("desktop/frontend/src/new.ts"));
    assert.equal(classifyPaths(files).flags.memory, true);
  }
  assert.deepEqual(changedFiles({ base: head, head, mode: "push", cwd: root }), []);
});

test("invalid diff identities fail instead of producing a skip", () => {
  assert.throws(() => changedFiles({ base: "0".repeat(40), head: "HEAD" }), /non-zero/);
  assert.throws(() => changedFiles({ base: "f".repeat(40), head: "HEAD" }));
});

test("CLI entry runs from paths with URL-significant characters", t => {
  const root = mkdtempSync(path.join(os.tmpdir(), "reasonix-ci-entry-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const expectedNames = ["code", "desktop", "desktop_go", "frontend", "browser", "memory", "memory_full", "electron", "native", "packaging", "site", "sdk", "windows_builtin", "release_control", "notes_only"];
  for (const directory of ["ordinary", "with space", "中文", "hash#directory", "literal%20directory"]) {
    const target = path.join(root, directory, "ci-paths.mjs");
    mkdirSync(path.dirname(target), { recursive: true });
    copyFileSync(new URL("./ci-paths.mjs", import.meta.url), target);
    const output = execFileSync(process.execPath, [target, "--full"], { encoding: "utf8" });
    const values = Object.fromEntries(output.trim().split("\n").map(line => line.split("=")));
    assert.deepEqual(Object.keys(values).sort(), [...expectedNames].sort(), directory);
    for (const name of expectedNames) assert.equal(values[name], name === "notes_only" ? "false" : "true", `${directory}: ${name}`);
  }
});

test("CLI writes GitHub output and module import stays side-effect free", t => {
  const root = mkdtempSync(path.join(os.tmpdir(), "reasonix-ci-output-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const target = path.join(root, "hash#literal%20", "ci-paths.mjs");
  const outputPath = path.join(root, "GitHub output.txt");
  mkdirSync(path.dirname(target), { recursive: true });
  copyFileSync(new URL("./ci-paths.mjs", import.meta.url), target);
  const stdout = execFileSync(process.execPath, [target, "--full", "--github-output", outputPath], { encoding: "utf8" });
  assert.equal(stdout, "");
  const output = readFileSync(outputPath, "utf8");
  assert.equal(output.trim().split("\n").length, 15);
  assert.match(output, /^desktop=true$/m);
  assert.match(output, /^notes_only=false$/m);

  const imported = execFileSync(process.execPath, ["--input-type=module", "--eval", `import(${JSON.stringify(pathToFileURL(target).href)})`], { encoding: "utf8" });
  assert.equal(imported, "");

  const stdinImported = execFileSync(process.execPath, ["--input-type=module", "-"], {
    input: `await import(${JSON.stringify(pathToFileURL(target).href)});`, encoding: "utf8",
  });
  assert.equal(stdinImported, "");
});

// site and sdk are in this list because the required aggregates accept a
// skipped site or a no-op sdk; a PR that edits the routing contract would
// otherwise satisfy those expectations without running either surface.
test("CI routing changes exercise their owning workflow surfaces", () => {
  for (const path of [".github/workflows/ci.yml", "scripts/ci-paths.mjs"]) {
    const flags = classifyPaths([path]).flags;
    for (const name of ["desktop", "desktop_go", "frontend", "browser", "electron", "native", "packaging", "site", "sdk"])
      assert.equal(flags[name], true, `${path}: ${name}`);
  }
  assert.equal(classifyPaths([".github/workflows/ci.yml"]).flags.memory, false);
  for (const path of [".github/workflows/app-memory.yml", "scripts/ci-paths.mjs", "scripts/ci-paths.test.mjs"]) {
    const flags = classifyPaths([path]).flags;
    assert.equal(flags.memory, true, path);
    assert.equal(flags.memory_full, true, path);
  }
  const memoryOnly = classifyPaths([".github/workflows/app-memory.yml"]).flags;
  assert.equal(memoryOnly.packaging, false);
  assert.equal(memoryOnly.sdk, false);
});

test("release control changes run focused contracts without selecting product suites", () => {
  for (const path of [
    ".github/workflows/release-candidate.yml",
    ".github/workflows/release-candidate-verify.yml",
    ".github/workflows/release-promote.yml",
    "scripts/release-candidate.mjs",
    "scripts/sync-release-site.sh",
    "scripts/sync-release-site.test.mjs",
    "scripts/check-release-public-access.sh",
    "scripts/validate-release-control-plane.sh",
    "scripts/verify-release-artifact-archive.mjs",
    "scripts/desktop-release-artifacts.test.mjs",
    "npm/publish-candidate.mjs",
  ]) {
    const flags = classifyPaths([path]).flags;
    assert.equal(flags.release_control, true, path);
    assert.equal(flags.code, false, path);
    assert.equal(flags.desktop, false, path);
  }
});
