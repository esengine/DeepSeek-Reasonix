import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import vm from "node:vm";
import test from "node:test";

const workflow = name => readFileSync(new URL(`../.github/workflows/${name}.yml`, import.meta.url), "utf8");
function job(source, name) {
  const body = source.match(new RegExp(`\\n  ${name}:\\n([\\s\\S]*?)(?=\\n  [a-z][a-z0-9-]*:|$)`))?.[1];
  assert.ok(body, name);
  return body;
}
function condition(body, context) {
  const expression = body.match(/^    if: (.+)$/m)[1].replace(/^\$\{\{\s*|\s*\}\}$/g, "")
    .replace(/needs\.([a-z][a-z0-9-]*)/g, 'needs["$1"]');
  return vm.runInNewContext(expression, { always: () => true, cancelled: () => false, ...context });
}
function shellStep(body, name) {
  return body.split(`      - name: ${name}\n`)[1].match(/        run: \|\n((?:          .*\n|\n)+)/)[1]
    .replace(/^          /gm, "");
}
const ci = workflow("ci");
const release = workflow("release-desktop");

test("macOS signing diagnostics require protected main and cannot publish", () => {
  const source = workflow("macos-signing-check");
  const verify = job(source, "verify");
  const github = { repository: "esengine/DeepSeek-Reasonix", ref: "refs/heads/main-v2", ref_protected: true };
  assert.equal(condition(verify, { github }), true);
  for (const changed of [{ repository: "fork/Reasonix" }, { ref: "refs/tags/v1.0.0" }, { ref_protected: false }]) {
    assert.equal(condition(verify, { github: { ...github, ...changed } }), false);
  }
  assert.match(verify, /environment: release/);
  assert.match(verify, /ref: \$\{\{ github.sha \}\}/);
  assert.match(source, /permissions:\n  contents: read\n/);
  assert.doesNotMatch(source, /: write|secrets\.(R2_|SIGNPATH_|MINISIGN_|NPM_)/);
  assert.match(verify, /HAS_APPLE_CERT: "true"/);
  assert.match(verify, /scripts\/desktop-build.sh darwin\/universal v0.0.0-signing-check stable/);
  assert.match(verify, /path: \$\{\{ runner.temp \}\}\/apple-notarization\/\*\.json/);
  assert.match(verify, /if: always\(\)/);
});

test("required desktop aggregate rejects every failed, cancelled or unexpectedly skipped child", () => {
  const script = shellStep(job(ci, "desktop"), "Verify desktop validation jobs");
  const success = { CHANGES_RESULT: "success", SHOULD_RUN: "true", PREPARE_RESULT: "success", GO_RESULT: "success", FRONTEND_RESULT: "success", BROWSER_RESULT: "success" };
  const run = env => spawnSync("bash", ["-e", "-c", script], { env: { ...process.env, ...env } }).status;
  assert.equal(run(success), 0);
  for (const key of ["PREPARE_RESULT", "GO_RESULT", "FRONTEND_RESULT", "BROWSER_RESULT", "CHANGES_RESULT"]) {
    for (const value of ["failure", "cancelled", "skipped", ""]) assert.notEqual(run({ ...success, [key]: value }), 0, `${key}=${value}`);
  }
  assert.equal(run({ ...success, SHOULD_RUN: "false", PREPARE_RESULT: "skipped", GO_RESULT: "skipped", FRONTEND_RESULT: "skipped", BROWSER_RESULT: "skipped" }), 0);
  assert.notEqual(run({ ...success, SHOULD_RUN: "false" }), 0);
});

test("reuse skips only build work and still gates every publisher on validation", () => {
  const context = {
    inputs: { preflight_artifact_prefix: "desktop-123-1-preflight", orchestrated: true, signing_preflight_verified: true, signing_preflight: false, production_signing_smoke: false },
    needs: { resolve: { result: "success" }, "cache-guard": { result: "success" }, "signing-contract": { result: "success" }, build: { result: "skipped" } },
  };
  assert.equal(condition(job(release, "build"), context), false);
  assert.equal(condition(job(release, "publish"), context), true);
  for (const key of ["resolve", "cache-guard", "signing-contract", "build"]) {
    for (const result of ["failure", "cancelled"]) {
      const changed = structuredClone(context);
      changed.needs[key].result = result;
      assert.equal(condition(job(release, "publish"), changed), false, `${key}=${result}`);
    }
  }
  for (const key of ["orchestrated", "signing_preflight_verified"]) {
    assert.equal(condition(job(release, "publish"), { ...context, inputs: { ...context.inputs, [key]: false } }), false);
  }
  for (const key of ["signing_preflight", "production_signing_smoke"]) {
    assert.equal(condition(job(release, "publish"), { ...context, inputs: { ...context.inputs, [key]: true } }), false);
  }
  const fresh = structuredClone(context);
  fresh.inputs.preflight_artifact_prefix = "";
  assert.equal(condition(job(release, "build"), fresh), true);
  assert.equal(condition(job(release, "publish"), fresh), false);
  fresh.needs.build.result = "success";
  assert.equal(condition(job(release, "publish"), fresh), true);
});

test("reuse never moves artifact verification past public mutation or trusts candidate scripts", () => {
  const publisher = job(release, "publish");
  assert.ok(publisher.indexOf("Verify complete signed artifact handoff") < publisher.indexOf("name: Publish GitHub release"));
  assert.ok(publisher.includes("node release-control/scripts/desktop-release-artifacts.mjs collect"));
  assert.ok(publisher.includes("ref: ${{ github.workflow_sha }}"));
  assert.ok(!publisher.includes("merge-multiple: true"));
  const stable = workflow("release-stable");
  assert.ok(job(stable, "desktop").includes("preflight_artifact_prefix: ${{ needs.signpath-preflight.outputs.artifact_prefix }}"));
  for (const name of ["desktop", "cli", "npm"]) assert.ok(job(stable, name).includes("needs: [authorize, signpath-preflight]"));
});

test("all Linux consumers use the prepared build and reject a failed preparation", () => {
  const context = { github: { event_name: "pull_request" },
    needs: { changes: { outputs: { desktop: "true" } }, "desktop-prepare": { result: "success" } } };
  const aggregate = job(ci, "desktop");
  for (const name of ["desktop-go", "desktop-frontend", "desktop-browser"]) {
    const body = job(ci, name);
    assert.ok(aggregate.includes(name));
    assert.ok(body.includes("needs: [changes, desktop-prepare]"));
    assert.ok(body.includes("name: ${{ needs.desktop-prepare.outputs.artifact_name }}"));
    assert.ok(!body.includes("pnpm --dir frontend build"));
    assert.equal(condition(body, context), true);
    assert.equal(condition(body, { ...context, needs: { ...context.needs, "desktop-prepare": { result: "failure" } } }), false);
  }
});
