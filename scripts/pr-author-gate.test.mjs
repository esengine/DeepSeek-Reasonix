import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { CLOSING_COMMENT, decide, parseAllowlist, run } from "./pr-author-gate.mjs";

const allowlist = "# comment\nLancy69\n\n  kevin9327  \n";
const event = (login, association = "NONE", type = "User") => ({
  pull_request: { number: 7, author_association: association, user: { login, type } },
});
const harness = () => {
  const calls = [];
  return { calls, exec: (args, input) => { calls.push({ args, input }); return ""; } };
};
const go = (e, h) => run({ event: e, repository: "o/r", allowlist, exec: h.exec, log: () => {} });

test("the allowlist ignores comments and blanks and matches case-insensitively", () => {
  assert.deepEqual([...parseAllowlist(allowlist)], ["lancy69", "kevin9327"]);
  assert.equal(go(event("LANCY69", "CONTRIBUTOR"), harness()), "allow");
  assert.equal(go(event("kevin9327", "FIRST_TIME_CONTRIBUTOR"), harness()), "allow");
});

for (const association of ["OWNER", "MEMBER", "COLLABORATOR"]) {
  test(`${association} is never gated`, () => {
    const h = harness();
    assert.equal(go(event("maintainer", association), h), "allow");
    assert.deepEqual(h.calls, []);
  });
}

test("a bot such as dependabot is never gated", () => {
  const h = harness();
  assert.equal(go(event("dependabot[bot]", "NONE", "Bot"), h), "allow");
  assert.deepEqual(h.calls, []);
});

for (const association of ["CONTRIBUTOR", "FIRST_TIME_CONTRIBUTOR", "FIRST_TIMER", "NONE", "MANNEQUIN"]) {
  test(`${association} who is not on the list is commented and closed, whatever their history`, () => {
    const h = harness();
    assert.equal(go(event("someone-else", association), h), "close");
    assert.deepEqual(h.calls.map((c) => c.args.slice(0, 3)), [["pr", "comment", "7"], ["pr", "close", "7"]]);
    assert.equal(h.calls[0].input, CLOSING_COMMENT);
  });
}

test("decide admits only maintainers, bots and listed logins", () => {
  const allowed = new Set(["a"]);
  assert.equal(decide({ association: "NONE", authorType: "User", login: "A", allowed }), "allow");
  assert.equal(decide({ association: "CONTRIBUTOR", authorType: "User", login: "b", allowed }), "close");
});

test("the shipped allowlist parses and the workflow is safe", () => {
  assert.ok(parseAllowlist(readFileSync(new URL("../.github/pr-authors.txt", import.meta.url), "utf8")).size >= 1);
  const text = readFileSync(new URL("../.github/workflows/pr-author-gate.yml", import.meta.url), "utf8");
  assert.match(text, /pull_request_target:\n\s+types: \[opened\]/);
  assert.doesNotMatch(text, /head\.(sha|ref)|refs\/pull/);
  assert.match(text, /permissions:\n\s+contents: read\n\s+pull-requests: write/);
  assert.match(text, /node scripts\/pr-author-gate\.mjs/);
});
