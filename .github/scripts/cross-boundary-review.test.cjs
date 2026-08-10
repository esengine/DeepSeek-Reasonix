"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const gate = require("./cross-boundary-review.cjs");
const {
  STATUS_CONTEXT,
  changeSummary,
  currentHeadApprovalCandidates,
  pullNumberFromContext,
} = require("./cross-boundary-review.cjs");

function gateHarness(reviews) {
  const listFiles = () => {};
  const listReviews = () => {};
  const statuses = [];
  const github = {
    rest: {
      pulls: {
        get: async () => ({ data: { number: 7, state: "open", draft: false, user: { login: "author" }, head: { sha: "current" } } }),
        listFiles,
        listReviews,
      },
      repos: {
        createCommitStatus: async (status) => statuses.push(status),
        getCollaboratorPermissionLevel: async () => ({ data: { permission: "write" } }),
      },
    },
    paginate: async (endpoint) => endpoint === listFiles
      ? [
          { filename: "desktop/frontend/src/App.tsx", changes: 10 },
          { filename: "desktop/main.go", changes: 10 },
          { filename: ".github/workflows/ci.yml", changes: 10 },
        ]
      : reviews,
  };
  const context = {
    payload: { pull_request: { number: 7 } },
    repo: { owner: "example", repo: "reasonix" },
    runId: 99,
    serverUrl: "https://github.example",
  };
  const core = { info() {}, warning() {} };
  return { github, context, core, statuses };
}

test("large multi-owner changes require independent review", () => {
  const files = [
    { filename: "desktop/frontend/src/App.tsx", changes: 10 },
    { filename: "desktop/main.go", changes: 10 },
    { filename: ".github/workflows/ci.yml", changes: 10 },
  ];
  assert.deepEqual(changeSummary(files), {
    changedLines: 30,
    groups: ["frontend", "desktop-native", "control-plane"],
    required: true,
  });
});

test("approval must target the current pull request head", () => {
  const pull = { user: { login: "author" }, head: { sha: "current" } };
  const reviews = [
    { user: { login: "stale", type: "User" }, state: "APPROVED", commit_id: "previous" },
    { user: { login: "current", type: "User" }, state: "APPROVED", commit_id: "current" },
    { user: { login: "author", type: "User" }, state: "APPROVED", commit_id: "current" },
    { user: { login: "automation", type: "Bot" }, state: "APPROVED", commit_id: "current" },
  ];
  assert.deepEqual(currentHeadApprovalCandidates(reviews, pull), ["current"]);
});

test("later explicit review decisions replace approval without comments revoking it", () => {
  const pull = { user: { login: "author" }, head: { sha: "current" } };
  const reviews = [
    { user: { login: "maintainer", type: "User" }, state: "APPROVED", commit_id: "current" },
    { user: { login: "maintainer", type: "User" }, state: "COMMENTED", commit_id: "current" },
    { user: { login: "maintainer", type: "User" }, state: "CHANGES_REQUESTED", commit_id: "current" },
  ];
  assert.deepEqual(currentHeadApprovalCandidates(reviews, pull), []);
});

test("protected events resolve the intended pull request", () => {
  assert.equal(pullNumberFromContext({ payload: { pull_request: { number: 12 } } }), 12);
  assert.equal(pullNumberFromContext({ payload: { workflow_run: { pull_requests: [{ number: 34 }] } } }), 34);
  assert.equal(pullNumberFromContext({ payload: { inputs: { pull_number: "56" } } }), 56);
  assert.equal(STATUS_CONTEXT, "independent cross-boundary review");
});

test("stale approval publishes a failing current-head status", async () => {
  const harness = gateHarness([
    { user: { login: "maintainer", type: "User" }, state: "APPROVED", commit_id: "previous" },
  ]);
  await gate(harness);
  assert.equal(harness.statuses.length, 1);
  assert.equal(harness.statuses[0].sha, "current");
  assert.equal(harness.statuses[0].state, "failure");
  assert.equal(harness.statuses[0].context, STATUS_CONTEXT);
});

test("current-head maintainer approval publishes success", async () => {
  const harness = gateHarness([
    { user: { login: "maintainer", type: "User" }, state: "APPROVED", commit_id: "current" },
  ]);
  await gate(harness);
  assert.equal(harness.statuses.length, 1);
  assert.equal(harness.statuses[0].state, "success");
});
