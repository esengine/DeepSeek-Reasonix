import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const scope = (...files) =>
  spawnSync("bash", ["scripts/ci-platform-scope.sh"], { cwd: root, input: files.map((f) => f + "\0").join(""), encoding: "utf8" }).stdout.trim();

test("neutral paths do not need the macOS and Windows legs", () => {
  assert.equal(scope("docs/a.md", "README.md", "release-notes/studio/1.md"), "false");
  assert.equal(scope("desktop/frontend-next/src/a.tsx", "workers/crash-report/src/a.ts"), "false");
  assert.equal(scope("internal/model/openai/client.go", "internal/contract/tool/tool.go"), "false");
});

test("platform paths and unknown paths need them", () => {
  for (const f of [
    "internal/safety/sandbox/a.go",
    "internal/runtime/isolation/store.go",
    "internal/runtime/isolation/durable_test.go",
    "internal/runtime/delegation/job_finalization.go",
    "internal/runtime/delegation/writer_no_launch_test.go",
    "internal/tools/jobs/finalization.go",
    "internal/state/mutation/resources.go",
    "internal/state/mutation/capacity_test.go",
    "internal/platform/update/a.go",
    "cmd/reasonix/main.go",
    "desktop/electron/main.ts",
    "go.mod",
    ".github/workflows/ci.yml",
    "scripts/install-smoke.ps1",
    "internal/session/control/a.go",
    "unknown-file.txt",
  ]) assert.equal(scope("docs/a.md", f), "true", f);
});

test("a package carrying an OS-constrained file is platform-relevant whichever file changed", () => {
  assert.equal(scope("internal/frontend/cli/root.go"), "true");
  assert.equal(scope("internal/model/openai/other_windows.go"), "true");
});

test("an empty diff fails toward running everything", () => {
  assert.equal(scope(), "true");
});

test("names with spaces, a leading dash or a newline are read whole", () => {
  assert.equal(scope("docs/a b.md", "-odd/-x.go"), "false");
  assert.equal(scope("docs/a\nb.md", "internal/safety/x y.go"), "true");
  assert.equal(scope("docs/a\nb.md"), "false");
});

test("a rename out of a platform directory and a deleted file are both counted", () => {
  const git = (cwd, ...a) => spawnSync("git", ["-c", "user.name=t", "-c", "user.email=t@t", ...a], { cwd, encoding: "utf8" });
  const repo = mkdtempSync(join(tmpdir(), "scope-"));
  git(repo, "init", "-q");
  mkdirSync(join(repo, "internal/safety"), { recursive: true });
  mkdirSync(join(repo, "internal/model"), { recursive: true });
  writeFileSync(join(repo, "internal/safety/a.go"), "package safety\nfunc A() {}\n".repeat(20));
  writeFileSync(join(repo, "internal/safety/gone.go"), "package safety\n");
  git(repo, "add", "-A");
  git(repo, "commit", "-qm", "base");
  git(repo, "mv", "internal/safety/a.go", "internal/model/a.go");
  git(repo, "commit", "-qam", "rename");
  const names = (extra) => git(repo, "diff", "-z", "--name-only", ...extra, "HEAD~1", "HEAD").stdout;
  const run = (input) => spawnSync("bash", [join(root, "scripts/ci-platform-scope.sh")], { cwd: repo, input, encoding: "utf8" }).stdout.trim();
  assert.equal(run(names(["--no-renames"])), "true");
  assert.equal(run(names([])), "false", "default rename detection reports only the neutral new path");
  git(repo, "rm", "-q", "internal/safety/gone.go");
  git(repo, "commit", "-qm", "delete");
  assert.equal(run(names(["--no-renames"])), "true");
});
