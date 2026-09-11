import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { notarizeDesktop } from "./notarize-desktop.mjs";

const id = "00000000-0000-4000-8000-000000000001";
const env = { APPLE_API_KEY_PATH: "private/key.p8", APPLE_API_KEY_ID: "key-id", APPLE_API_ISSUER_ID: "issuer" };
const ok = (value = {}) => ({ status: 0, stdout: JSON.stringify(value) });

function fixture(t, { kind = "app", status = "Accepted", submit, log, fail } = {}) {
  const diagnosticsDir = mkdtempSync(join(tmpdir(), "reasonix-notary-test-"));
  t.after(() => rmSync(diagnosticsDir, { recursive: true, force: true }));
  const calls = [], warnings = [];
  const execute = () => notarizeDesktop({
    archive: kind === "app" ? "upload.zip" : "Reasonix.dmg",
    target: kind === "app" ? "Reasonix.app" : "Reasonix.dmg",
    kind, diagnosticsDir, env, warn: (message) => warnings.push(message),
    run: (command, args) => {
      calls.push([command, ...args]);
      if (fail?.(command, args)) return { status: 65, stdout: "" };
      if (args[0] === "notarytool" && args[1] === "submit") return submit ?? ok({ id, status, path: "private/upload.zip" });
      if (args[0] === "notarytool" && args[1] === "log") return log ?? ok({ jobId: id, status, issues: [] });
      return { status: 0, stdout: "" };
    },
  });
  const report = (name) => JSON.parse(readFileSync(join(diagnosticsDir, `${kind}-${name}.json`), "utf8"));
  return { execute, calls, warnings, report, diagnosticsDir };
}

for (const kind of ["app", "dmg"]) {
  test(`${kind}: verify before submit, then log, staple, validate and assess`, (t) => {
    const f = fixture(t, { kind });
    f.execute();
    assert.deepEqual(f.calls.map((call) => call.slice(0, 3)), [
      ["codesign", "--verify", kind === "app" ? "--deep" : "--strict"],
      ["xcrun", "notarytool", "submit"], ["xcrun", "notarytool", "log"],
      ["xcrun", "stapler", "staple"], ["xcrun", "stapler", "validate"],
      ["spctl", "--assess", "--verbose=4"],
    ]);
    assert.ok(f.calls[0].includes("--strict"));
    assert.deepEqual(f.calls[1].slice(-3), ["--wait", "--output-format", "json"]);
    assert.equal(f.calls[2][3], id);
    assert.deepEqual(f.calls.at(-1), kind === "app"
      ? ["spctl", "--assess", "--verbose=4", "--type", "exec", "Reasonix.app"]
      : ["spctl", "--assess", "--verbose=4", "--type", "open", "--context", "context:primary-signature", "Reasonix.dmg"]);
    assert.deepEqual(f.report("submission"), { id, status: "Accepted", exitCode: 0, signal: null });
    assert.equal(f.report("notary-log").jobId, id);
    for (const name of readdirSync(f.diagnosticsDir)) {
      const text = readFileSync(join(f.diagnosticsDir, name), "utf8");
      assert.doesNotMatch(text, /private\/|key-id|issuer/);
    }
  });
}

for (const status of ["Invalid", "Rejected", "In Progress", undefined]) {
  test(`zero exit with ${status} must fetch log and stop before stapling`, (t) => {
    const f = fixture(t, { submit: ok({ id, status }) });
    assert.throws(f.execute, /Notarization/);
    assert.equal(f.report("submission").status, status ?? null);
    assert.equal(f.report("notary-log").jobId, id);
    assert.equal(f.calls.length, 3);
  });
}

test("Apple rejection details are preserved in the log artifact", (t) => {
  const issues = [{ severity: "error", path: "Reasonix.app/Contents/MacOS/Reasonix", message: "The signature is invalid." }];
  const f = fixture(t, { status: "Invalid", log: ok({ jobId: id, issues }) });
  assert.throws(f.execute, /Invalid/);
  assert.deepEqual(f.report("notary-log").issues, issues);
});

