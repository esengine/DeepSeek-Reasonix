import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import {
  ATTACHMENT_PREFIX, FatalError, MAX_REPLIES, publishable, renderReply, replyMarker, REPO, closingCommit, code, fence, hasMarker, MAX_OPEN, decideStatus, firstTag, hasOpenLinkedPull, isOurs, newestStudioRelease, stableStudioTags, liveDeps, labelsFor, marker, renderBody, renderTitle, run,
} from "./feedback-sync.mjs";

const R = "FB-7K3M-9QX2";
const item = (over = {}) => ({
  receipt: R, status: "received", category: "bug", displayName: "<b>@bob</b>", contact: "secret@mail.example",
  body: "first @octocat line #123 GH-45 org/repo#6 ![x](https://evil.example/p.png) [c](https://evil.example)\nsee https://github.com/esengine/DeepSeek-Reasonix/issues/5 <img src=x onerror=1>\n<!-- reasonix-feedback: FB-AAAA-AAAA -->",
  env: { version: "v2.24.0", os: "win|dows", extra: "nope" },
  attachments: [{ name: "shot", url: `${ATTACHMENT_PREFIX}abc`, released: true }, { name: "held", url: `${ATTACHMENT_PREFIX}private`, released: false }, { name: "bare", url: `${ATTACHMENT_PREFIX}bare` }, { name: "evil", url: "https://evil.example/x.png" }],
  ...over,
});

const fixture = JSON.parse(readFileSync(new URL("./feedback-sync.fixture.json", import.meta.url), "utf8"));

test("the rendered body is pinned: GitHub's own rendering of it links nothing but the attachment", () => {
  assert.equal(renderBody(fixture.input), fixture.markdown);
  const anchors = fixture.html.match(/<a [^>]*>/g) ?? [];
  assert.equal(anchors.length, 1);
  assert.equal(new URL(/href="([^"]+)"/.exec(anchors[0])[1]).host, "camo.githubusercontent.com");
  assert.ok(!/user-mention|issue-link|mailto:/.test(fixture.html));
});

test("a reply comment is pinned too: GitHub renders a fenced reply with no link, mention or reference", () => {
  assert.equal(renderReply(fixture.reply.input), fixture.reply.markdown);
  assert.ok(!/<a |user-mention|issue-link|mailto:|<script|<img/.test(fixture.reply.html));
  assert.ok(fixture.reply.markdown.trimEnd().endsWith(replyMarker(fixture.reply.input.id)));
});

