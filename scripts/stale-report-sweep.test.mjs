import assert from "node:assert/strict";
import { test } from "node:test";
import {
  ASK_MARKER,
  CUTOFF_MINOR,
  WINDOW_DAYS,
  highestMentionedVersion,
  isStaleVersion,
  parseReportedVersion,
  releasedVersions,
  renderAsk,
  renderClose,
  shouldAsk,
  shouldClose,
} from "./stale-report-sweep.mjs";

const NOW = "2026-07-30T00:00:00Z";
const daysBefore = (n) => new Date(Date.parse(NOW) - n * 86400000).toISOString();
const ask = (createdAt) => ({ body: `${ASK_MARKER}\nstill reproducing?`, createdAt, authorAssociation: "OWNER" });

test("the reported version comes from the form field, not from prose", () => {
  const body = "### Version line\n\nv2\n\n### Exact version\n\n1.8.1\n\n### What happened?\n\nsaw 1.17.9 mentioned in a log";
  assert.deepEqual(parseReportedVersion(body), { major: 1, minor: 8, patch: 1, raw: "1.8.1" });
  assert.equal(parseReportedVersion("### What happened?\n\n1.2.3"), null);
});

test("staleness is a version cutoff, and an unparsable version is never stale", () => {
  assert.equal(isStaleVersion(parseReportedVersion("### Exact version\n\n1.8.1")), true);
  assert.equal(isStaleVersion(parseReportedVersion("### Exact version\n\n1.17.21")), false);
  assert.equal(isStaleVersion(parseReportedVersion("### Exact version\n\n1.10.0")), false);
  assert.equal(isStaleVersion(null), false);
});

test("a version that was never released is a typo, not an old release", () => {
  const released = releasedVersions(["desktop-v1.7.0", "desktop-v1.17.18", "desktop-v1.18.0", "npm-v1.17.18"]);
  const typo = parseReportedVersion("### Exact version\n\n1.7.18"); // dropped digit from 1.17.18
  const real = parseReportedVersion("### Exact version\n\n1.7.0");
  assert.equal(isStaleVersion(typo, CUTOFF_MINOR, released), false);
  assert.equal(isStaleVersion(real, CUTOFF_MINOR, released), true);
  // without the tag list there is nothing to catch it, which is why main passes one
  assert.equal(isStaleVersion(typo), true);
});

test("a version named elsewhere in the body outranks a wrong form field", () => {
  const released = releasedVersions(["desktop-v1.0.0", "desktop-v1.17.13", "desktop-v1.18.0"]);
  // real shape: the form field kept its default while the true build is in prose
  const body = "### Exact version\n\n1.0.0\n\n### Steps to reproduce\n\nwin64 vscode, 软件版本 v1.17.13";
  assert.equal(parseReportedVersion(body).raw, "1.0.0");
  assert.equal(highestMentionedVersion(body, released).raw, "1.17.13");
  assert.equal(isStaleVersion(highestMentionedVersion(body, released), CUTOFF_MINOR, released), false);
});

test("unreleased numbers in the body are ignored, so Node versions cannot age a report", () => {
  const released = releasedVersions(["desktop-v1.8.1", "desktop-v1.18.0"]);
  const body = "### Exact version\n\n1.8.1\n\n**Node**: v26.3.1\n**Terminal size**: 65.0.0";
  assert.equal(highestMentionedVersion(body, released).raw, "1.8.1");
  assert.equal(highestMentionedVersion("no versions here", released), null);
});

test("released versions are collected across every tag series", () => {
  const released = releasedVersions(["v1.18.0", "desktop-v1.17.21", "npm-v1.17.21", "not-a-tag", "desktop-v1.7.0"]);
  assert.deepEqual([...released].sort(), ["1.17.21", "1.18.0", "1.7.0"]);
});

