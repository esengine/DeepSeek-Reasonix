#!/usr/bin/env node
// Ask, wait, then close — for bug reports filed against a version far enough
// back that nobody can tell whether they still reproduce.
//
// Age alone is never the reason. The close only ever follows an unanswered
// question, the window is long, and the reason is "not planned" with reopening
// invited, so a wrong guess costs the reporter one click.
//
// Usage:
//   GH_TOKEN=... node scripts/stale-report-sweep.mjs [--dry-run] [--limit N]

import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

// A report this far back predates rewrites large enough that its symptom cannot
// be assumed to survive; anything newer is still close enough to reason about.
export const CUTOFF_MINOR = 10;
// Consequences too expensive to guess at. These stay for a human.
export const SEVERITY_LABELS = ["data-loss", "security", "crash"];
// A maintainer reply means the report was already judged; a bot must not
// overrule that by timing it out.
export const MAINTAINER_ASSOCIATIONS = new Set(["OWNER", "MEMBER", "COLLABORATOR"]);
// Long enough to see a notification, install a release and actually re-test.
export const WINDOW_DAYS = 21;
export const ASK_MARKER = "<!-- stale-report-sweep:ask -->";

export function parseReportedVersion(body) {
  const section = (body || "").split(/^###\s+/m).find((s) => /^Exact version/i.test(s));
  const m = /(\d+)\.(\d+)\.(\d+)/.exec(section || "");
  return m ? { major: Number(m[1]), minor: Number(m[2]), patch: Number(m[3]), raw: m[0] } : null;
}

// "1.7.18" parses cleanly and 1.7.0 shipped, so a dropped digit in "1.17.18"
// reads as a release eleven minors old instead of the current one. Only a
// version that was actually published counts as evidence of age.
export function isStaleVersion(version, cutoffMinor = CUTOFF_MINOR, released = null) {
  if (!version) return false;
  if (released && !released.has(`${version.major}.${version.minor}.${version.patch}`)) return false;
  return version.major < 1 || (version.major === 1 && version.minor < cutoffMinor);
}

// The form's version field is whatever the reporter typed there, and they get
// it wrong in both directions: a dropped digit, or the default left in place
// while the real build is named further down the body. A report cannot be older
// than the newest release it mentions anywhere, so that is what age is measured
// from. Filtering to published versions keeps Node versions and timestamps out.
export function highestMentionedVersion(body, released) {
  let best = null;
  for (const m of (body || "").matchAll(/(\d+)\.(\d+)\.(\d+)/g)) {
    const v = { major: Number(m[1]), minor: Number(m[2]), patch: Number(m[3]), raw: m[0] };
    if (released && !released.has(`${v.major}.${v.minor}.${v.patch}`)) continue;
    if (!best || v.major > best.major || (v.major === best.major && (v.minor > best.minor || (v.minor === best.minor && v.patch > best.patch)))) {
      best = v;
    }
  }
  return best;
}

export function releasedVersions(tags) {
  const out = new Set();
  for (const tag of tags) {
    const m = /(\d+)\.(\d+)\.(\d+)$/.exec(tag);
    if (m) out.add(`${Number(m[1])}.${Number(m[2])}.${Number(m[3])}`);
  }
  return out;
}

export function hasMaintainerReply(comments = []) {
  return comments.some((c) => MAINTAINER_ASSOCIATIONS.has(c.authorAssociation));
}

export function findAsk(comments = []) {
  return comments.find((c) => (c.body || "").includes(ASK_MARKER)) || null;
}

export function shouldAsk(issue, { cutoffMinor = CUTOFF_MINOR, released = null } = {}) {
  // "does it still reproduce" is a question only a defect report can answer; a
  // feature request does not go stale because the version moved on.
  if (!(issue.labels || []).includes("bug")) return false;
  if (!isStaleVersion(issue.version, cutoffMinor, released)) return false;
  if ((issue.labels || []).some((l) => SEVERITY_LABELS.includes(l))) return false;
  if (hasMaintainerReply(issue.comments)) return false;
  return !findAsk(issue.comments);
}

// Only an ask that nobody answered may expire. Any later comment — from the
// reporter, a bystander or a maintainer — takes the issue back out of the sweep.
export function shouldClose(issue, { now, windowDays = WINDOW_DAYS } = {}) {
  const ask = findAsk(issue.comments);
  if (!ask) return false;
  if ((issue.labels || []).some((l) => SEVERITY_LABELS.includes(l))) return false;
  const askedAt = Date.parse(ask.createdAt);
  if (Number.isNaN(askedAt)) return false;
  const answered = (issue.comments || []).some((c) => Date.parse(c.createdAt) > askedAt);
  if (answered) return false;
  return Date.parse(now) - askedAt >= windowDays * 86400000;
}

export function renderAsk({ version, current, windowDays = WINDOW_DAYS }) {
  return [
    ASK_MARKER,
    `This was reported against **${version}**, and the current release is **${current}** — far enough apart that we cannot tell from here whether it still happens.`,
    "",
    "Does it still reproduce on the current release?",
    "",
    `If we hear nothing in ${windowDays} days this gets closed as stale, which is a bookkeeping state and not a judgement about the report. Reopening takes one click, and a reply at any point — including after it closes — brings it back.`,
  ].join("\n");
}

export function renderClose({ windowDays = WINDOW_DAYS }) {
  return [
    `Closing as stale — the verification question above went ${windowDays} days without an answer.`,
    "",
    "This is not a decision that the report was invalid. It means nobody can confirm the symptom against a current build, and leaving it open makes the tracker less useful for the reports that can be acted on.",
    "",
    "If you hit this again, comment here and it reopens — no need to file a new one.",
  ].join("\n");
}

function gh(args, { json = true } = {}) {
  const out = execFileSync("gh", args, { encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  return json ? JSON.parse(out) : out;
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function main() {
  const argv = process.argv.slice(2);
  const dryRun = argv.includes("--dry-run");
  const li = argv.indexOf("--limit");
  const limit = li >= 0 ? Number(argv[li + 1]) : 40;
  const now = new Date().toISOString();

  const repo = gh(["repo", "view", "--json", "nameWithOwner"]).nameWithOwner;
  const current = gh(["release", "list", "--limit", "40", "--json", "tagName"])
    .map((r) => r.tagName)
    .find((t) => t.startsWith("desktop-v")) || "the latest release";

  const released = releasedVersions(gh(["api", `repos/${repo}/tags`, "--paginate"]).map((t) => t.name));
  const issues = gh([
    "issue", "list", "--state", "open", "--limit", "1200",
    "--json", "number,body,labels,comments",
  ]).map((i) => ({
    number: i.number,
    version: highestMentionedVersion(i.body, released),
    labels: (i.labels || []).map((l) => l.name),
    comments: (i.comments || []).map((c) => ({
      body: c.body,
      createdAt: c.createdAt,
      authorAssociation: c.authorAssociation,
    })),
  }));

  const toClose = issues.filter((i) => shouldClose(i, { now })).slice(0, limit);
  const toAsk = issues.filter((i) => shouldAsk(i, { released })).slice(0, limit);
  console.log(`${issues.length} open; ${toAsk.length} to ask (cap ${limit}), ${toClose.length} to close`);

  for (const issue of toClose) {
    if (dryRun) {
      console.log(`[dry-run] close #${issue.number}`);
      continue;
    }
    gh(["issue", "comment", String(issue.number), "--body", renderClose({})], { json: false });
    gh(["issue", "close", String(issue.number), "--reason", "not planned"], { json: false });
    console.log(`closed #${issue.number}`);
    await sleep(3000);
  }

  for (const issue of toAsk) {
    const body = renderAsk({ version: issue.version.raw, current });
    if (dryRun) {
      console.log(`[dry-run] ask #${issue.number} (reported ${issue.version.raw})`);
      continue;
    }
    gh(["issue", "comment", String(issue.number), "--body", body], { json: false });
    console.log(`asked #${issue.number}`);
    await sleep(3000);
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
