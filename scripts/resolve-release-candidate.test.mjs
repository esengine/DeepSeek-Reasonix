import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { inspectRecord, requireNotRevoked, selectRecordArtifact, validateCandidateRun } from "./resolve-release-candidate.mjs";

const id = `v1.2.3-${"a".repeat(12)}-${"b".repeat(12)}`;
const artifact = { id: 22, name: `release-candidate-record-${id}`, expired: false, workflow_run: { id: 11 } };
const run = { id: 11, run_attempt: 2, repository: { full_name: "esengine/DeepSeek-Reasonix" }, path: ".github/workflows/release-candidate.yml", head_branch: "main-v2", head_sha: "c".repeat(40), event: "workflow_dispatch", status: "completed", conclusion: "success" };
const record = { candidateId: id, version: "1.2.3", sourceSHA: "a".repeat(40), control: { buildSHA: run.head_sha }, signing: { desktopFingerprint: "v1:example" }, validity: { createdAt: "2026-01-01T00:00:00Z", expiresAt: "2027-01-01T00:00:00Z", revoked: false }, source: { runId: "11", runAttempt: "2", desktopPrefix: "desktop-11-2-preflight", payloadArtifactId: "33", payloadArtifactName: `release-candidate-payload-${id}`, evidenceArtifactId: "34", evidenceArtifactName: `release-candidate-evidence-${id}` } };

for (const purpose of ["release", "rehearsal"]) {
  test(`${purpose} CLI writes the exact workflow output contract`, t => {
    const root = mkdtempSync(path.join(tmpdir(), "reasonix-candidate-outputs-"));
    t.after(() => rmSync(root, { recursive: true, force: true }));
    const namespace = purpose === "release" ? "release-candidate" : "release-candidate-rehearsal";
    const payloadName = `${namespace}-payload-${id}`;
    const evidenceName = `${namespace}-evidence-${id}`;
    const sealed = { ...record, purpose,
      validity: { ...record.validity, expiresAt: "2099-01-01T00:00:00Z" },
      source: { ...record.source, payloadArtifactName: payloadName, evidenceArtifactName: evidenceName },
    };
    const inputs = [sealed, { ...artifact, name: `${namespace}-record-${id}` }, run].map((value, index) => {
      const file = path.join(root, `${index}.json`);
      writeFileSync(file, JSON.stringify(value));
      return file;
    });
    const output = path.join(root, "outputs");
    const result = spawnSync(process.execPath, [fileURLToPath(new URL("./resolve-release-candidate.mjs", import.meta.url)),
      purpose === "release" ? "inspect" : "inspect-rehearsal", id, ...inputs],
    { encoding: "utf8", env: { ...process.env, GITHUB_OUTPUT: output } });
    assert.equal(result.status, 0, result.stderr);
    const values = Object.fromEntries(readFileSync(output, "utf8").trim().split("\n").map(line => {
      const separator = line.indexOf("=");
      return [line.slice(0, separator), line.slice(separator + 1)];
    }));
    assert.deepEqual(values, {
      candidate_id: id, version: "1.2.3", source_sha: record.sourceSHA,
      candidate_control_sha: run.head_sha, signing_fingerprint: "v1:example",
      producer_run_id: "11", producer_run_attempt: "2", desktop_prefix: "desktop-11-2-preflight",
      payload_artifact_id: "33", payload_artifact_name: payloadName,
      evidence_artifact_id: "34", evidence_artifact_name: evidenceName,
    });
  });
}

test("rejects missing or malformed source/control identities before emitting outputs", () => {
  for (const sourceSHA of [undefined, "", "main-v2"]) {
    assert.throws(() => inspectRecord({ ...record, sourceSHA }, id, artifact, run), /source SHA/);
  }
  for (const buildSHA of [undefined, "", "main-v2"]) {
    assert.throws(() => inspectRecord({ ...record, control: { buildSHA } }, id, artifact, run), /control SHA/);
  }
});

test("selects the newest active exact-name record", () => {
  assert.equal(selectRecordArtifact([{ ...artifact, id: 20 }, artifact, { ...artifact, id: 30, expired: true }], id).id, 22);
  assert.equal(selectRecordArtifact([], id, false), null);
  assert.throws(() => selectRecordArtifact([], id), /not found/);
});

