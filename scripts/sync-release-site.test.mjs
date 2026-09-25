import assert from "node:assert/strict";
import test from "node:test";
import { mkdtempSync, mkdirSync, copyFileSync, writeFileSync, readFileSync, rmSync, realpathSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { createCoreLedger, createSiteLedger } from "./release-publication-ledger.mjs";

const sha = "a".repeat(40);
function fixture(t) {
  const root = realpathSync(mkdtempSync(path.join(tmpdir(), "release-site-sync-")));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const scripts = path.join(root, "scripts"), bin = path.join(root, "bin");
  mkdirSync(scripts); mkdirSync(bin);
  for (const file of ["sync-release-site.sh", "observe-release-site.sh", "fetch-stable-release-manifest.sh", "check-release-public-access.sh", "release-publication-ledger.mjs"]) copyFileSync(`scripts/${file}`, path.join(scripts, file));
  const release = { isDraft: false, isPrerelease: false, assets: [] };
  writeFileSync(path.join(root, "core.json"), JSON.stringify(createCoreLedger({ version: "1.2.3", sourceSHA: sha, operation: "recover", cliRelease: release, desktopRelease: release })));
  writeFileSync(path.join(root, "site.json"), JSON.stringify(createSiteLedger({ version: "1.2.3", sourceSHA: sha, operation: "recover", manifest: { version: "v1.2.3" } })));
  writeFileSync(path.join(scripts, "verify-stable-release-artifacts.sh"), `#!/usr/bin/env bash
set -euo pipefail
if [ "$VERIFY_PUBLIC_SITE_ONLY" = true ]; then
  test "\${SITE_FAIL:-}" != true
  cp "$FIXTURE/site.json" "$RELEASE_LEDGER_OUTPUT"
else
  test "\${CORE_FAIL:-}" != true
  cp "$FIXTURE/core.json" "$RELEASE_LEDGER_OUTPUT"
fi
`);
  writeFileSync(path.join(scripts, "release-event.mjs"), `import {writeFileSync} from 'node:fs'; writeFileSync(process.argv[process.argv.indexOf('--output')+1], '{}');`);
  writeFileSync(path.join(bin, "go"), `#!/usr/bin/env node
const args=process.argv.slice(2);
if (args[0]!=='run' || !args[1].endsWith('/release-manifest-fetch/main.go') || args[2]!=='1.2.3') process.exit(99);
if(process.env.HTTP_FAIL==='true') process.exit(22);
const body=process.env.MANIFEST_BODY || JSON.stringify({version:process.env.POINTER || 'v1.2.3'});
try { if(!/^v[0-9]+\\.[0-9]+\\.[0-9]+$/.test(JSON.parse(body).version)) process.exit(1); }
catch { process.exit(1); }
require('node:fs').writeFileSync(args[3],body);
`, { mode: 0o755 });
  writeFileSync(path.join(bin, "gh"), `#!/usr/bin/env node
const fs=require('node:fs'), path=require('node:path');
const args=process.argv.slice(2), root=process.env.FIXTURE;
fs.appendFileSync(path.join(root,'calls'),JSON.stringify(args)+'\\n');
if(args[0]==='api' && args.includes('POST')) {
  const body=JSON.parse(fs.readFileSync(0,'utf8'));
  if(body.ref!=='main-v2' || body.inputs.release_version!=='1.2.3' || body.inputs.release_request!=='release-123-2') process.exit(3);
  fs.writeFileSync(path.join(root,'request'),body.inputs.release_request);
} else if(args[0]==='api') console.log('2026-01-01T00:00:00Z');
else if(args[0]==='release' && args[1]==='view') console.log(process.env.MISSING_EVENT==='true'?'0':'1');
else if(args[0]==='release' && args[1]==='download') {
  if(process.env.EVENT_DOWNLOAD_FAIL==='true') process.exit(1);
  fs.writeFileSync(args[args.indexOf('--output')+1],process.env.EVENT_CONFLICT==='true'?'conflict':'{}');
} else if(args[0]==='release' && args[1]==='upload') {
  if(path.basename(args.at(-1))!=='release-event.json') process.exit(3);
} else if(args[0]==='run' && args[1]==='list') {
  const title='Deploy site v1.2.3 ['+fs.readFileSync(path.join(root,'request'),'utf8')+']';
  const runs=[{databaseId:900,displayTitle:'Deploy site v1.2.3 [unrelated]'},{databaseId:901,displayTitle:title}];
  if(process.env.AMBIGUOUS==='true') runs.push({databaseId:902,displayTitle:title});
  console.log(JSON.stringify(runs));
} else if(args[0]==='run' && args[1]==='watch') {
  if(args[2]!=='901' || process.env.PAGES_FAIL==='true') process.exit(1);
} else if(args[0]==='run' && args[1]==='view') console.log(process.env.TERMINAL_FAIL==='true'?'failure':'success');
else process.exit(4);
`, { mode: 0o755 });
  const env = { ...process.env, FIXTURE: root, PATH: `${bin}:${process.env.PATH}`, GITHUB_ACTIONS: "true", GITHUB_REPOSITORY: "esengine/DeepSeek-Reasonix", GITHUB_REF: "refs/heads/main-v2", GITHUB_REF_PROTECTED: "true", GITHUB_RUN_ID: "123", GITHUB_RUN_ATTEMPT: "2", RELEASE_REPOSITORY: "esengine/DeepSeek-Reasonix", RELEASE_VERSION: "1.2.3", RELEASE_OPERATION: "recover", RELEASE_EXPECTED_SHA: sha, RELEASE_LEDGER_OUTPUT: path.join(root, "ledger.json"), RELEASE_SITE_RECOVERY_ONLY: "true" };
  return { root, scripts, env,
    run: extra => spawnSync("bash", [path.join(scripts, "sync-release-site.sh")], { env: { ...env, ...extra }, encoding: "utf8" }),
    ledger: () => JSON.parse(readFileSync(env.RELEASE_LEDGER_OUTPUT, "utf8")),
    calls: () => { try { return readFileSync(path.join(root, "calls"), "utf8"); } catch { return ""; } },
  };
}

test("site recovery correlates its own Pages run and merges all six observed surfaces", t => {
  const f = fixture(t), result = f.run({});
  assert.equal(result.status, 0, result.stderr);
  const ledger = f.ledger();
  assert.equal(ledger.completionState, "complete");
  assert.equal(Object.keys(ledger.surfaces).length, 6);
  assert.equal(ledger.verificationContext.pagesRunId, "901");
  assert.doesNotMatch(f.calls(), /"(?:upload|publish|push)"/);
});

test("newer pointer is preserved without deploying", t => {
  const f = fixture(t), result = f.run({ POINTER: "v1.2.4" });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(f.ledger().completionState, "immutable-complete-newer-pointer-preserved");
  assert.doesNotMatch(f.calls(), /POST/);
});

for (const [extra, stage] of [
  [{ CORE_FAIL: "true" }, "immutable"],
  [{ MISSING_EVENT: "true" }, "release-event"],
  [{ EVENT_DOWNLOAD_FAIL: "true" }, "release-event"],
  [{ EVENT_CONFLICT: "true" }, "release-event"],
  [{ HTTP_FAIL: "true" }, "ownership"],
  [{ MANIFEST_BODY: "<html>challenge</html>" }, "ownership"],
  [{ POINTER: "v1.2.2" }, "ownership"],
  [{ AMBIGUOUS: "true" }, "pages-dispatch"],
  [{ PAGES_FAIL: "true" }, "pages"],
  [{ TERMINAL_FAIL: "true" }, "pages"],
  [{ SITE_FAIL: "true" }, "public-site"],
]) test(`failure evidence survives ${JSON.stringify(extra)}`, t => {
  const f = fixture(t), result = f.run(extra);
  assert.notEqual(result.status, 0);
  const ledger = f.ledger();
  assert.equal(ledger.verificationContext.stage, stage);
  assert.equal(ledger.completionState, stage === "immutable" ? "verification-failed" : "public-sync-pending");
  assert.doesNotMatch(f.calls(), /upload/);
  if (["immutable", "release-event", "ownership"].includes(stage)) assert.doesNotMatch(f.calls(), /POST/);
});

test("full recovery may create the missing event with the immutable asset filename", t => {
  const f = fixture(t), result = f.run({ MISSING_EVENT: "true", RELEASE_SITE_RECOVERY_ONLY: "false" });
  assert.equal(result.status, 0, result.stderr);
  assert.match(f.calls(), /release-event\.json/);
  assert.match(f.calls(), /upload/);
});

test("public access preflight accepts the previous stable version but rejects challenges and invalid JSON", t => {
  const f = fixture(t);
  for (const [extra, success] of [[{ POINTER: "v1.0.0" }, true], [{ HTTP_FAIL: "true" }, false], [{ MANIFEST_BODY: "{}" }, false], [{ MANIFEST_BODY: "html" }, false]]) {
    const result = spawnSync("bash", [path.join(f.scripts, "check-release-public-access.sh"), "1.2.3"], { env: { ...f.env, ...extra }, encoding: "utf8" });
    assert.equal(result.status === 0, success, result.stderr);
  }
});

test("real public verifier rejects tag drift before inspecting any release", t => {
  const f = fixture(t);
  writeFileSync(path.join(f.root, "bin/git"), `#!/usr/bin/env node\nconsole.log('${"b".repeat(40)}\\trefs/tags/v1.2.3');`, { mode: 0o755 });
  const result = spawnSync("bash", ["scripts/verify-stable-release-artifacts.sh"], { env: { ...f.env, CLI_TAG: "v1.2.3", DESKTOP_TAG: "desktop-v1.2.3" }, encoding: "utf8" });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /differs from the verified source SHA/);
  assert.equal(f.calls(), "");
});

test("recovery and publication share one owner and recovery cannot enter a publisher", () => {
  const recovery = readFileSync(".github/workflows/release-site-recovery.yml", "utf8");
  const promote = readFileSync(".github/workflows/release-promote.yml", "utf8");
  assert.match(readFileSync(".github/workflows/release-stable.yml", "utf8"), /group: stable-release-publication/);
  for (const workflow of [recovery, promote]) {
    assert.match(workflow, /group: stable-release-publication/);
    assert.match(workflow, /bash scripts\/sync-release-site.sh/);
    assert.match(workflow, /retention-days: 90/);
    assert.match(workflow, /bash scripts\/check-release-public-access.sh/);
  }
  assert.match(recovery, /RELEASE_SITE_RECOVERY_ONLY: "true"/);
  assert.doesNotMatch(recovery, /secrets: inherit|release-candidate-tags.sh activate|contents: write|uses: .*release-(desktop|npm)\.yml/);
  assert.equal((recovery.match(/environment: release/g) || []).length, 1);
  const pages = readFileSync(".github/workflows/pages.yml", "utf8");
  assert.ok(pages.indexOf("Recheck release ownership") < pages.indexOf("uses: actions/deploy-pages"));
});
