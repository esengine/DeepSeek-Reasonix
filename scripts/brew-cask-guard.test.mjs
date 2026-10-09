import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { caskDecision } from "./brew-cask-guard.mjs";

const cask = (v) => `cask "reasonix" do\n  version "${v}"\nend\n`;

test("a candidate equal to or newer than the tap's cask is written", () => {
  assert.equal(caskDecision(cask("2.27.0"), "2.27.0"), "write");
  assert.equal(caskDecision(cask("1.39.7"), "2.28.0"), "write");
  assert.equal(caskDecision(cask("2.9.0"), "2.10.0"), "write");
});

test("an older candidate never moves the cask backwards", () => {
  assert.equal(caskDecision(cask("2.28.0"), "2.27.0"), "skip");
  assert.equal(caskDecision(cask("2.10.0"), "2.9.0"), "skip");
  assert.equal(caskDecision(cask("2.28.0"), "2.28.0-preview.1"), "skip");
});

test("a cask without a readable version fails closed", () => {
  assert.throws(() => caskDecision("cask \"reasonix\" do\nend\n", "2.28.0"), /no readable version/);
});

test("the command prints the decision and fails with an error annotation when unreadable", () => {
  const script = path.join(path.dirname(fileURLToPath(import.meta.url)), "brew-cask-guard.mjs");
  const dir = mkdtempSync(path.join(tmpdir(), "cask-"));
  const file = path.join(dir, "reasonix.rb");
  writeFileSync(file, cask("2.28.0"));
  const run = (f, v) => spawnSync("node", [script, f, v], { encoding: "utf8" });
  assert.equal(run(file, "2.27.0").stdout.trim(), "skip");
  const missing = run(path.join(dir, "absent.rb"), "2.27.0");
  assert.notEqual(missing.status, 0);
  assert.match(missing.stderr, /::error::/);
});

const here = path.dirname(fileURLToPath(import.meta.url));
const workflowLines = readFileSync(path.join(here, "../.github/workflows/release-studio.yml"), "utf8").split("\n");

function stepRun(name) {
  const at = workflowLines.findIndex((l) => l.trim() === `- name: ${name}`);
  assert.ok(at >= 0, `no step named ${name}`);
  const run = workflowLines.findIndex((l, i) => i > at && l.trim() === "run: |");
  const indent = workflowLines[run].search(/\S/) + 2;
  const body = [];
  for (let i = run + 1; i < workflowLines.length; i += 1) {
    if (workflowLines[i].trim() !== "" && workflowLines[i].search(/\S/) < indent) break;
    body.push(workflowLines[i].slice(indent));
  }
  return body.join("\n");
}

// Runs the workflow's own Homebrew step with git stubbed: clone yields a tap
// holding `tapCask`, every other git call is a no-op.
function runStep(tapCask, { brokenNode = false } = {}) {
  const dir = mkdtempSync(path.join(tmpdir(), "cask-step-"));
  const bin = path.join(dir, "bin");
  mkdirSync(bin);
  writeFileSync(
    path.join(bin, "git"),
    `#!/bin/sh\nif [ "$1" = clone ]; then mkdir -p tap/Casks; printf '%s' "$STUB_CASK" > tap/Casks/reasonix.rb; fi\nexit 0\n`,
    { mode: 0o755 },
  );
  if (brokenNode) writeFileSync(path.join(bin, "node"), "#!/bin/sh\nexit 1\n", { mode: 0o755 });
  mkdirSync(path.join(dir, "cli-archives"));
  const names = ["darwin-arm64.tar.gz", "darwin-amd64.tar.gz", "linux-arm64.tar.gz", "linux-amd64.tar.gz"];
  writeFileSync(
    path.join(dir, "cli-archives/SHA256SUMS"),
    names.map((n) => `${"a".repeat(64)}  reasonix-${n}`).join("\n") + "\n",
  );
  symlinkSync(here, path.join(dir, "scripts"));
  symlinkSync(path.join(here, "../npm"), path.join(dir, "npm"));
  const result = spawnSync("bash", ["-c", stepRun("Update the Homebrew cask")], {
    cwd: dir,
    encoding: "utf8",
    env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, STUB_CASK: tapCask, TAP_TOKEN: "t", VERSION: "v2.27.0", TAG: "v2.27.0" },
  });
  return { ...result, cask: readFileSync(path.join(dir, "tap/Casks/reasonix.rb"), "utf8") };
}

test("the step leaves a newer cask untouched", () => {
  const r = runStep(cask("2.28.0"));
  assert.equal(r.status, 0, `${r.stdout} ${r.stderr}`);
  assert.equal(r.cask, cask("2.28.0"));
});

test("the step fails closed and leaves the cask untouched when the tap version is unreadable", () => {
  const unreadable = 'cask "reasonix" do\nend\n';
  const r = runStep(unreadable);
  assert.notEqual(r.status, 0);
  assert.equal(r.cask, unreadable);
});

test("the step fails closed when the guard itself cannot run", () => {
  const r = runStep(cask("1.0.0"), { brokenNode: true });
  assert.notEqual(r.status, 0);
  assert.equal(r.cask, cask("1.0.0"));
});

test("the step rewrites an older cask", () => {
  const r = runStep(cask("2.26.0"));
  assert.equal(r.status, 0, `${r.stdout} ${r.stderr}`);
  assert.match(r.cask, /version "2\.27\.0"/);
});
