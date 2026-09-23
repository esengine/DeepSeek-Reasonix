import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

const workflowPath = ".github/workflows/release-desktop.yml";
const contractPath = ".signpath/contracts/release-signing.yml";

function run(command, args) {
  const result = spawnSync(command, args, { encoding: "utf8", maxBuffer: 16 * 1024 * 1024 });
  if (result.status !== 0) throw new Error(`${command} ${args[0]} failed: ${result.stderr.trim() || result.stdout.trim()}`);
  return result.stdout.trim();
}

export function fingerprintFiles(contract) {
  const match = /^fingerprint_files:\n((?:  - [^\n]+\n)+)/m.exec(contract);
  if (!match) throw new Error("signing contract has no fingerprint file list");
  const files = match[1].trimEnd().split("\n").map(line => line.slice(4));
  if (!files.includes(workflowPath)) throw new Error("Desktop workflow is absent from the signing fingerprint");
  return files;
}

export function outsideSigningContract(workflow) {
  const startMarker = "\n  signing-contract:\n";
  const endMarker = "\n  build:\n";
  const start = workflow.indexOf(startMarker);
  const end = workflow.indexOf(endMarker, start + startMarker.length);
  if (start < 0 || end < 0 || workflow.indexOf(startMarker, start + 1) >= 0 || workflow.indexOf(endMarker, end + 1) >= 0) {
    throw new Error("Desktop workflow signing-contract boundaries are ambiguous");
  }
  return workflow.slice(0, start) + workflow.slice(end);
}

export function verifyCandidateSigningRepair(candidateSHA, expectedFingerprint) {
  if (!/^[0-9a-f]{40}$/.test(candidateSHA) || !/^v1:[0-9a-f]{64}$/.test(expectedFingerprint)) {
    throw new Error("invalid candidate signing identity");
  }
  run("git", ["merge-base", "--is-ancestor", candidateSHA, "HEAD"]);
  const contract = readFileSync(contractPath, "utf8");
  if (spawnSync("git", ["diff", "--quiet", candidateSHA, "HEAD", "--", contractPath]).status !== 0) {
    throw new Error("protected signing contract changed after candidate sealing");
  }
  const otherFiles = fingerprintFiles(contract).filter(file => file !== workflowPath);
  if (spawnSync("git", ["diff", "--quiet", candidateSHA, "HEAD", "--", ...otherFiles]).status !== 0) {
    throw new Error("signing inputs changed after candidate sealing");
  }
  const originalWorkflow = run("git", ["show", `${candidateSHA}:${workflowPath}`]);
  if (outsideSigningContract(originalWorkflow) !== outsideSigningContract(readFileSync(workflowPath, "utf8").trimEnd())) {
    throw new Error("Desktop build or publication changed after candidate sealing");
  }

  const temporary = mkdtempSync(path.join(tmpdir(), "reasonix-signing-candidate-"));
  const checkout = path.join(temporary, "candidate");
  try {
    run("git", ["worktree", "add", "--detach", checkout, candidateSHA]);
    const actual = run("go", ["run", "./cmd/signpath-contract", "fingerprint", checkout]);
    if (actual !== expectedFingerprint) throw new Error("sealed candidate signing fingerprint differs from its protected control commit");
    return actual;
  } finally {
    spawnSync("git", ["worktree", "remove", "--force", checkout], { encoding: "utf8" });
    rmSync(temporary, { recursive: true, force: true });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    process.stdout.write(`${verifyCandidateSigningRepair(process.argv[2], process.argv[3])}\n`);
  } catch (error) {
    process.stderr.write(`candidate signing repair: ${error.message}\n`);
    process.exitCode = 1;
  }
}
