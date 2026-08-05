import assert from "node:assert/strict";
import { test } from "node:test";
import {
  parseIssueRefs,
  parsePullRequestNumbers,
  previousTag,
  renderComment,
  isNotifiable,
  selectTargets,
  tagSeries,
  verificationMarker,
} from "./release-verify-issues.mjs";

test("upstream references are not mistaken for our issue numbers", () => {
  const body = "Follows wailsapp/wails#5544 and ScoopInstaller/Extras#18363. Refs #3470, fixes (#4092).";
  assert.deepEqual([...parseIssueRefs(body)].sort((a, b) => a - b), [3470, 4092]);
});

test("issue refs survive the punctuation PR bodies actually use", () => {
  const refs = parseIssueRefs("Addresses #6607 (also the mechanism behind #6346 / #5760).\n- #6225\nRelated: #6259, #6333");
  assert.deepEqual([...refs].sort((a, b) => a - b), [5760, 6225, 6259, 6333, 6346, 6607]);
});

test("pull request numbers come from both merge and squash subjects", () => {
  const log = [
    "Merge pull request #7053 from esengine/fix/cli-colour-profile",
    "fix(cli): resolve colour support from colorprofile",
    "polish(desktop): tighten spacing (#7019)",
  ].join("\n");
  assert.deepEqual(parsePullRequestNumbers(log), [7019, 7053]);
});

test("a release only follows its own tag series", () => {
  const tags = ["desktop-v1.18.0", "npm-v1.18.0", "v1.18.0", "desktop-v1.17.21", "npm-v1.17.21", "v1.17.21"];
  assert.equal(previousTag(tags, "desktop-v1.18.0"), "desktop-v1.17.21");
  assert.equal(previousTag(tags, "npm-v1.18.0"), "npm-v1.17.21");
  assert.equal(previousTag(tags, "v1.18.0"), "v1.17.21");
  assert.equal(tagSeries("desktop-v1.18.0"), "desktop-");
  assert.equal(tagSeries("v1.18.0"), "");
});

test("the oldest tag in a series has nothing to compare against", () => {
  assert.equal(previousTag(["desktop-v1.0.0"], "desktop-v1.0.0"), null);
  assert.equal(previousTag(["desktop-v1.0.0"], "desktop-v9.9.9"), null);
});

test("a referenced pull request is never treated as a reportable issue", () => {
  const tag = "desktop-v1.18.0";
  // GitHub's issues API answers for pull requests too, so this is the only
  // thing separating a consolidation PR's "PR #5576 by @x" from a real report.
  assert.equal(isNotifiable({ state: "open", isPullRequest: true, commentBodies: [] }, tag), false);
  assert.equal(isNotifiable({ state: "open", isPullRequest: false, commentBodies: [] }, tag), true);
});

test("closed and already-notified issues are skipped", () => {
  const tag = "desktop-v1.18.0";
  assert.equal(isNotifiable({ state: "closed", isPullRequest: false, commentBodies: [] }, tag), false);
  assert.equal(
    isNotifiable({ state: "open", isPullRequest: false, commentBodies: [`x ${verificationMarker(tag)} y`] }, tag),
    false,
  );
  // a marker from a different release must not suppress this one
  assert.equal(
    isNotifiable({ state: "open", isPullRequest: false, commentBodies: [verificationMarker("desktop-v1.17.21")] }, tag),
    true,
  );
});

test("selectTargets keeps only notifiable issues, sorted, with their source PRs", () => {
  const tag = "desktop-v1.18.0";
  const refsByIssue = new Map([
    [300, new Set([30])],
    [100, new Set([10])],
    [200, new Set([21, 20])],
  ]);
  const records = new Map([
    [100, { state: "open", isPullRequest: false, commentBodies: [verificationMarker(tag)] }],
    [200, { state: "open", isPullRequest: false, commentBodies: [] }],
    [300, { state: "open", isPullRequest: true, commentBodies: [] }],
  ]);
  assert.deepEqual(selectTargets({ refsByIssue, records, tag }), [{ issue: 200, pullNumbers: [20, 21] }]);
});

test("the comment carries a per-tag marker that the skip check matches", () => {
  const body = renderComment({ tag: "desktop-v1.18.0", pullNumbers: [7019] });
  assert.ok(body.includes(verificationMarker("desktop-v1.18.0")));
  assert.ok(!body.includes(verificationMarker("desktop-v1.17.21")));
  assert.match(body, /#7019 is in that release/);
});

test("the comment asks for verification and never claims the issue is fixed", () => {
  const body = renderComment({ tag: "desktop-v1.18.0", pullNumbers: [1, 2] });
  assert.match(body, /#1, #2 are in that release/);
  assert.match(body, /request to verify rather than a fix announcement/);
  assert.doesNotMatch(body, /\bclosing\b|\bfixed in\b/i);
});
