#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { dirname } from "node:path";
import { loadCatalog, upsertRelease, validateCatalog } from "./release-notes.mjs";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const apiBase = process.env.DEEPSEEK_API_BASE || "https://api.deepseek.com";
const model = process.env.DEEPSEEK_MODEL || "deepseek-v4-pro";

function parseArgs(argv) {
  const values = {};
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (!arg.startsWith("--")) throw new Error(`unexpected argument ${arg}`);
    values[arg.slice(2)] = argv[++index];
  }
  return values;
}

function runGit(args) {
  return execFileSync("git", args, { cwd: repoRoot, encoding: "utf8" }).trim();
}

function normalizeVersion(version) {
  return String(version || "").replace(/^(?:desktop-|npm-)?v/, "");
}

function repositoryName() {
  if (process.env.GITHUB_REPOSITORY) return process.env.GITHUB_REPOSITORY;
  const remote = runGit(["remote", "get-url", "origin"]);
  const match = remote.match(/github\.com[/:]([^/]+\/[^/.]+)(?:\.git)?$/);
  if (!match) throw new Error("cannot determine GitHub repository; set GITHUB_REPOSITORY");
  return match[1];
}

function commitRange(from, to) {
  return runGit(["log", "--first-parent", "--format=%H%x09%s%x09%b%x00", `${from}..${to}`])
    .split("\0")
    .map((record) => record.trim())
    .filter(Boolean)
    .map((record) => {
      const [sha, subject, ...body] = record.split("\t");
      return { sha, subject, body: body.join("\t").trim() };
    });
}

