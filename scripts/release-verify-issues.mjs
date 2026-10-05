#!/usr/bin/env node
// Ask reporters to verify, on the release that actually carries their fix.
// A PR that writes "Fixes #N" auto-closes its issue on merge; one that writes
// "Refs"/"Related"/"Addresses" does not, so the report sits open until someone
// remembers to come back after the release. Nobody does. This posts that
// follow-up for every still-open issue referenced by a PR in the release.
//
// It never closes anything — deciding a report is resolved stays human.
//
// Usage:
//   GH_TOKEN=... node scripts/release-verify-issues.mjs --tag desktop-v1.18.0 [--dry-run]

import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

// A bare "#123" is ours. "owner/repo#123" is upstream — matching those once
// filed a Wails issue number as if it were one of ours.
const ISSUE_REF = /(^|[^\w/#-])#(\d{1,6})\b/g;
const MERGE_SUBJECT = /Merge pull request #(\d+)/g;
const SQUASH_SUBJECT = /\(#(\d+)\)\s*$/gm;

export function parseIssueRefs(text) {
  const out = new Set();
  for (const [, , n] of (text || "").matchAll(ISSUE_REF)) out.add(Number(n));
  return out;
}

export function parsePullRequestNumbers(gitLog) {
  const out = new Set();
  for (const [, n] of (gitLog || "").matchAll(MERGE_SUBJECT)) out.add(Number(n));
  for (const [, n] of (gitLog || "").matchAll(SQUASH_SUBJECT)) out.add(Number(n));
  return [...out].sort((a, b) => a - b);
}

// Releases share a repo but not a series: desktop-v1.18.0 follows
// desktop-v1.17.21, not npm-v1.18.0.
export function tagSeries(tag) {
  const m = /^(.*?)v\d/.exec(tag);
  return m ? m[1] : "";
}

export function previousTag(tags, current) {
  const series = tagSeries(current);
  const peers = tags.filter((t) => tagSeries(t) === series);
  const i = peers.indexOf(current);
  return i >= 0 && i + 1 < peers.length ? peers[i + 1] : null;
}

export function verificationMarker(tag) {
  return `<!-- release-verify:${tag} -->`;
}

export function renderComment({ tag, pullNumbers }) {
  const prs = pullNumbers.map((n) => `#${n}`).join(", ");
  const source = pullNumbers.length === 1 ? `${prs} is` : `${prs} are`;
  return [
    verificationMarker(tag),
    `A change referencing this report shipped in **\`${tag}\`** — ${source} in that release.`,
    "",
    "The PR referenced this issue without claiming to close it, so this is a request to verify rather than a fix announcement: the change may resolve what you reported, address only part of it, or turn out to be unrelated.",
    "",
    `Could you re-test on \`${tag}\` and reply either way? "Still happening" is as useful as "fixed" — an unverified fix is why this thread stayed open.`,
    "",
    "No action needed if you have moved on; this stays open until someone says otherwise.",
  ].join("\n");
}

// GitHub's issues API returns pull requests too, so a reference to a sibling PR
// looks like a perfectly good open issue until you check for this key.
export function isNotifiable(record, tag) {
  if (!record || record.isPullRequest) return false;
  if (record.state !== "open") return false;
  return !(record.commentBodies || []).some((body) => (body || "").includes(verificationMarker(tag)));
}

export function selectTargets({ refsByIssue, records, tag }) {
  return [...refsByIssue.entries()]
    .filter(([issue]) => isNotifiable(records.get(issue), tag))
    .map(([issue, pulls]) => ({ issue, pullNumbers: [...pulls].sort((a, b) => a - b) }))
    .sort((a, b) => a.issue - b.issue);
}

function gh(args, { json = true } = {}) {
  const out = execFileSync("gh", args, { encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  return json ? JSON.parse(out) : out;
}

function git(args) {
  return execFileSync("git", args, { encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function main() {
  const argv = process.argv.slice(2);
  const dryRun = argv.includes("--dry-run");
  const tag = argv[argv.indexOf("--tag") + 1];
  if (!tag || tag.startsWith("--")) {
    console.error("usage: release-verify-issues.mjs --tag <release-tag> [--dry-run]");
    process.exit(1);
  }

  const tags = git(["tag", "--list", "--sort=-v:refname"]).split("\n").filter(Boolean);
  const from = previousTag(tags, tag);
  if (!from) {
    console.log(`no previous tag in the ${tagSeries(tag) || "root"} series; nothing to compare`);
    return;
  }

  const pulls = parsePullRequestNumbers(git(["log", `${from}..${tag}`, "--pretty=%s%n%b"]));
  console.log(`${from}..${tag}: ${pulls.length} pull request(s)`);

  const refsByIssue = new Map();
  for (const pr of pulls) {
    let data;
    try {
      data = gh(["pr", "view", String(pr), "--json", "title,body"]);
    } catch {
      continue; // a "#N" that is not a PR in this repo
    }
    for (const issue of parseIssueRefs(`${data.title}\n${data.body || ""}`)) {
      if (issue === pr) continue;
      if (!refsByIssue.has(issue)) refsByIssue.set(issue, new Set());
      refsByIssue.get(issue).add(pr);
    }
  }

  const repo = gh(["repo", "view", "--json", "nameWithOwner"]).nameWithOwner;
  const records = new Map();
  for (const issue of refsByIssue.keys()) {
    let head;
    try {
      head = gh(["api", `repos/${repo}/issues/${issue}`]);
    } catch {
      continue; // referenced number does not exist in this repo
    }
    const record = { state: head.state, isPullRequest: Boolean(head.pull_request), commentBodies: [] };
    if (isNotifiable(record, tag)) {
      const comments = gh(["api", `repos/${repo}/issues/${issue}/comments`, "--paginate"]);
      record.commentBodies = comments.map((c) => c.body || "");
    }
    records.set(issue, record);
  }

  const targets = selectTargets({ refsByIssue, records, tag });
  const skippedPulls = [...records.values()].filter((r) => r.isPullRequest).length;
  console.log(`${refsByIssue.size} referenced (${skippedPulls} were pull requests), ${targets.length} to notify`);

  for (const { issue, pullNumbers } of targets) {
    const body = renderComment({ tag, pullNumbers });
    if (dryRun) {
      console.log(`[dry-run] #${issue} <- ${pullNumbers.map((n) => `#${n}`).join(", ")}`);
      continue;
    }
    gh(["issue", "comment", String(issue), "--body", body], { json: false });
    console.log(`commented on #${issue}`);
    await sleep(3000); // stay under GitHub's secondary content-creation limit
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