test("only defect reports are swept — a feature request cannot answer the question", () => {
  const base = { version: { major: 1, minor: 2, patch: 0, raw: "1.2.0" }, comments: [] };
  assert.equal(shouldAsk({ ...base, labels: ["enhancement", "desktop"] }), false);
  assert.equal(shouldAsk({ ...base, labels: [] }), false);
  assert.equal(shouldAsk({ ...base, labels: ["bug", "enhancement"] }), true);
});

test("severity labels are never swept, however old the report", () => {
  const base = { version: { major: 1, minor: 2, patch: 0, raw: "1.2.0" }, comments: [] };
  for (const label of ["data-loss", "security", "crash"]) {
    assert.equal(shouldAsk({ ...base, labels: ["bug", label] }), false, label);
  }
  assert.equal(shouldAsk({ ...base, labels: ["bug", "windows"] }), true);
});

test("a maintainer reply takes the report out of the sweep", () => {
  const base = { version: { major: 1, minor: 2, patch: 0, raw: "1.2.0" }, labels: ["bug"] };
  const reporter = { body: "me too", createdAt: daysBefore(40), authorAssociation: "NONE" };
  const maintainer = { body: "looking", createdAt: daysBefore(40), authorAssociation: "COLLABORATOR" };
  assert.equal(shouldAsk({ ...base, comments: [reporter] }), true);
  assert.equal(shouldAsk({ ...base, comments: [maintainer] }), false);
});

test("the same issue is never asked twice", () => {
  const issue = {
    version: { major: 1, minor: 2, patch: 0, raw: "1.2.0" },
    labels: ["bug"],
    comments: [ask(daysBefore(3))],
  };
  assert.equal(shouldAsk(issue), false);
});

test("closing requires an ask that nobody answered and a window that elapsed", () => {
  const labels = ["bug"];
  assert.equal(shouldClose({ labels, comments: [] }, { now: NOW }), false, "never asked");
  assert.equal(
    shouldClose({ labels, comments: [ask(daysBefore(WINDOW_DAYS - 1))] }, { now: NOW }),
    false,
    "window not elapsed",
  );
  assert.equal(shouldClose({ labels, comments: [ask(daysBefore(WINDOW_DAYS))] }, { now: NOW }), true);
});

test("any reply after the ask cancels the close, whoever wrote it", () => {
  const labels = ["bug"];
  const asked = ask(daysBefore(60));
  for (const association of ["NONE", "CONTRIBUTOR", "COLLABORATOR"]) {
    const reply = { body: "still broken", createdAt: daysBefore(1), authorAssociation: association };
    assert.equal(shouldClose({ labels, comments: [asked, reply] }, { now: NOW }), false, association);
  }
  // a comment predating the ask is not an answer to it
  const older = { body: "me too", createdAt: daysBefore(90), authorAssociation: "NONE" };
  assert.equal(shouldClose({ labels, comments: [older, asked] }, { now: NOW }), true);
});

test("a severity label added after the ask still blocks the close", () => {
  const comments = [ask(daysBefore(60))];
  assert.equal(shouldClose({ labels: ["bug"], comments }, { now: NOW }), true);
  assert.equal(shouldClose({ labels: ["bug", "data-loss"], comments }, { now: NOW }), false);
});

test("the ask states both versions and the deadline; the close invites reopening", () => {
  const body = renderAsk({ version: "1.8.1", current: "desktop-v1.18.0" });
  assert.ok(body.startsWith(ASK_MARKER));
  assert.match(body, /1\.8\.1/);
  assert.match(body, /desktop-v1\.18\.0/);
  assert.match(body, new RegExp(`${WINDOW_DAYS} days`));
  const closed = renderClose({});
  assert.match(closed, /reopens/);
  assert.match(closed, new RegExp(`${WINDOW_DAYS} days`));
  // stale is a bookkeeping state; claiming a fix we never verified would be a lie
  assert.doesNotMatch(closed, /\bfixed\b|\bresolved\b/i);
});
