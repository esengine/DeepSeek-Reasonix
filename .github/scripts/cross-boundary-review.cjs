"use strict";

const STATUS_CONTEXT = "independent cross-boundary review";
const ALLOWED_PERMISSIONS = new Set(["admin", "maintain", "write"]);

function pullNumberFromContext(context) {
  const direct = context.payload.pull_request?.number;
  const fromWorkflowRun = context.payload.workflow_run?.pull_requests?.[0]?.number;
  const fromDispatch = Number(context.payload.inputs?.pull_number ?? 0);
  return direct ?? fromWorkflowRun ?? (Number.isInteger(fromDispatch) && fromDispatch > 0 ? fromDispatch : undefined);
}

function changeSummary(files) {
  const groups = new Set();
  for (const { filename } of files) {
    if (filename.startsWith("desktop/frontend/")) groups.add("frontend");
    else if (filename.startsWith("desktop/")) groups.add("desktop-native");
    else if (/^(?:internal|cmd)\//.test(filename)) groups.add("kernel");
    else if (/^(?:\.github|scripts|tools)\//.test(filename)) groups.add("control-plane");
    else if (/^(?:sdk|npm|site|docs|workers)\//.test(filename)) groups.add("distribution");
  }
  const changedLines = files.reduce((sum, file) => sum + file.changes, 0);
  return {
    changedLines,
    groups: [...groups],
    required: files.length >= 25 || changedLines >= 1000 || groups.size >= 3,
  };
}

function currentHeadApprovalCandidates(reviews, pull) {
  const latestByReviewer = new Map();
  for (const review of reviews) {
    const login = review.user?.login;
    if (!login || login === pull.user.login || review.user?.type === "Bot") continue;
    // Comments do not revoke an approval; only explicit review decisions do.
    if (review.state === "COMMENTED" || review.state === "PENDING") continue;
    latestByReviewer.set(login, { state: review.state, commitId: review.commit_id });
  }
  return [...latestByReviewer.entries()]
    .filter(([, review]) => review.state === "APPROVED" && review.commitId === pull.head.sha)
    .map(([login]) => login);
}

async function publishStatus({ github, context, sha, state, description }) {
  await github.rest.repos.createCommitStatus({
    owner: context.repo.owner,
    repo: context.repo.repo,
    sha,
    state,
    context: STATUS_CONTEXT,
    description: description.slice(0, 140),
    target_url: `${context.serverUrl ?? "https://github.com"}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}`,
  });
}

async function run({ github, context, core }) {
  let pullNumber = pullNumberFromContext(context);
  if (!pullNumber && context.payload.workflow_run?.head_sha) {
    const associated = await github.rest.repos.listPullRequestsAssociatedWithCommit({
      owner: context.repo.owner,
      repo: context.repo.repo,
      commit_sha: context.payload.workflow_run.head_sha,
    });
    const pull = associated.data.find((candidate) =>
      candidate.state === "open" && candidate.base?.repo?.full_name === `${context.repo.owner}/${context.repo.repo}`,
    );
    pullNumber = pull?.number;
  }
  if (!pullNumber) {
    core.info("No pull request is associated with this event; nothing to reevaluate.");
    return;
  }

  let pull;
  try {
    ({ data: pull } = await github.rest.pulls.get({
      owner: context.repo.owner,
      repo: context.repo.repo,
      pull_number: pullNumber,
    }));
    if (pull.state !== "open") {
      core.info(`Pull request #${pull.number} is not open; nothing to reevaluate.`);
      return;
    }
    if (pull.draft) {
      await publishStatus({
        github,
        context,
        sha: pull.head.sha,
        state: "success",
        description: "Draft PR; independent review is deferred",
      });
      return;
    }

    const files = await github.paginate(github.rest.pulls.listFiles, {
      owner: context.repo.owner,
      repo: context.repo.repo,
      pull_number: pull.number,
      per_page: 100,
    });
    const summary = changeSummary(files);
    if (!summary.required) {
      await publishStatus({
        github,
        context,
        sha: pull.head.sha,
        state: "success",
        description: [
          `${files.length} files`,
          `${summary.changedLines} lines`,
          `${summary.groups.length} owner groups; review gate not required`,
        ].join(", "),
      });
      return;
    }

    const reviews = await github.paginate(github.rest.pulls.listReviews, {
      owner: context.repo.owner,
      repo: context.repo.repo,
      pull_number: pull.number,
      per_page: 100,
    });
    const approvers = [];
    for (const login of currentHeadApprovalCandidates(reviews, pull)) {
      try {
        const permission = await github.rest.repos.getCollaboratorPermissionLevel({
          owner: context.repo.owner,
          repo: context.repo.repo,
          username: login,
        });
        if (ALLOWED_PERMISSIONS.has(permission.data.permission)) approvers.push(login);
      } catch (error) {
        core.info(`Ignoring approval from ${login}: repository permission could not be verified (${error.status ?? "unknown"}).`);
      }
    }

    if (approvers.length === 0) {
      await publishStatus({
        github,
        context,
        sha: pull.head.sha,
        state: "failure",
        description: "Large cross-boundary change needs current-head approval from a non-author maintainer",
      });
      const areas = summary.groups.join(", ") || "one uncategorized area";
      core.warning(
        `PR changes ${files.length} files and ${summary.changedLines} lines across ${areas}; ` +
        "split it or obtain current-head approval from a non-author maintainer.",
      );
      return;
    }

    await publishStatus({
      github,
      context,
      sha: pull.head.sha,
      state: "success",
      description: `Current-head independent review approved by ${approvers.join(", ")}`,
    });
    core.info(`Independent cross-boundary review approved by ${approvers.join(", ")}.`);
  } catch (error) {
    if (pull?.head?.sha) {
      try {
        await publishStatus({
          github,
          context,
          sha: pull.head.sha,
          state: "error",
          description: "Independent review policy could not be evaluated",
        });
      } catch (statusError) {
        core.info(`Could not publish evaluation error status: ${statusError.message}`);
      }
    }
    throw error;
  }
}

module.exports = run;
module.exports.changeSummary = changeSummary;
module.exports.currentHeadApprovalCandidates = currentHeadApprovalCandidates;
module.exports.pullNumberFromContext = pullNumberFromContext;
module.exports.STATUS_CONTEXT = STATUS_CONTEXT;
