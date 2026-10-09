import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const dir = path.dirname(fileURLToPath(import.meta.url));
const indexScript = path.join(dir, "update-versions-index.sh");
const backfill = path.join(dir, "backfill-studio-notes.sh");
const workflow = readFileSync(path.join(dir, "..", ".github", "workflows", "release-studio.yml"), "utf8");

function index(existing, ...args) {
  const run = spawnSync("bash", [indexScript, existing, ...args], { encoding: "utf8" });
  return { status: run.status, stderr: run.stderr, json: run.status === 0 ? JSON.parse(run.stdout) : null };
}

const NOTES = "https://dl.reasonix.io/studio/notes/2.32.0.md";

test("an entry carries notes only when a notes address is given", () => {
  const withNotes = index("-", "v2.32.0", "studio-v2.32.0", "studio", "2026-10-09T00:00:00Z", "20", NOTES);
  assert.equal(withNotes.json.versions[0].notes, NOTES);
  const without = index("-", "v2.32.0", "studio-v2.32.0", "studio", "2026-10-09T00:00:00Z");
  assert.equal("notes" in without.json.versions[0], false);
});

test("a notes address outside the notes prefix is refused", () => {
  for (const bad of ["https://example.com/2.32.0.md", "https://dl.reasonix.io/studio-v2.32.0/latest.json"]) {
    const run = index("-", "v2.32.0", "studio-v2.32.0", "studio", "2026-10-09T00:00:00Z", "20", bad);
    assert.notEqual(run.status, 0);
  }
});

test("a rerun never rewrites an existing entry, notes included", () => {
  const first = index("-", "v2.32.0", "studio-v2.32.0", "studio", "2026-10-09T00:00:00Z");
  const dirPath = mkdtempSync(path.join(tmpdir(), "idx-"));
  const file = path.join(dirPath, "c.json");
  writeFileSync(file, JSON.stringify(first.json));
  const again = index(file, "v2.32.0", "studio-v2.32.0", "studio", "2026-10-10T00:00:00Z", "20", NOTES);
  assert.equal(again.json.versions.length, 1);
  assert.equal("notes" in again.json.versions[0], false);
});

test("the release uploads the notes object before it writes the catalog that names it", () => {
  const step = workflow.slice(workflow.indexOf("Mirror to R2 and update the Studio catalog"));
  const at = (needle) => {
    const i = step.indexOf(needle);
    assert.notEqual(i, -1, needle);
    return i;
  };
  const candidate = at('if [ "$CANDIDATE" = "true" ]');
  const upload = at('"s3://${R2_BUCKET}/${notes_key}"');
  const verify = at("head-object");
  const merge = at("scripts/update-versions-index.sh");
  const catalog = at('"s3://${R2_BUCKET}/studio/versions.json" \\\n            --endpoint-url "$endpoint" \\\n            --content-type');
  assert.ok(candidate < upload && upload < verify && verify < merge && merge < catalog);
  assert.match(step.slice(merge, catalog), /\$\{notes_arg\[@\]\}/);
});

// Runs the workflow's own notes block with aws stubbed, so what is tested is
// the text that ships rather than a copy of it.
function notesBlock(awsBody) {
  const step = workflow.slice(workflow.indexOf("Mirror to R2 and update the Studio catalog"));
  const from = step.indexOf('notes_key="studio/notes/');
  const to = step.indexOf("# Studio's own catalog.");
  const block = step.slice(from, to).replace(/^ {10}/gm, "");
  const root = mkdtempSync(path.join(tmpdir(), "nb-"));
  writeFileSync(path.join(root, "aws"), `#!/bin/sh\n${awsBody}\n`);
  chmodSync(path.join(root, "aws"), 0o755);
  const script = `set -e\nVERSION=v2.32.0 R2_BUCKET=b RUNNER_TEMP=/tmp endpoint=e\n${block}\necho "ARGS=\${notes_arg[*]}"`;
  return spawnSync("bash", ["-c", script], { encoding: "utf8", env: { ...process.env, PATH: `${root}:${process.env.PATH}` } });
}

test("a failed notes upload warns and leaves the catalog entry without a pointer, and the step goes on", () => {
  const run = notesBlock("exit 1");
  assert.equal(run.status, 0, run.stderr);
  assert.match(run.stdout, /::warning::release notes for v2\.32\.0 were not uploaded/);
  assert.match(run.stdout, /ARGS=$/m);
});

test("a notes object the mirror cannot confirm gets no pointer either", () => {
  const run = notesBlock('[ "$1" = s3api ] && exit 1; exit 0');
  assert.equal(run.status, 0, run.stderr);
  assert.match(run.stdout, /::warning::/);
  assert.match(run.stdout, /ARGS=$/m);
});

test("only a confirmed upload produces the notes pointer", () => {
  const run = notesBlock("exit 0");
  assert.equal(run.status, 0, run.stderr);
  assert.doesNotMatch(run.stdout, /::warning::/);
  assert.match(run.stdout, /ARGS=20 https:\/\/dl\.reasonix\.io\/studio\/notes\/2\.32\.0\.md$/m);
});