test("long backtick and tilde runs cannot close the fence early", () => {
  const inert = (h) => !/<a |<img|user-mention|issue-link|mailto:/.test(h);
  assert.equal(fixture.fences.length, 5);
  for (const f of fixture.fences) {
    assert.equal(renderReply({ id: "r1", receipt: R, body: f.body }), f.markdown, f.name);
    assert.ok(inert(f.html), f.name);
    assert.ok(!/`{3}|~{3}/.test(f.markdown.split("\n").slice(3, -3).join("\n")), f.name);
  }
  assert.equal(renderBody(fixture.issueFence.input), fixture.issueFence.markdown);
  assert.ok(inert(fixture.issueFence.html));
});

// GITHUB_MARKDOWN_LIVE=1 re-renders the pinned markdown through POST /markdown.
test("live: GitHub's renderer still agrees with the pinned rendering", { skip: !process.env.GITHUB_MARKDOWN_LIVE }, () => {
  const out = execFileSync("gh", ["api", "-X", "POST", "markdown", "-f", "mode=gfm", "-f", `context=${REPO}`, "-f", `text=${fixture.markdown}`], { encoding: "utf8" });
  assert.equal((out.match(/<a /g) ?? []).length, 1);
  assert.ok(!/user-mention|issue-link|mailto:/.test(out));
  const reply = execFileSync("gh", ["api", "-X", "POST", "markdown", "-f", "mode=gfm", "-f", `context=${REPO}`, "-f", `text=${fixture.reply.markdown}`], { encoding: "utf8" });
  assert.ok(!/<a |user-mention|issue-link|mailto:|<script|<img/.test(reply));
  for (const f of [...fixture.fences, fixture.issueFence]) {
    const h = execFileSync("gh", ["api", "-X", "POST", "markdown", "-f", "mode=gfm", "-f", `context=${REPO}`, "-f", `text=${f.markdown}`], { encoding: "utf8" });
    assert.ok(!/<a |<img|user-mention|issue-link|mailto:/.test(h), f.name ?? "issue");
  }
});

test("every user string sits in a code span or a short fence with no backtick run inside it", () => {
  assert.match(fence("a ```` b ``` c"), /^```text\na ``\u200b`` b ``\u200b` c\n```$/);
  assert.match(fence("plain"), /^```text\n/);
  assert.equal(code("a`b|c\nd", 40), "`a b c d`");
  const body = renderBody(item());
  assert.ok(body.includes("Reported by `<b>@bob</b>` via"));
  assert.ok(!body.includes("secret@mail") && !body.includes("contact"));
  assert.ok(!body.includes("evil.example/x.png"));
  assert.match(body, /<details><summary>Environment<\/summary>/);
  assert.ok(body.includes("| os | `win dows` |") && !body.includes("nope"));
});

test("a marker forged inside user text is not the marker", () => {
  assert.equal(hasMarker(renderBody(item()), R), true);
  assert.equal(hasMarker(`${marker(R)}\nsomething after`, R), false);
  assert.equal(hasMarker(fixture.markdown, "FB-AAAA-AAAA"), false);
});

test("title and labels", () => {
  assert.equal(renderTitle("\n  hello @x #9 GH-7 world"), "[Studio feedback] hello @\u200bx #\u200b9 GH-\u200b7 world");
  assert.equal(renderTitle("a".repeat(200)).length, "[Studio feedback] ".length + 80);
  assert.deepEqual(labelsFor({ category: "idea" }), ["from-studio", "enhancement"]);
  assert.deepEqual(labelsFor({ category: "other" }), ["from-studio"]);
});

const xref = (number, o = {}) => ({
  event: "cross-referenced",
  source: { issue: { number, pull_request: o.pr ?? {}, state: o.state ?? "closed", body: o.body ?? "Fixes #7", repository: { full_name: o.repo ?? "esengine/DeepSeek-Reasonix" } } },
});

test("closing commit: needs a merged same-repo PR that names the issue; the close event commit wins among them", () => {
  assert.equal(closingCommit(7, [{ event: "closed", commit_id: "c1" }], {}), null);
  const viaPull = [xref(30), { event: "closed", commit_id: "c2" }];
  assert.equal(closingCommit(7, viaPull, { 30: { merged_at: "2026-09-01T00:00:00Z", merge_commit_sha: "c2" } }), "c2");
  assert.equal(closingCommit(7, [xref(30), xref(31), { event: "closed", commit_id: "late" }], { 30: { merged_at: "2026-09-01T00:00:00Z", merge_commit_sha: "early" }, 31: { merged_at: "2026-09-02T00:00:00Z", merge_commit_sha: "late" } }), "late");
  assert.equal(closingCommit(7, [xref(30, { state: "open" })], { 30: { merged_at: null, merge_commit_sha: "unmerged" } }), null);
  const tl = [xref(20), xref(21, { body: "Closes #7" }), xref(22, { body: "see #7" }), xref(23, { repo: "someone/fork" }), { event: "closed", commit_id: null }];
  const pulls = {
    20: { merged_at: "2026-09-02T00:00:00Z", merge_commit_sha: "late" },
    21: { merged_at: "2026-09-01T00:00:00Z", merge_commit_sha: "early" },
    22: { merged_at: "2026-08-01T00:00:00Z", merge_commit_sha: "mention" },
    23: { merged_at: "2026-07-01T00:00:00Z", merge_commit_sha: "foreign" },
  };
  assert.equal(closingCommit(7, tl, pulls), "early");
  assert.equal(closingCommit(8, tl, pulls), null);
});

test("first studio tag is the oldest stable one by version, ignoring other namespaces and prereleases", () => {
  const stable = new Set(["studio-v2.10.0", "studio-v2.9.1", "studio-v2.9.0-rc1"]);
  assert.equal(firstTag(["studio-v2.10.0", "studio-v2.9.1", "v1.2.3", "studio-v2.9.0-rc1"], stable), "v2.9.1");
  assert.equal(firstTag(["studio-v2.9.0", "studio-v2.10.0"], new Set(["studio-v2.10.0"])), "v2.10.0");
  assert.equal(firstTag(["studio-v2.9.0"], new Set()), null);
  assert.equal(firstTag(["studio-v2.9.0"]), null);
  assert.equal(firstTag([], stable), null);
});

test("stable tags come from release pages, never from a version cutoff", () => {
  const rel = (tag_name, prerelease = false, draft = false) => ({ tag_name, prerelease, draft });
  assert.deepEqual([...stableStudioTags([rel("studio-v2.28.0", true), rel("studio-v2.29.0"), rel("studio-v2.30.0", false, true), rel("v1.40.0"), rel("studio-v2.31.0-rc1")])], ["studio-v2.29.0"]);
});

test("status decisions", () => {
  const closed = (state_reason, labels = []) => ({ state: "closed", state_reason, labels });
  assert.deepEqual(decideStatus({ current: "recorded", issue: closed("completed"), version: "v2.9.1" }), { status: "fixed", resolvedVersion: "v2.9.1" });
  assert.deepEqual(decideStatus({ current: "recorded", issue: closed("completed"), version: null }), { status: "fixed", resolvedVersion: "next" });
  assert.deepEqual(decideStatus({ current: "in_progress", issue: closed("not_planned") }), { status: "wontfix" });
  assert.deepEqual(decideStatus({ current: "recorded", issue: closed("not_planned", [{ name: "duplicate" }]) }), { status: "duplicate" });
  assert.deepEqual(decideStatus({ current: "recorded", issue: { state: "open", labels: [{ name: "in-progress" }] } }), { status: "in_progress" });
  assert.deepEqual(decideStatus({ current: "recorded", issue: { state: "open", labels: [] }, inProgress: true }), { status: "in_progress" });
  assert.equal(decideStatus({ current: "in_progress", issue: { state: "open", labels: [] }, inProgress: true }), null);
  assert.equal(decideStatus({ current: "recorded", issue: { state: "open", labels: [] } }), null);
});

test("fixed+next upgrades once, and only when the tag exists", () => {
  const issue = { state: "closed", state_reason: "completed", labels: [] };
  assert.deepEqual(decideStatus({ current: "fixed", resolvedVersion: "next", issue, version: "v2.10.0" }), { status: "fixed", resolvedVersion: "v2.10.0" });
  assert.equal(decideStatus({ current: "fixed", resolvedVersion: "next", issue, version: null }), null);
  assert.equal(decideStatus({ current: "fixed", resolvedVersion: "v2.10.0", issue, version: "v2.11.0" }), null);
});

test("in_progress is not forgeable by a mention, a foreign repo or a merged PR", () => {
  assert.equal(hasOpenLinkedPull(7, [xref(20, { state: "open", body: "related to #7" })]), false);
  assert.equal(hasOpenLinkedPull(7, [xref(20, { state: "open", repo: "someone/fork" })]), false);
  assert.equal(hasOpenLinkedPull(7, [xref(20, { state: "open", pr: { merged_at: "x" } })]), false);
  assert.equal(hasOpenLinkedPull(7, [{ event: "cross-referenced", source: { issue: { number: 3, state: "open", body: "Fixes #7", repository: { full_name: "esengine/DeepSeek-Reasonix" } } } }]), false);
  assert.equal(hasOpenLinkedPull(7, [xref(20, { state: "open" })]), true);
});

test("only issues the bot filed with the source label are ours", () => {
  const ok = { user: { login: "github-actions[bot]" }, labels: [{ name: "from-studio" }] };
  assert.equal(isOurs(ok), true);
  assert.equal(isOurs({ ...ok, user: { login: "mallory" } }), false);
  assert.equal(isOurs({ ...ok, labels: [] }), false);
});

function fakes({ replies = [], ackFails = false, repliesFails = null, issueOf = (n) => ({ ...BOT, number: n, comments: 1 }), comments = [], commentFails = false, recent = [], recordedFails = false, pendingFails = null, open = [], pending = [item()], issues = {}, changed = [], timeline = [], pulls = {}, tags = [], stable = null, releaseAt = null } = {}) {
  const calls = [];
  return {
    calls,
    log: (m) => calls.push(["log", m]),
    worker: {
      pending: async () => { if (pendingFails) throw pendingFails; return pending; },
      open: async () => open,
      recorded: async (r, b) => { calls.push(["recorded", r, b]); if (recordedFails) throw new Error("boom"); },
      status: async (r, b) => calls.push(["status", r, b]),
      replies: async () => { if (repliesFails) throw repliesFails; return replies; },
      ackReply: async (id) => { calls.push(["ack", id]); if (ackFails) throw new Error("ack boom"); },
    },
    gh: {
      recentIssues: async () => recent,
      changedSince: async (since) => { calls.push(["since", since]); return changed; },
      ensureLabels: async (l) => calls.push(["labels", l]),
      createIssue: async (p) => { calls.push(["create", p]); return { number: 42, html_url: "https://github.com/x/42" }; },
      getIssue: async (n) => { calls.push(["getIssue", n]); return issues[n] ?? issueOf(n); },
      recentComments: async (n) => { calls.push(["comments", n]); return comments; },
      comment: async (n, body) => { calls.push(["comment", n, body]); if (commentFails) throw new Error("gh boom"); },
      timeline: async (n) => { calls.push(["timeline", n]); return timeline; },
      getPull: async (n) => pulls[n],
    },
    tagsContaining: async () => tags,
    stableTags: async () => new Set(stable ?? tags),
    newestReleaseAt: async () => { calls.push(["releaseAt"]); return releaseAt; },
  };
}
const writes = (d) => d.calls.filter((c) => c[0] !== "log").map((c) => c[0]);
const BOT = { user: { login: "github-actions[bot]" }, labels: [{ name: "from-studio" }] };

test("pending item is created once, then recorded", async () => {
  const d = fakes();
  const { failures } = await run(d);
  assert.deepEqual(failures, []);
  assert.deepEqual(writes(d), ["labels", "create", "recorded"]);
  assert.deepEqual(d.calls.find((c) => c[0] === "recorded")[2], { issueNumber: 42, issueUrl: "https://github.com/x/42" });
});

test("failed write-back: next run finds the marker in the recent listing and only re-POSTs", async () => {
  const first = fakes({ recordedFails: true });
  assert.equal((await run(first)).failures.length, 1);
  const second = fakes({ recent: [{ ...BOT, number: 42, html_url: "u", body: renderBody(item()) }] });
  await run(second);
  assert.deepEqual(writes(second), ["recorded"]);
});

test("a forged marker in an issue the bot did not file is not adopted", async () => {
  const d = fakes({ recent: [{ user: { login: "mallory" }, labels: [{ name: "from-studio" }], number: 9, body: marker(R) }] });
  await run(d);
  assert.deepEqual(writes(d), ["labels", "create", "recorded"]);
});

test("malformed receipts never reach GitHub", async () => {
  const d = fakes({ pending: [item({ receipt: 'FB-1" OR x' })] });
  await run(d);
  assert.deepEqual(writes(d), []);
});

test("responses are capped at the limit", async () => {
  const many = Array.from({ length: 30 }, (_, i) => item({ receipt: `FB-AAAA-${"ABCDEFGHJKMNPQRSTVWXYZ23456789"[i]}${"AAA"}` }));
  const d = fakes({ pending: many });
  await run(d, { limit: 5 });
  assert.equal(d.calls.filter((c) => c[0] === "create").length, 5);
});

test("worker errors are warnings, auth misconfiguration is fatal", async () => {
  const d = fakes({ pendingFails: new Error("500") });
  const { failures } = await run(d);
  assert.equal(failures.length, 1);
  assert.ok(d.calls.some((c) => c[0] === "log" && c[1].startsWith("::warning::")));
  await assert.rejects(run(fakes({ pendingFails: new FatalError("401") })), FatalError);
  await assert.rejects(run(fakes({ pendingFails: new TypeError("bug") })), TypeError);
});

test("dry run makes no writes", async () => {
  const d = fakes({ open: [{ receipt: R, issueNumber: 7, status: "recorded" }], changed: [{ number: 7, state: "closed", state_reason: "not_planned", labels: [] }] });
  await run(d, { dryRun: true });
  assert.deepEqual(writes(d).filter((w) => ["labels", "create", "recorded", "status"].includes(w)), []);
});

test("sync reads only issues changed inside the window, not every open row", async () => {
  const d = fakes({
    pending: [],
    open: [{ receipt: R, issueNumber: 7, status: "recorded" }, { receipt: "FB-AAAA-BBBB", issueNumber: 8, status: "recorded" }],
    changed: [{ number: 8, state: "open", labels: [{ name: "in-progress" }] }],
  });
  await run(d, { now: Date.parse("2026-09-30T12:00:00Z") });
  assert.deepEqual(d.calls.find((c) => c[0] === "since"), ["since", "2026-09-30T10:00:00.000Z"]);
  assert.deepEqual(d.calls.filter((c) => c[0] === "timeline"), [["timeline", 8]]);
  assert.deepEqual(d.calls.find((c) => c[0] === "status"), ["status", "FB-AAAA-BBBB", { status: "in_progress" }]);
});

test("closed as completed resolves PR -> commit -> tag; unlisted 'next' entries are upgraded without the window", async () => {
  const sha = "a".repeat(40);
  const base = {
    pending: [],
    timeline: [xref(20), { event: "closed", commit_id: null }],
    pulls: { 20: { merged_at: "2026-09-01T00:00:00Z", merge_commit_sha: sha } },
  };
  const closedIssue = { number: 7, state: "closed", state_reason: "completed", labels: [] };
  const noTag = fakes({ ...base, open: [{ receipt: R, issueNumber: 7, status: "recorded" }], changed: [closedIssue], tags: [] });
  await run(noTag);
  assert.deepEqual(noTag.calls.find((c) => c[0] === "status"), ["status", R, { status: "fixed", resolvedVersion: "next" }]);
  const NOW = Date.parse("2026-09-30T12:00:00Z");
  const later = fakes({ ...base, open: [{ receipt: R, issueNumber: 7, status: "fixed", resolvedVersion: "next" }], issues: { 7: closedIssue }, tags: ["studio-v2.11.0", "studio-v2.10.0"], releaseAt: NOW - 600000 });
  await run(later, { now: NOW });
  assert.ok(!later.calls.some((c) => c[0] === "since"));
  assert.deepEqual(later.calls.find((c) => c[0] === "status"), ["status", R, { status: "fixed", resolvedVersion: "v2.10.0" }]);
  const stillNext = fakes({ ...base, open: [{ receipt: R, issueNumber: 7, status: "fixed", resolvedVersion: "next" }], issues: { 7: closedIssue }, tags: [], releaseAt: NOW - 600000 });
  await run(stillNext, { now: NOW });
  assert.ok(!stillNext.calls.some((c) => c[0] === "status"));
  const prereleaseOnly = fakes({ ...base, open: [{ receipt: R, issueNumber: 7, status: "fixed", resolvedVersion: "next" }], issues: { 7: closedIssue }, tags: ["studio-v2.11.0", "studio-v2.10.0"], stable: ["studio-v2.11.0"], releaseAt: NOW - 600000 });
  await run(prereleaseOnly, { now: NOW });
  assert.deepEqual(prereleaseOnly.calls.find((c) => c[0] === "status"), ["status", R, { status: "fixed", resolvedVersion: "v2.11.0" }]);
});

test("a closed issue without a merged closing PR is never given a version, and is reported", async () => {
  const closedIssue = { number: 7, state: "closed", state_reason: "completed", labels: [] };
  const handClosed = fakes({ pending: [], timeline: [{ event: "closed", commit_id: "b".repeat(40) }], open: [{ receipt: R, issueNumber: 7, status: "fixed", resolvedVersion: "next" }], issues: { 7: closedIssue }, tags: ["studio-v2.11.0"], releaseAt: Date.now() });
  await run(handClosed, { full: true });
  assert.ok(!handClosed.calls.some((c) => c[0] === "status"));
  assert.ok(handClosed.calls.some((c) => c[0] === "log" && /without a merged closing PR/.test(c[1])));
});

test("an open row that omits resolvedVersion is never upgraded", async () => {
  const closedIssue = { number: 7, state: "closed", state_reason: "completed", labels: [] };
  const d = fakes({ pending: [], timeline: [xref(20)], pulls: { 20: { merged_at: "2026-09-01T00:00:00Z", merge_commit_sha: "a".repeat(40) } }, open: [{ receipt: R, issueNumber: 7, status: "fixed" }], issues: { 7: closedIssue }, tags: ["studio-v2.11.0"] });
  await run(d, { full: true });
  assert.ok(!d.calls.some((c) => c[0] === "status"));
});

test("--full re-reads every open row", async () => {
  const d = fakes({ pending: [], open: [{ receipt: R, issueNumber: 7, status: "recorded" }], issues: { 7: { number: 7, state: "open", labels: [{ name: "in-progress" }] } } });
  await run(d, { full: true });
  assert.ok(!d.calls.some((c) => c[0] === "since"));
  assert.deepEqual(d.calls.find((c) => c[0] === "status"), ["status", R, { status: "in_progress" }]);
});

test("a programming error on one row is fatal, an operational one is skipped", async () => {
  const bug = fakes();
  bug.worker.recorded = async () => { throw new TypeError("bug"); };
  await assert.rejects(run(bug), TypeError);
  const flaky = fakes();
  flaky.worker.recorded = async () => { throw new Error("502"); };
  assert.equal((await run(flaky)).failures.length, 1);
});

test("a waiting 'next' entry costs nothing until a Studio release is newer than the window (or on --full)", async () => {
  const NOW = Date.parse("2026-09-30T12:00:00Z");
  const open = [{ receipt: R, issueNumber: 7, status: "fixed", resolvedVersion: "next" }];
  const issues = { 7: { number: 7, state: "closed", state_reason: "completed", labels: [] } };
  const old = fakes({ pending: [], open, issues, releaseAt: NOW - 3 * 3600000 });
  await run(old, { now: NOW });
  assert.deepEqual(writes(old).filter((w) => w !== "releaseAt"), []);
  const none = fakes({ pending: [], open, issues, releaseAt: null });
  await run(none, { now: NOW });
  assert.ok(!none.calls.some((c) => c[0] === "getIssue"));
  const full = fakes({ pending: [], open, issues, releaseAt: NOW - 3 * 3600000 });
  await run(full, { now: NOW, full: true });
  assert.ok(full.calls.some((c) => c[0] === "getIssue"));
});

test("open rows are capped", async () => {
  const letters = "ABCDEFGHJKMNPQRSTVWXYZ23456789";
  const open = Array.from({ length: MAX_OPEN + 50 }, (_, i) => ({ receipt: `FB-AAAA-${letters[i % 30]}${letters[Math.floor(i / 30)]}AA`, issueNumber: i + 1, status: "recorded" }));
  const d = fakes({ pending: [], open, changed: open.map((r) => ({ number: r.issueNumber, state: "open", labels: [{ name: "in-progress" }] })) });
  await run(d);
  assert.equal(d.calls.filter((c) => c[0] === "status").length, MAX_OPEN);
});

test("an unreachable worker is a warning, not a failed job", async () => {
  const real = globalThis.fetch;
  globalThis.fetch = async () => { throw Object.assign(new TypeError("fetch failed"), { cause: { code: "ECONNREFUSED" } }); };
  try {
    const deps = liveDeps({ FEEDBACK_ADMIN_TOKEN: "t" });
    const logs = [];
    const { failures } = await run({ ...deps, log: (m) => logs.push(m) });
    assert.equal(failures.length, 3);
    assert.ok(logs.every((m) => m.startsWith("::warning::") && m.includes("ECONNREFUSED")));
  } finally {
    globalThis.fetch = real;
  }
});

test("the release signal ignores drafts and other release lines", () => {
  const rel = (tag_name, published_at, draft = false, prerelease = false) => ({ tag_name, published_at, draft, prerelease });
  assert.equal(newestStudioRelease([rel("v1.40.0", "2026-09-30T00:00:00Z"), rel("studio-v2.9.0", "2026-09-28T00:00:00Z"), rel("studio-v2.10.0", "2026-09-29T00:00:00Z", true), rel("studio-v2.11.0", "2026-09-30T00:00:00Z", false, true)]), Date.parse("2026-09-28T00:00:00Z"));
  assert.equal(newestStudioRelease([]), null);
});

const reply = (over = {}) => ({ id: "r1", receipt: R, issueNumber: 42, body: "me too @octocat", ...over });
const postedBodies = (d) => d.calls.filter((c) => c[0] === "comment").map((c) => c[2]);

test("only a released item becomes an issue; unreleased images are never linked", async () => {
  assert.equal(publishable(item({ status: "received" })), true);
  assert.equal(publishable(item({ status: undefined })), false);
  for (const status of ["held", "answered", "needs_info", "rejected", "recorded"]) assert.equal(publishable(item({ status })), false, status);
  const d = fakes({ pending: [item({ status: "held", receipt: "FB-AAAA-AAAA" }), item({ status: "needs_info", receipt: "FB-AAAA-BBBB" }), item({ status: "rejected", receipt: "FB-AAAA-CCCC" }), item({ status: "received" }), item({ status: undefined, receipt: "FB-AAAA-DDDD" })] });
  await run(d);
  assert.ok(d.calls.some((c) => c[0] === "log" && c[1].includes("FB-AAAA-DDDD") && c[1].startsWith("::warning::")));
  const created = d.calls.filter((c) => c[0] === "create");
  assert.equal(created.length, 1);
  const body = created[0][1].body;
  assert.ok(body.includes(`${ATTACHMENT_PREFIX}abc`));
  assert.ok(!body.includes("private") && !body.includes("bare"));
});

test("a user reply becomes one bot comment on its issue, then is acked", async () => {
  const d = fakes({ pending: [], replies: [reply()] });
  assert.deepEqual((await run(d)).failures, []);
  assert.deepEqual(writes(d), ["getIssue", "comments", "comment", "ack"]);
  assert.equal(postedBodies(d)[0], renderReply(reply()));
  assert.deepEqual(d.calls.find((c) => c[0] === "ack"), ["ack", "r1"]);
});

test("replies are idempotent: a comment carrying the marker is adopted, a forged one is not", async () => {
  const mine = { user: { login: "github-actions[bot]" }, body: renderReply(reply()) };
  const forged = { user: { login: "mallory" }, body: renderReply(reply()) };
  const inside = { user: { login: "github-actions[bot]" }, body: `${replyMarker("r1")}\nmore text` };
  const a = fakes({ pending: [], replies: [reply()], comments: [mine] });
  await run(a);
  assert.deepEqual(writes(a), ["getIssue", "comments", "ack"]);
  const b = fakes({ pending: [], replies: [reply()], comments: [forged, inside] });
  await run(b);
  assert.deepEqual(writes(b), ["getIssue", "comments", "comment", "ack"]);
});

test("a failed ack or post is a warning and retried; the post is never skipped silently", async () => {
  const a = fakes({ pending: [], replies: [reply()], ackFails: true });
  assert.equal((await run(a)).failures.length, 1);
  const b = fakes({ pending: [], replies: [reply()], commentFails: true });
  assert.equal((await run(b)).failures.length, 1);
  assert.ok(!writes(b).includes("ack"));
  const c = fakes({ pending: [], repliesFails: new Error("500") });
  assert.equal((await run(c)).failures.length, 1);
});

test("replies without an issue, malformed ids, and issues the bot did not file never get a comment", async () => {
  const d = fakes({ pending: [], replies: [reply({ issueNumber: null }), reply({ id: "x y", issueNumber: 5 }), reply({ id: "r2", receipt: "bad", issueNumber: 5 }), reply({ id: "r3", issueNumber: 7 })], issueOf: (n) => ({ user: { login: "mallory" }, labels: [], number: n }) });
  await run(d);
  assert.deepEqual(postedBodies(d), []);
  assert.deepEqual(d.calls.filter((c) => c[0] === "ack"), [["ack", "r3"]]);
});

test("reply mirroring is bounded, deduplicated and idle runs cost no GitHub call", async () => {
  const many = Array.from({ length: 30 }, (_, i) => reply({ id: `r${i}` }));
  const d = fakes({ pending: [], replies: [...many, reply({ id: "r0" })] });
  await run(d, { limit: 20 });
  assert.equal(postedBodies(d).length, MAX_REPLIES);
  const idle = fakes({ pending: [] });
  await run(idle);
  assert.deepEqual(writes(idle), []);
});

test("when every returned reply is skipped, one warning says how many", async () => {
  const d = fakes({ pending: [], replies: [reply({ id: "a", issueNumber: null }), reply({ id: "b", issueNumber: null })] });
  await run(d);
  const warns = d.calls.filter((c) => c[0] === "log" && c[1].includes("all 2 returned"));
  assert.equal(warns.length, 1);
  assert.deepEqual(writes(d), []);
});

test("dry run mirrors nothing", async () => {
  const d = fakes({ pending: [], replies: [reply()] });
  await run(d, { dryRun: true });
  assert.ok(!writes(d).includes("comment") && !writes(d).includes("ack"));
});

test("contact is never read, rendered or sent by the converter", async () => {
  const secret = "SECRET-CONTACT-VALUE";
  const d = fakes({
    pending: [item({ contact: secret, displayName: "n", env: { version: "1", contact: secret } })],
    replies: [reply({ contact: secret })],
    open: [{ receipt: R, issueNumber: 42, status: "recorded", contact: secret }],
    issues: { 42: { ...BOT, number: 42, comments: 1, state: "closed", contact: secret } },
  });
  await run(d, { full: true });
  assert.ok(!JSON.stringify(d.calls).includes(secret));
  assert.ok(!renderBody(item({ contact: secret })).includes(secret) && !renderReply(reply({ contact: secret })).includes(secret));
  const source = readFileSync(new URL("./feedback-sync.mjs", import.meta.url), "utf8");
  assert.ok(!/contact/i.test(source), "the converter must not name a contact field at all");
});