test("accepts a successful protected producer and exact record", () => {
  assert.doesNotThrow(() => validateCandidateRun(run, artifact, "esengine/DeepSeek-Reasonix"));
  const inspected = inspectRecord(record, id, artifact, run);
  assert.equal(inspected.payloadArtifactId, "33");
  assert.equal(inspected.evidenceArtifactId, "34");
});

test("a rerun can seal Desktop artifacts built by an earlier attempt of the same producer", () => {
  const reused = { ...record, source: { ...record.source, desktopPrefix: "desktop-11-1-preflight" } };
  assert.equal(inspectRecord(reused, id, artifact, run).desktopPrefix, "desktop-11-1-preflight");
  for (const desktopPrefix of ["desktop-12-1-preflight", "desktop-11-3-preflight", "desktop-11-0-preflight", "desktop-11-1-other"]) {
    assert.throws(() => inspectRecord({ ...record, source: { ...record.source, desktopPrefix } }, id, artifact, run), /Desktop prefix/);
  }
});

test("Desktop release contract accepts only the sealed producer's current or earlier build attempt", () => {
  const script = fileURLToPath(new URL("./check-candidate-desktop-prefix.sh", import.meta.url));
  for (const prefix of ["desktop-11-1-preflight", "desktop-11-2-preflight"]) {
    assert.equal(spawnSync("bash", [script, prefix, "11", "2"]).status, 0, prefix);
  }
  for (const prefix of ["desktop-12-1-preflight", "desktop-11-3-preflight", "desktop-11-0-preflight", "desktop-11-1-other"]) {
    assert.notEqual(spawnSync("bash", [script, prefix, "11", "2"]).status, 0, prefix);
  }
});

test("rehearsal records cannot be selected or inspected for publication", () => {
  const rehearsalArtifact = { ...artifact, name: `release-candidate-rehearsal-record-${id}` };
  const rehearsalRecord = {
    ...record, purpose: "rehearsal",
    source: {
      ...record.source,
      payloadArtifactName: `release-candidate-rehearsal-payload-${id}`,
      evidenceArtifactName: `release-candidate-rehearsal-evidence-${id}`,
    },
  };
  assert.equal(selectRecordArtifact([rehearsalArtifact], id, false), null);
  assert.equal(selectRecordArtifact([rehearsalArtifact], id, true, "rehearsal"), rehearsalArtifact);
  assert.throws(() => inspectRecord(rehearsalRecord, id, rehearsalArtifact, run), /purpose mismatch/);
  assert.throws(() => inspectRecord(rehearsalRecord, id, artifact, run, new Date(), "rehearsal"), /artifact identity/);
  assert.equal(inspectRecord(rehearsalRecord, id, rehearsalArtifact, run, new Date(), "rehearsal").payloadArtifactId, "33");
});

test("accepts automatic preparation after the reviewed Notes PR merges", () => {
  assert.doesNotThrow(() => validateCandidateRun({ ...run, event: "push" }, artifact, "esengine/DeepSeek-Reasonix"));
});

for (const change of [
  { path: ".github/workflows/other.yml" }, { head_branch: "topic" }, { event: "pull_request" },
  { conclusion: "failure" }, { repository: { full_name: "fork/Reasonix" } },
]) {
  test(`rejects untrusted producer ${JSON.stringify(change)}`, () => {
    assert.throws(() => validateCandidateRun({ ...run, ...change }, artifact, "esengine/DeepSeek-Reasonix"));
  });
}

test("rejects a payload artifact substituted after sealing", () => {
  assert.throws(() => inspectRecord({ ...record, source: { ...record.source, payloadArtifactId: "" } }, id, artifact, run));
});

test("rejects expired or record-revoked candidates before payload reuse", () => {
  assert.throws(() => inspectRecord(record, id, artifact, run, new Date("2027-01-02T00:00:00Z")), /expired/);
  assert.throws(() => inspectRecord({ ...record, validity: { ...record.validity, revoked: true } }, id, artifact, run,
    new Date("2026-01-02T00:00:00Z")), /revoked/);
});

test("rejects a candidate on the repository revocation list", () => {
  assert.throws(() => requireNotRevoked(id, `v9.9.9-${"c".repeat(12)}-${"d".repeat(12)}, ${id}`), /is revoked/);
  assert.doesNotThrow(() => requireNotRevoked(id, ""));
});
