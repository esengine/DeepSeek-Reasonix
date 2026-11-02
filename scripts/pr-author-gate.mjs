#!/usr/bin/env node
// Closes a newly opened pull request whose author is not on the allowlist.
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const MAINTAINERS = new Set(["OWNER", "MEMBER", "COLLABORATOR"]);

export function parseAllowlist(text) {
  return new Set(text.split("\n").map((line) => line.trim()).filter((line) => line && !line.startsWith("#"))
    .map((login) => login.toLowerCase()));
}

export function decide({ association, authorType, login, allowed }) {
  if (authorType === "Bot" || MAINTAINERS.has(association)) return "allow";
  return allowed.has(login.toLowerCase()) ? "allow" : "close";
}

export const CLOSING_COMMENT = `Thanks for wanting to contribute. Pull requests are currently limited to the maintainers and a small set of invited contributors, so this one has been closed automatically; nothing is wrong with it.

Please open an issue instead (a bug report with the steps to reproduce is the most useful). A maintainer will look at it, and if you would like to fix it yourself, say so there: if a maintainer agrees, this pull request can be reopened.`;

function gh(args, input) {
  const result = spawnSync("gh", args, { encoding: "utf8", input, maxBuffer: 4 * 1024 * 1024 });
  if (result.status !== 0) throw new Error(`gh ${args[0]} ${args[1]} failed: ${result.stderr}`);
  return result.stdout;
}

export function run({ event, repository, allowlist, exec = gh, log = console.log }) {
  const pr = event.pull_request;
  const verdict = decide({
    association: pr.author_association, authorType: pr.user.type, login: pr.user.login, allowed: parseAllowlist(allowlist),
  });
  log(`#${pr.number} by ${pr.user.login} (${pr.author_association}): ${verdict}`);
  if (verdict === "allow") return verdict;
  exec(["pr", "comment", String(pr.number), "--repo", repository, "--body-file", "-"], CLOSING_COMMENT);
  exec(["pr", "close", String(pr.number), "--repo", repository]);
  return verdict;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, "utf8"));
  const allowlist = readFileSync(new URL("../.github/pr-authors.txt", import.meta.url), "utf8");
  run({ event, repository: process.env.GITHUB_REPOSITORY, allowlist });
}