test("a repeated local build cannot retain an older submission's log", (t) => {
  const f = fixture(t, { status: "Invalid", log: { status: 1, stdout: "" } });
  writeFileSync(join(f.diagnosticsDir, "app-notary-log.json"), JSON.stringify({ status: "Accepted" }));
  writeFileSync(join(f.diagnosticsDir, "dmg-notary-log.json"), JSON.stringify({ status: "Accepted" }));
  assert.throws(f.execute, /Invalid/);
  assert.deepEqual(readdirSync(f.diagnosticsDir).sort(), ["app-submission.json", "dmg-notary-log.json"]);
});

test("nonzero submit still fetches log and cannot pass with Accepted", (t) => {
  const f = fixture(t, { submit: { status: 1, stdout: JSON.stringify({ id, status: "Accepted" }) } });
  assert.throws(f.execute, /Notarization/);
  assert.equal(f.report("submission").exitCode, 1);
  assert.equal(f.calls.length, 3);
});

for (const submit of [
  { status: 1, stdout: "not JSON" }, ok({ status: "Accepted" }),
  ok({ id: "--unexpected-option", status: "Accepted" }),
  { status: null, signal: "SIGTERM", stdout: "" },
  { status: null, error: new Error("spawn failed"), stdout: "" },
]) {
  test(`unusable submit response fails without fetching an unknown ID: ${JSON.stringify(submit)}`, (t) => {
    const f = fixture(t, { submit });
    assert.throws(f.execute, /no submission ID/);
    assert.equal(f.report("submission").id, null);
    assert.equal(f.calls.length, 2);
  });
}

for (const status of ["Accepted", "Invalid"]) {
  for (const log of [{ status: 1, stdout: "" }, { status: 0, stdout: "not JSON" }]) {
    test(`unavailable log does not replace the ${status} verdict (${log.status})`, (t) => {
      const f = fixture(t, { status, log });
      if (status === "Accepted") f.execute();
      else assert.throws(f.execute, /Invalid/);
      assert.equal(f.warnings.length, 1);
      assert.match(f.warnings[0], new RegExp(id));
      assert.deepEqual(readdirSync(f.diagnosticsDir), ["app-submission.json"]);
    });
  }
}

for (const stage of ["codesign", "staple", "validate", "spctl"]) {
  test(`${stage} failure stops the pipeline`, (t) => {
    const f = fixture(t, { fail: (command, args) => command === stage || args[1] === stage });
    assert.throws(f.execute, /failed/);
    assert.equal(f.calls.length, { codesign: 1, staple: 4, validate: 5, spctl: 6 }[stage]);
  });
}

