import assert from "node:assert/strict";
import { test } from "node:test";
import {
  contributorLogins,
  CreditError,
  CreditErrorCode,
  creditExclusions,
  creditSuffix,
  githubRefLookup,
  isBotAccount,
  reportWarnings,
  resolveCredits,
  tokenFromEnvironment,
  unresolvedError,
} from "./release-credits.mjs";

function jsonResponse(status, body, headers = {}) {
  return {
    status,
    ok: status >= 200 && status < 300,
    headers: { get: (name) => headers[name] ?? null },
    json: async () => body,
  };
}

// routes: number -> node | "discussion" | Error | [status, body, headers] |
// an array of those, answered in turn; a missing number is NOT_FOUND.
function fakeGitHub(routes) {
  const calls = [];
  const fetchImpl = async (url, init) => {
    const { variables } = JSON.parse(init.body);
    calls.push({ url, init, variables });
    let route = routes[variables.number];
    if (route?.sequence) route = route.sequence.shift();
    if (route instanceof Error) throw route;
    if (Array.isArray(route)) return jsonResponse(...route);
    if (route && route !== "discussion") {
      return jsonResponse(200, { data: { repository: { discussion: null, issueOrPullRequest: route } } });
    }
    const errors = [{ type: "NOT_FOUND", message: "Could not resolve" }];
    if (!route) errors.push({ type: "NOT_FOUND", message: "Could not resolve" });
    return jsonResponse(200, {
      data: { repository: { discussion: route ? { number: variables.number } : null, issueOrPullRequest: null } },
      errors,
    });
  };
  return { calls, fetchImpl };
}

const noSleep = { sleep: async () => {} };

const user = (login, type = "User") => ({ login, __typename: type });
const pull = (login, type) => ({ __typename: "PullRequest", author: user(login, type) });
const linked = (number, login, merged = true, type) => ({ number, merged, author: user(login, type) });
const issue = (references = [], closer = null) => ({
  __typename: "Issue",
  closedByPullRequestsReferences: { nodes: references },
  timelineItems: { nodes: closer ? [{ closer }] : [] },
});

test("a pull request credits its author", async () => {
  const { calls, fetchImpl } = fakeGitHub({ 1: pull("alice") });
  const lookup = githubRefLookup({ repository: "o/r", token: "t", fetchImpl });
  assert.deepEqual(await lookup(1), { kind: "pull", login: "alice", bot: false });
  assert.equal(calls[0].url, "https://api.github.com/graphql");
  assert.equal(calls[0].init.headers.Authorization, "Bearer t");
  assert.deepEqual(calls[0].variables, { owner: "o", name: "r", number: 1 });
});

test("an issue credits the merged pull requests that closed it, and nothing else", async () => {
  const { fetchImpl } = fakeGitHub({
    2: issue([linked(12, "bob"), linked(11, "carol", false)], { __typename: "PullRequest", ...linked(10, "alice") }),
    3: issue([], { __typename: "Commit" }),
    4: issue([linked(14, "bob"), linked(14, "bob")], { __typename: "PullRequest", ...linked(14, "bob") }),
  });
  const lookup = githubRefLookup({ repository: "o/r", fetchImpl });
  assert.deepEqual(await lookup(2), {
    kind: "issue",
    fixes: [
      { number: 10, login: "alice", bot: false },
      { number: 12, login: "bob", bot: false },
    ],
  });
  assert.deepEqual(await lookup(3), { kind: "issue", fixes: [] });
  assert.deepEqual(await lookup(4), { kind: "issue", fixes: [{ number: 14, login: "bob", bot: false }] });
});

test("bot and deleted authors are never credited", async () => {
  const { fetchImpl } = fakeGitHub({
    1: pull("github-actions", "Bot"),
    2: pull("dependabot"),
    3: pull("renovate[bot]"),
    4: { __typename: "PullRequest", author: null },
    5: issue([linked(15, "dependabot[bot]")]),
  });
  const lookup = githubRefLookup({ repository: "o/r", fetchImpl });
  for (const ref of [1, 2, 3, 4]) assert.equal((await lookup(ref)).bot, true);
  const fixed = await lookup(5);
  assert.equal(fixed.fixes[0].bot, true);
  assert.equal(creditSuffix(fixed), " fixed in #15");
  assert.equal(isBotAccount("github-actions"), true);
  assert.equal(isBotAccount("esengine"), false);
});

test("a discussion or a number naming nothing resolves, and warns instead of failing", async () => {
  const { fetchImpl } = fakeGitHub({ 8: "discussion", 1: pull("alice") });
  const lookup = githubRefLookup({ repository: "o/r", fetchImpl });
  assert.deepEqual(await lookup(8), { kind: "discussion" });
  assert.deepEqual(await lookup(9), { kind: "missing" });
  const { credits, failures, warnings } = await resolveCredits([8, 1, 9], lookup);
  assert.deepEqual(failures, []);
  assert.equal(creditSuffix(credits.get(8)), "");
  assert.deepEqual(
    warnings.map((warning) => [warning.ref, warning.code]),
    [
      [8, CreditErrorCode.discussion],
      [9, CreditErrorCode.notFound],
    ],
  );
  const lines = [];
  reportWarnings(warnings, { env: { GITHUB_ACTIONS: "true" }, write: (line) => lines.push(line) });
  assert.ok(lines.every((line) => line.startsWith("::warning::release_credits.")));
});

