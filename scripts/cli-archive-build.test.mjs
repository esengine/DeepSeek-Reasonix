import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(here, "..");
const lines = readFileSync(path.join(root, ".github/workflows/release-studio.yml"), "utf8").split("\n");

function stepRun(name) {
  const at = lines.findIndex((l) => l.trim() === `- name: ${name}`);
  assert.ok(at >= 0, `no step named ${name}`);
  const run = lines.findIndex((l, i) => i > at && l.trim() === "run: |");
  const indent = lines[run].search(/\S/) + 2;
  const body = [];
  for (let i = run + 1; i < lines.length; i += 1) {
    if (lines[i].trim() !== "" && lines[i].search(/\S/) < indent) break;
    body.push(lines[i].slice(indent));
  }
  return body.join("\n");
}

test("the archives the updaters install report the commit and release they were built from", () => {
  const out = mkdtempSync(path.join(tmpdir(), "cli-archive-"));
  try {
    const goenv = (key) => execFileSync("go", ["env", key], { encoding: "utf8" }).trim();
    const [goos, goarch] = [goenv("GOOS"), goenv("GOARCH")];
    const sha = execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim();
    const rewrite = (text, from, to) => {
      const next = text.replace(from, to);
      assert.notEqual(next, text, `the step no longer contains ${from}; update this test`);
      return next;
    };
    let script = stepRun("Cross-compile and archive");
    script = rewrite(script, /for target in [^;]+; do/, `for target in ${goos}/${goarch}; do`);
    script = rewrite(script, /^\(cd .* sha256sum .*$/m, "");
    script = script.replaceAll("dist", `"${out}"`);
    execFileSync("bash", ["-c", script], {
      cwd: root,
      env: { ...process.env, VERSION: "v9.8.7", SHA: sha },
      stdio: "pipe",
    });
    const archive = path.join(out, `reasonix-${goos}-${goarch}.${goos === "windows" ? "zip" : "tar.gz"}`);
    execFileSync("tar", ["xf", archive, "-C", out]);
    const exe = path.join(out, goos === "windows" ? "reasonix.exe" : "reasonix");
    const report = execFileSync(exe, ["version", "--verbose"], { encoding: "utf8" });
    assert.match(report, /^reasonix v9\.8\.7$/m);
    assert.ok(report.includes(`git_commit: ${sha.slice(0, 12)}`), report);
    assert.match(report, /^build_time_utc: \d{4}-\d\d-\d\dT/m);
    const binary = readFileSync(exe);
    assert.ok(binary.includes(sha), "the docs corpus revision is not linked");
  } finally {
    rmSync(out, { recursive: true, force: true });
  }
});