test("both release artifacts use the shared notarization gate and diagnostics survive failure", () => {
  const build = readFileSync(new URL("./desktop-build.sh", import.meta.url), "utf8");
  const workflow = readFileSync(new URL("../.github/workflows/release-desktop.yml", import.meta.url), "utf8");
  assert.match(build, /notarize-desktop\.mjs" "\$staging\/notarize\.zip" "\$app" app "\$notary_diagnostics"/);
  assert.match(build, /notarize-desktop\.mjs" "\$dmg" "\$dmg" dmg "\$notary_diagnostics"/);
  assert.doesNotMatch(build, /xcrun (notarytool|stapler)/);
  const upload = workflow.split("- name: Upload Apple notarization diagnostics")[1]?.split("\n      #")[0];
  assert.ok(upload);
  assert.match(upload, /always\(\) && runner.os == 'macOS'/);
  assert.match(upload, /uses: actions\/upload-artifact@v7/);
  assert.match(upload, /path: \$\{\{ runner.temp \}\}\/apple-notarization\/\*\.json/);
  assert.match(workflow, /APPLE_NOTARIZATION_LOG_DIR: \$\{\{ runner.temp \}\}\/apple-notarization/);
});

test("historical log workflow only reads Apple submissions from the protected release environment", () => {
  const workflow = readFileSync(new URL("../.github/workflows/apple-notary-log.yml", import.meta.url), "utf8");
  assert.match(workflow, /workflow_dispatch:/);
  assert.match(workflow, /github\.ref == 'refs\/heads\/main-v2' && github\.ref_protected/);
  assert.match(workflow, /environment: release/);
  assert.match(workflow, /contents: read/);
  assert.match(workflow, /SUBMISSION_ID: \$\{\{ inputs.submission_id \}\}/);
  assert.match(workflow, /\[\[ "\$SUBMISSION_ID" =~ \^\[0-9a-fA-F\]/);
  assert.match(workflow, /trap 'rm -f "\$key_path"' EXIT/);
  assert.match(workflow, /umask 077/);
  assert.match(workflow, /xcrun notarytool log "\$SUBMISSION_ID"/);
  assert.match(workflow, /if: always\(\)/);
  assert.match(workflow, /path: \$\{\{ runner.temp \}\}\/apple-notarization\/\*\.json/);
  assert.doesNotMatch(workflow, /notarytool submit|codesign|stapler|actions\/checkout|contents: write|pull_request_target/);
});

for (const scenario of ["success", "log-failure", "invalid-id"]) {
  test(`historical workflow shell: ${scenario}, with credential cleanup`, (t) => {
    const root = mkdtempSync(join(tmpdir(), "reasonix-notary-workflow-"));
    t.after(() => rmSync(root, { recursive: true, force: true }));
    const xcrun = join(root, "xcrun");
    writeFileSync(xcrun, `#!${process.execPath}
import fs from 'node:fs';
const args = process.argv.slice(2);
if (args[0] !== 'notarytool' || !['info', 'log'].includes(args[1])) process.exit(99);
const key = args[args.indexOf('--key') + 1];
if (fs.readFileSync(key, 'utf8') !== 'fixture-key') process.exit(98);
if ((fs.statSync(key).mode & 0o777) !== 0o600) process.exit(97);
fs.appendFileSync(process.env.RUNNER_TEMP + '/calls', args[1] + '\\n');
if (args[1] === 'info') console.log(JSON.stringify({id: args[2], status: 'Invalid'}));
else if (process.env.FAIL_LOG === 'true') process.exit(1);
else fs.writeFileSync(args.at(-1), JSON.stringify({jobId: args[2], status: 'Invalid'}));
`);
    chmodSync(xcrun, 0o755);
    const workflow = readFileSync(new URL("../.github/workflows/apple-notary-log.yml", import.meta.url), "utf8");
    const script = workflow.split("        run: |\n")[1].split("\n      - name:")[0]
      .split("\n").map((line) => line.replace(/^          /, "")).join("\n");
    const result = spawnSync("bash", ["-c", script], { encoding: "utf8", env: {
      ...process.env, PATH: `${root}:${process.env.PATH}`, RUNNER_TEMP: root,
      SUBMISSION_ID: scenario === "invalid-id" ? "$(touch should-not-exist)" : id,
      APPLE_API_KEY_P8: Buffer.from("fixture-key").toString("base64"),
      APPLE_API_KEY_ID: "fixture-id", APPLE_API_ISSUER_ID: "fixture-issuer",
      FAIL_LOG: String(scenario === "log-failure"),
    } });
    assert.equal(result.status, scenario === "success" ? 0 : 1, result.stderr);
    assert.ok(!readdirSync(root).some((name) => name.startsWith("apple-notary-key.")));
    assert.doesNotMatch(result.stdout + result.stderr, /fixture-key/);
    if (scenario === "invalid-id") assert.deepEqual(readdirSync(root), ["xcrun"]);
    else assert.equal(readFileSync(join(root, "calls"), "utf8"), "info\nlog\n");
    if (scenario === "success") {
      assert.equal(JSON.parse(readFileSync(join(root, "apple-notarization/notary-log.json"))).status, "Invalid");
    }
  });
}