function prNumbersFromCommits(commits) {
  const refs = new Set();
  for (const commit of commits) {
    for (const match of `${commit.subject}\n${commit.body}`.matchAll(/#(\d+)/g)) refs.add(Number(match[1]));
  }
  return [...refs];
}

async function githubJson(path, { allowMissing = false } = {}) {
  const headers = { Accept: "application/vnd.github+json", "User-Agent": "reasonix-release-notes" };
  if (process.env.GITHUB_TOKEN || process.env.GH_TOKEN) {
    headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN || process.env.GH_TOKEN}`;
  }
  const response = await fetch(`https://api.github.com${path}`, { headers, signal: AbortSignal.timeout(30_000) });
  if (allowMissing && response.status === 404) return null;
  if (!response.ok) throw new Error(`GitHub API ${path} failed: ${response.status}`);
  return response.json();
}

async function collectPullRequests(repository, commits) {
  const numbers = new Set(prNumbersFromCommits(commits));
  const associated = await Promise.all(
    commits.map((commit) => githubJson(`/repos/${repository}/commits/${commit.sha}/pulls`, { allowMissing: true })),
  );
  for (const pulls of associated) for (const pull of pulls || []) numbers.add(pull.number);
  const pulls = await Promise.all([...numbers].map((number) => githubJson(`/repos/${repository}/pulls/${number}`, { allowMissing: true })));
  return pulls.filter(Boolean).map((pull) => ({
    number: pull.number,
    title: pull.title,
    body: String(pull.body || "").slice(0, 2000),
    author: pull.user?.login || "",
    labels: (pull.labels || []).map((label) => label.name),
  }));
}

function collectDocLinks(from, repository, to) {
  const linkRef = runGit(["rev-parse", to]);
  const paths = runGit(["diff", "--name-only", `${from}..${to}`])
    .split("\n")
    .filter((path) => /^(?:docs|README)[/\w.-]*\.(?:md|mdx)$/i.test(path));
  return paths.map((path) => `https://github.com/${repository}/blob/${linkRef}/${path}`);
}

function assertGroundedRefs(value, allowedRefs, path = "release") {
  if (Array.isArray(value)) {
    value.forEach((item, index) => assertGroundedRefs(item, allowedRefs, `${path}[${index}]`));
    return;
  }
  if (!value || typeof value !== "object") return;
  if (Array.isArray(value.refs)) {
    for (const ref of value.refs) {
      if (!allowedRefs.has(ref)) throw new Error(`${path}.refs contains PR #${ref}, which is outside the release range`);
    }
  }
  for (const [key, child] of Object.entries(value)) assertGroundedRefs(child, allowedRefs, `${path}.${key}`);
}

function extractJson(content) {
  if (!content?.trim()) throw new Error("DeepSeek returned empty content");
  const parsed = JSON.parse(content);
  return parsed.release || parsed;
}

async function askDeepSeek(payload, retry = true) {
  const key = process.env.DEEPSEEK_API_KEY;
  if (!key) throw new Error("DEEPSEEK_API_KEY is required");
  const response = await fetch(`${apiBase.replace(/\/$/, "")}/chat/completions`, {
    method: "POST",
    headers: { Authorization: `Bearer ${key}`, "Content-Type": "application/json" },
    body: JSON.stringify({
      model,
      temperature: 0,
      max_tokens: 8000,
      response_format: { type: "json_object" },
      messages: [
        {
          role: "system",
          content: `You are Reasonix's release editor. Return one JSON object with a \"release\" property. Write factual, user-facing product release notes in equivalent English and Simplified Chinese. Group changes by user outcome, not by commit. Never invent capabilities, migrations, risks, PR numbers, contributors, URLs, or metrics. Every highlight and change must cite one or more supplied PR numbers. Use this exact release shape:
{
  \"version\": \"semver\", \"date\": \"YYYY-MM-DD\", \"channel\": \"stable|prerelease\",
  \"title\": {\"en\":\"\",\"zh\":\"\"}, \"summary\": {\"en\":\"\",\"zh\":\"\"},
  \"surfaces\": [\"desktop\"],
  \"guides\": [{\"title\":{\"en\":\"\",\"zh\":\"\"},\"body\":{\"en\":\"\",\"zh\":\"\"},\"href\":\"https://...\"}],
  \"highlights\": [{\"kind\":\"new|improved|fixed|security\",\"title\":{\"en\":\"\",\"zh\":\"\"},\"body\":{\"en\":\"\",\"zh\":\"\"},\"refs\":[123]}],
  \"changes\": {\"new\":[],\"improved\":[],\"fixed\":[]},
  \"upgrade\": [{\"level\":\"info|warning\",\"title\":{\"en\":\"\",\"zh\":\"\"},\"body\":{\"en\":\"\",\"zh\":\"\"},\"refs\":[123]}],
  \"risks\": [{\"title\":{\"en\":\"\",\"zh\":\"\"},\"body\":{\"en\":\"\",\"zh\":\"\"},\"refs\":[123]}],
  \"contributors\": [], \"links\": {\"github\":\"https://...\",\"compare\":\"https://...\",\"download\":\"https://...\"}
}
Return guides only for supplied documentation URLs. Mention upgrade action or risk only when explicitly supported; otherwise use empty arrays. Output JSON only.`,
        },
        { role: "user", content: `Create the release record from these public GitHub sources:\n${JSON.stringify(payload)}` },
      ],
    }),
  });
  if (!response.ok) throw new Error(`DeepSeek API failed: ${response.status} ${await response.text()}`);
  const data = await response.json();
  try {
    return extractJson(data.choices?.[0]?.message?.content);
  } catch (error) {
    if (!retry) throw error;
    return askDeepSeek(payload, false);
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (!args.version) throw new Error("--version is required");
  const version = normalizeVersion(args.version);
  const catalog = await loadCatalog();
  const previous = args.from || catalog.releases.find((release) => release.version !== version)?.version;
  if (!previous) throw new Error("--from is required when no previous release exists");
  const from = previous.match(/^(?:desktop-|npm-)?v/) ? previous : `desktop-v${previous}`;
  const to = args.to || "HEAD";
  const repository = repositoryName();
  const commits = commitRange(from, to);
  if (!commits.length) throw new Error(`no commits found in ${from}..${to}`);
  const pulls = await collectPullRequests(repository, commits);
  if (!pulls.length) throw new Error(`no pull requests found in ${from}..${to}`);
  const docLinks = collectDocLinks(from, repository, to);
  const date = args.date || new Date().toISOString().slice(0, 10);
  const tag = args.tag || `desktop-v${version}`;
  const source = {
    version,
    date,
    channel: version.includes("-") ? "prerelease" : "stable",
    range: `${from}..${to}`,
    pullRequests: pulls,
    documentationUrls: docLinks,
  };
  const release = await askDeepSeek(source);
  release.version = version;
  release.date = date;
  release.channel = source.channel;
  release.contributors = [...new Set(pulls.map((pull) => pull.author).filter(Boolean))];
  release.links = {
    github: `https://github.com/${repository}/releases/tag/${tag}`,
    compare: `https://github.com/${repository}/compare/${from}...${tag}`,
    download: "https://reasonix.io/?download=desktop#start",
  };
  release.guides = (release.guides || []).filter((guide) => docLinks.includes(guide.href));
  assertGroundedRefs(release, new Set(pulls.map((pull) => pull.number)));
  validateCatalog({ schemaVersion: 1, releases: [release] });
  await upsertRelease(release);
  console.log(`Generated bilingual release notes for v${version} from ${pulls.length} pull request(s).`);
}

main().catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