function sandbox({ catalog, bucket = {}, busy = "0", tags }) {
  const root = mkdtempSync(path.join(tmpdir(), "bf-"));
  const store = path.join(root, "bucket");
  const bin = path.join(root, "bin");
  const repo = path.join(root, "repo");
  mkdirSync(store, { recursive: true });
  mkdirSync(bin);
  mkdirSync(repo);
  const put = (key, body) => {
    mkdirSync(path.dirname(path.join(store, key)), { recursive: true });
    writeFileSync(path.join(store, key), body);
  };
  put("studio/versions.json", JSON.stringify(catalog));
  for (const [key, body] of Object.entries(bucket)) put(key, body);
  const shim = (name, body) => {
    writeFileSync(path.join(bin, name), `#!/bin/sh\n${body}\n`);
    chmodSync(path.join(bin, name), 0o755);
  };
  shim("aws", `
log="$STORE/../calls.log"; echo "$@" >> "$log"
if [ "$1" = s3 ] && [ "$2" = cp ]; then
  strip() { echo "$1" | sed "s|^s3://[^/]*/||"; }
  case "$3" in s3://*) cp "$STORE/$(strip "$3")" "$4" ;; *) mkdir -p "$(dirname "$STORE/$(strip "$4")")"; cp "$3" "$STORE/$(strip "$4")" ;; esac
  exit $?
fi
if [ "$1" = s3api ]; then
  while [ $# -gt 0 ]; do [ "$1" = --key ] && key="$2"; shift; done
  [ -f "$STORE/$key" ]; exit $?
fi
exit 3`);
  shim("gh", `echo "${busy}"`);
  shim("node", `cp "$2" "$3"; printf '\\nrendered\\n' >> "$3"`);
  const git = (...a) => spawnSync("git", ["-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", ...a], { encoding: "utf8" });
  git("init", "-q");
  for (const v of tags) {
    mkdirSync(path.join(repo, "release-notes", "studio"), { recursive: true });
    writeFileSync(path.join(repo, "release-notes", "studio", `${v}.md`), `notes ${v}\n`);
    git("add", ".");
    git("commit", "-q", "-m", v);
    git("tag", `studio-v${v}`);
  }
  const run = (...args) =>
    spawnSync("bash", [backfill, ...args], {
      encoding: "utf8",
      env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, STORE: store, BACKFILL_REPO: repo, R2_ACCOUNT_ID: "acct", R2_BUCKET: "bkt" },
    });
  const read = (key) => (existsSync(path.join(store, key)) ? readFileSync(path.join(store, key), "utf8") : null);
  return { run, read, calls: () => (existsSync(path.join(root, "calls.log")) ? readFileSync(path.join(root, "calls.log"), "utf8") : "") };
}

const entry = (v, extra = {}) => ({ version: `v${v}`, tag: `studio-v${v}`, channel: "studio", publishedAt: "2026-10-01T00:00:00Z", manifest: `https://dl.reasonix.io/studio-v${v}/latest.json`, ...extra });
const catalog = () => ({ schemaVersion: 1, updatedAt: "x", versions: [entry("2.3.0"), entry("2.2.0"), entry("2.1.0", { notes: "https://dl.reasonix.io/studio/notes/2.1.0.md" })] });

test("the backfill is a dry run unless asked, and uploads and writes nothing", () => {
  const s = sandbox({ catalog: catalog(), tags: ["2.3.0"] });
  const before = s.read("studio/versions.json");
  const run = s.run();
  assert.equal(run.status, 0, run.stderr);
  assert.match(run.stdout, /would upload 2\.3\.0/);
  assert.match(run.stdout, /no notes 2\.2\.0/);
  assert.equal(s.read("studio/notes/2.3.0.md"), null);
  assert.equal(s.read("studio/versions.json"), before);
  assert.doesNotMatch(s.calls(), /cp \/\S+ s3:\/\//);
});

test("apply uploads the missing notes, skips what exists, and leaves a version without a file bare", () => {
  const s = sandbox({ catalog: catalog(), bucket: { "studio/notes/2.2.0.md": "kept\n" }, tags: ["2.3.0", "2.2.0"] });
  const run = s.run("--apply");
  assert.equal(run.status, 0, run.stderr);
  assert.match(s.read("studio/notes/2.3.0.md"), /rendered/);
  assert.equal(s.read("studio/notes/2.2.0.md"), "kept\n");
  const next = JSON.parse(s.read("studio/versions.json"));
  const byVersion = Object.fromEntries(next.versions.map((e) => [e.version, e]));
  assert.equal(byVersion["v2.3.0"].notes, "https://dl.reasonix.io/studio/notes/2.3.0.md");
  assert.equal(byVersion["v2.2.0"].notes, "https://dl.reasonix.io/studio/notes/2.2.0.md");
  assert.equal(byVersion["v2.1.0"].notes, "https://dl.reasonix.io/studio/notes/2.1.0.md");
});

test("a version with no notes file gets no field", () => {
  const s = sandbox({ catalog: catalog(), tags: ["2.3.0"] });
  assert.equal(s.run("--apply").status, 0);
  const next = JSON.parse(s.read("studio/versions.json"));
  assert.equal("notes" in next.versions.find((e) => e.version === "v2.2.0"), false);
});

test("apply refuses while a release run is in progress and touches nothing", () => {
  const s = sandbox({ catalog: catalog(), tags: ["2.3.0"], busy: "1" });
  const before = s.read("studio/versions.json");
  const run = s.run("--apply");
  assert.notEqual(run.status, 0);
  assert.match(run.stderr, /in progress/);
  assert.equal(s.read("studio/notes/2.3.0.md"), null);
  assert.equal(s.read("studio/versions.json"), before);
});