test("transient failures are retried with backoff; the last one is reported by code", async () => {
  const slept = [];
  const sleep = async (ms) => slept.push(ms);
  const { calls, fetchImpl } = fakeGitHub({
    1: { sequence: [[502, {}], new TypeError("socket hang up"), [429, {}, { "retry-after": "7" }], pull("alice")] },
    2: [503, {}],
    3: new TypeError("fetch failed"),
    4: { sequence: [[200, { data: null, errors: [{ type: "RATE_LIMITED" }] }], pull("bob")] },
    5: [403, {}, { "x-ratelimit-remaining": "0" }],
  });
  const lookup = githubRefLookup({ repository: "o/r", fetchImpl, sleep });
  assert.equal((await lookup(1)).login, "alice");
  assert.deepEqual(slept, [1000, 2000, 7000]);
  await assert.rejects(lookup(2), { code: CreditErrorCode.status, ref: 2 });
  await assert.rejects(lookup(3), { code: CreditErrorCode.unreachable, ref: 3 });
  assert.equal((await lookup(4)).login, "bob");
  await assert.rejects(lookup(5), { code: CreditErrorCode.status, ref: 5 });
  const tries = (ref) => calls.filter((call) => call.variables.number === ref).length;
  assert.deepEqual([1, 2, 3, 4, 5].map(tries), [4, 4, 4, 2, 4]);
});

test("an authorisation failure is final and never retried", async () => {
  const { calls, fetchImpl } = fakeGitHub({ 1: [401, {}], 2: [403, {}], 3: [200, { data: null, errors: [{ type: "FORBIDDEN" }] }] });
  const lookup = githubRefLookup({ repository: "o/r", fetchImpl, ...noSleep });
  await assert.rejects(lookup(1), { code: CreditErrorCode.status });
  await assert.rejects(lookup(2), { code: CreditErrorCode.status });
  await assert.rejects(lookup(3), { code: CreditErrorCode.status });
  assert.equal(calls.length, 3);
});

test("suffixes use one wording for pull requests and fixed issues", () => {
  assert.equal(creditSuffix({ kind: "pull", login: "alice", bot: false }), " by @alice");
  assert.equal(creditSuffix({ kind: "pull", login: "x[bot]", bot: true }), "");
  assert.equal(creditSuffix({ kind: "issue", fixes: [] }), "");
  assert.equal(
    creditSuffix({
      kind: "issue",
      fixes: [
        { number: 2, login: "alice", bot: false },
        { number: 3, login: "bob", bot: false },
      ],
    }),
    " fixed in #2 by @alice and #3 by @bob",
  );
});

test("each distinct ref is looked up once and failures are collected in ref order", async () => {
  const seen = [];
  const lookup = async (ref) => {
    seen.push(ref);
    if (ref === 3 || ref === 1) throw new CreditError(CreditErrorCode.notFound, `#${ref}`, { ref });
    return { kind: "pull", login: `u${ref}`, bot: false };
  };
  const { credits, failures } = await resolveCredits([3, 2, 3, 1, 2], lookup, { concurrency: 2 });
  assert.deepEqual(seen.sort(), [1, 2, 3]);
  assert.deepEqual([...credits.keys()], [2]);
  assert.deepEqual(failures.map((failure) => failure.ref), [3, 1]);
  const error = unresolvedError(failures);
  assert.equal(error.code, CreditErrorCode.unresolved);
  assert.deepEqual(error.failures.map((failure) => failure.code), [CreditErrorCode.notFound, CreditErrorCode.notFound]);
});

test("an unexpected lookup error is not swallowed", async () => {
  await assert.rejects(
    resolveCredits([1], async () => {
      throw new RangeError("bug");
    }),
    RangeError,
  );
});

test("contributors are unique humans in first-appearance order, fixers included", () => {
  const credits = new Map([
    [1, { kind: "pull", login: "bob", bot: false }],
    [2, { kind: "pull", login: "alice", bot: false }],
    [3, { kind: "issue", fixes: [{ number: 30, login: "carol", bot: false }] }],
    [4, { kind: "pull", login: "github-actions[bot]", bot: true }],
    [5, { kind: "pull", login: "Bob", bot: false }],
    [6, { kind: "issue", fixes: [] }],
  ]);
  assert.deepEqual(contributorLogins([3, 1, 4, 2, 1, 5, 6, 9], credits), ["carol", "bob", "alice"]);
});

test("the token comes from the environment, then gh, else a coded error", () => {
  assert.equal(tokenFromEnvironment({ GH_TOKEN: "a", GITHUB_TOKEN: "b" }, () => "c"), "a");
  assert.equal(tokenFromEnvironment({ GITHUB_TOKEN: "b" }, () => "c"), "b");
  assert.equal(tokenFromEnvironment({}, () => "c\n"), "c");
  assert.throws(
    () =>
      tokenFromEnvironment({}, () => {
        throw new Error("gh: not logged in");
      }),
    { code: CreditErrorCode.noToken },
  );
});

test("the repository owner and excluded logins are left as written, a community author is credited", async () => {
  const { fetchImpl } = fakeGitHub({
    1: pull("Owner"),
    2: pull("alice"),
    3: issue([linked(30, "owner"), linked(31, "alice")]),
    4: pull("helper"),
  });
  const lookup = githubRefLookup({ repository: "owner/r", fetchImpl });
  const credits = new Map([
    [1, await lookup(1)],
    [2, await lookup(2)],
    [3, await lookup(3)],
  ]);
  assert.equal(creditSuffix(credits.get(1)), "");
  assert.equal(creditSuffix(credits.get(2)), " by @alice");
  assert.equal(creditSuffix(credits.get(3)), " fixed in #30 and #31 by @alice");
  assert.deepEqual(contributorLogins([1, 2, 3], credits), ["alice"]);
  const excluding = githubRefLookup({ repository: "owner/r", fetchImpl, excluded: creditExclusions("owner/r", { RELEASE_CREDIT_EXCLUDE: " Helper, ," }) });
  assert.equal(creditSuffix(await excluding(4)), "");
  assert.deepEqual([...creditExclusions("o/r", {})], ["o"]);
});
