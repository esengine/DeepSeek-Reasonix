import { readFileSync } from "node:fs";
import { appendFileSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { artifactNamespace } from "./release-candidate.mjs";

const ID_RE = /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-[0-9a-f]{12}-[0-9a-f]{12}$/;

export function requireNotRevoked(candidateId, value = "") {
  const revoked = new Set(String(value).split(/[\s,]+/).filter(Boolean));
  if (revoked.has(candidateId)) throw new Error(`release candidate is revoked: ${candidateId}`);
}

export function selectRecordArtifact(artifacts, candidateId, required = true, purpose = "release") {
  if (!ID_RE.test(candidateId)) throw new Error("invalid release candidate id");
  const name = `${artifactNamespace(purpose)}-record-${candidateId}`;
  const matches = artifacts
    .filter(item => item.name === name && item.expired === false)
    .sort((a, b) => Number(b.id) - Number(a.id));
  if (matches.length === 0) {
    if (!required) return null;
    throw new Error(`active candidate record artifact not found: ${candidateId}`);
  }
  return matches[0];
}

export function validateCandidateRun(run, recordArtifact, repository) {
  if (run.id !== recordArtifact.workflow_run?.id) throw new Error("candidate artifact run identity mismatch");
  if (run.repository?.full_name !== repository || run.path !== ".github/workflows/release-candidate.yml") {
    throw new Error("candidate artifact was not produced by the protected candidate workflow");
  }
  if (run.head_branch !== "main-v2" || !["workflow_dispatch", "push"].includes(run.event) || run.status !== "completed" || run.conclusion !== "success") {
    throw new Error("candidate producer run is not a successful protected main-v2 dispatch");
  }
}

export function inspectRecord(record, candidateId, recordArtifact, run, now = new Date(), purpose = "release") {
  const namespace = artifactNamespace(purpose);
  if ((record.purpose ?? "release") !== purpose) throw new Error("candidate purpose mismatch; rehearsal cannot be published");
  if (recordArtifact.name !== `${namespace}-record-${candidateId}` || recordArtifact.expired !== false) {
    throw new Error("candidate record artifact identity mismatch");
  }
  if (record.candidateId !== candidateId) throw new Error("candidate record identity mismatch");
  if (!/^[0-9a-f]{40}$/.test(record.sourceSHA ?? "")) throw new Error("candidate source SHA is invalid");
  if (!/^[0-9a-f]{40}$/.test(record.control?.buildSHA ?? "")) throw new Error("candidate control SHA is invalid");
  if (record.source?.runId !== String(run.id) || record.source?.runAttempt !== String(run.run_attempt)) {
    throw new Error("candidate record producer mismatch");
  }
  const desktopPrefix = /^desktop-([1-9][0-9]*)-([1-9][0-9]*)-preflight$/.exec(record.source?.desktopPrefix ?? "");
  if (!desktopPrefix || desktopPrefix[1] !== record.source.runId || Number(desktopPrefix[2]) > Number(record.source.runAttempt)) {
    throw new Error("candidate Desktop prefix does not belong to a completed producer attempt");
  }
  if (record.control?.buildSHA !== run.head_sha) throw new Error("candidate control SHA mismatch");
  if (!/^[1-9][0-9]*$/.test(record.source?.payloadArtifactId ?? "")) throw new Error("candidate payload artifact id is invalid");
  if (record.source?.payloadArtifactName !== `${namespace}-payload-${candidateId}`) throw new Error("candidate payload artifact name mismatch");
  if (!/^[1-9][0-9]*$/.test(record.source?.evidenceArtifactId ?? "")) throw new Error("candidate evidence artifact id is invalid");
  if (record.source?.evidenceArtifactName !== `${namespace}-evidence-${candidateId}`) throw new Error("candidate evidence artifact name mismatch");
  if (String(recordArtifact.workflow_run.id) !== record.source.runId) throw new Error("record artifact belongs to another run");
  if (record.validity?.revoked !== false) throw new Error("candidate record is revoked");
  const created = new Date(record.validity?.createdAt);
  const expires = new Date(record.validity?.expiresAt);
  if (!Number.isFinite(created.valueOf()) || !Number.isFinite(expires.valueOf()) || expires <= created || expires <= now) {
    throw new Error("candidate record has expired or has an invalid validity window");
  }
  return {
    candidateId,
    version: record.version,
    sourceSHA: record.sourceSHA,
    candidateControlSHA: record.control.buildSHA,
    signingFingerprint: record.signing.desktopFingerprint,
    producerRunId: record.source.runId,
    producerRunAttempt: record.source.runAttempt,
    desktopPrefix: record.source.desktopPrefix,
    payloadArtifactId: record.source.payloadArtifactId,
    payloadArtifactName: record.source.payloadArtifactName,
    evidenceArtifactId: record.source.evidenceArtifactId,
    evidenceArtifactName: record.source.evidenceArtifactName,
  };
}

async function githubJSON(url, token) {
  const response = await fetch(`https://api.github.com${url}`, {
    headers: { Accept: "application/vnd.github+json", Authorization: `Bearer ${token}`, "X-GitHub-Api-Version": "2022-11-28" },
  });
  if (!response.ok) throw new Error(`GitHub API ${response.status}: ${url}`);
  return response.json();
}

function outputs(values) {
  if (!process.env.GITHUB_OUTPUT) return process.stdout.write(`${JSON.stringify(values)}\n`);
  appendFileSync(process.env.GITHUB_OUTPUT, `${Object.entries(values).map(([key, value]) => `${key}=${value}`).join("\n")}\n`);
}

async function resolve(candidateId, required = true, purpose = "release") {
  const repository = process.env.GITHUB_REPOSITORY;
  const token = process.env.GH_TOKEN;
  if (!repository || !token) throw new Error("GITHUB_REPOSITORY and GH_TOKEN are required");
  requireNotRevoked(candidateId, process.env.RELEASE_REVOKED_CANDIDATES);
  const artifactData = await githubJSON(`/repos/${repository}/actions/artifacts?name=${encodeURIComponent(`${artifactNamespace(purpose)}-record-${candidateId}`)}&per_page=100`, token);
  const artifact = selectRecordArtifact(artifactData.artifacts ?? [], candidateId, required, purpose);
  if (!artifact) {
    outputs({ found: false });
    return;
  }
  const run = await githubJSON(`/repos/${repository}/actions/runs/${artifact.workflow_run.id}`, token);
  validateCandidateRun(run, artifact, repository);
  outputs({ found: true, record_artifact_id: artifact.id, producer_run_id: run.id, producer_run_attempt: run.run_attempt });
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  const [command, candidateId, recordPath, artifactPath, runPath] = process.argv.slice(2);
  if (command === "active") requireNotRevoked(candidateId, process.env.RELEASE_REVOKED_CANDIDATES);
  else if (command === "resolve") await resolve(candidateId);
  else if (command === "resolve-optional") await resolve(candidateId, false);
  else if (command === "resolve-rehearsal") await resolve(candidateId, true, "rehearsal");
  else if (command === "inspect" || command === "inspect-rehearsal") {
    const result = inspectRecord(
      JSON.parse(readFileSync(recordPath, "utf8")), candidateId,
      JSON.parse(readFileSync(artifactPath, "utf8")), JSON.parse(readFileSync(runPath, "utf8")),
      new Date(), command === "inspect-rehearsal" ? "rehearsal" : "release",
    );
    // This is a workflow API, not a case-conversion convention. In particular,
    // SHA is one field suffix, not three independently underscored letters.
    outputs({
      candidate_id: result.candidateId,
      version: result.version,
      source_sha: result.sourceSHA,
      candidate_control_sha: result.candidateControlSHA,
      signing_fingerprint: result.signingFingerprint,
      producer_run_id: result.producerRunId,
      producer_run_attempt: result.producerRunAttempt,
      desktop_prefix: result.desktopPrefix,
      payload_artifact_id: result.payloadArtifactId,
      payload_artifact_name: result.payloadArtifactName,
      evidence_artifact_id: result.evidenceArtifactId,
      evidence_artifact_name: result.evidenceArtifactName,
    });
  } else throw new Error("usage: resolve-release-candidate.mjs active ID | resolve|resolve-optional|resolve-rehearsal ID | inspect|inspect-rehearsal ID RECORD ARTIFACT RUN");
}
