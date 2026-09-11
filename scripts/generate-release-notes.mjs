#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { dirname } from "node:path";
import { loadCatalog, upsertRelease, validateCatalog } from "./release-notes.mjs";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const apiBase = process.env.DEEPSEEK_API_BASE || "https://api.deepseek.com";
const model = process.env.DEEPSEEK_MODEL || "deepseek-v4-pro";
const releaseTargetOrder = ["desktop", "cli", "site", "service"];

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

async function githubList(path) {
  const items = [];
  for (let page = 1; ; page += 1) {
    const separator = path.includes("?") ? "&" : "?";
    const batch = await githubJson(`${path}${separator}per_page=100&page=${page}`);
    items.push(...batch);
    if (batch.length < 100) return items;
  }
}

export function inferPullTargetHints(labels, files) {
  const labelSet = new Set(labels);
  const targets = new Set();
  const add = (target) => targets.add(target);
  const hasPath = (pattern) => files.some((path) => pattern.test(path));

  const explicitlyDesktop = labelSet.has("desktop");
  const explicitlyCLI = labelSet.has("tui") || labelSet.has("cli");
  if (explicitlyDesktop) add("desktop");
  if (explicitlyCLI) add("cli");
  if (hasPath(/^desktop\//)) add("desktop");
  if (hasPath(/^(?:internal\/cli\/|cmd\/)/)) add("cli");
  if (hasPath(/^site\//)) add("site");
  if (hasPath(/^(?:workers\/|\.github\/|scripts\/|release-notes\/)/)) add("service");

  const hasSharedProductCode = files.some((path) =>
    /^(?:internal\/|sdk\/|npm\/)/.test(path) &&
    !/^internal\/cli\//.test(path) &&
    !/^internal\/telemetry\//.test(path),
  );
  if (!explicitlyDesktop && !explicitlyCLI && hasSharedProductCode) {
    add("desktop");
    add("cli");
  }
  if (!targets.size) {
    add("desktop");
    add("cli");
  }
  return releaseTargetOrder.filter((target) => targets.has(target));
}

async function collectPullRequests(repository, commits) {
  const numbers = new Set(prNumbersFromCommits(commits));
  const associated = await Promise.all(
    commits.map((commit) => githubJson(`/repos/${repository}/commits/${commit.sha}/pulls`, { allowMissing: true })),
  );
  for (const pulls of associated) for (const pull of pulls || []) numbers.add(pull.number);
  const pulls = await Promise.all([...numbers].map((number) => githubJson(`/repos/${repository}/pulls/${number}`, { allowMissing: true })));
  return Promise.all(pulls.filter(Boolean).map(async (pull) => {
    const labels = (pull.labels || []).map((label) => label.name);
    const files = (await githubList(`/repos/${repository}/pulls/${pull.number}/files`)).map((file) => file.filename);
    return {
      number: pull.number,
      title: pull.title,
      body: String(pull.body || "").slice(0, 2000),
      author: pull.user?.login || "",
      labels,
      changedFileCount: files.length,
      files: files.slice(0, 200),
      targetHints: inferPullTargetHints(labels, files),
    };
  }));
}

function releaseItems(release) {
  return [
    ...(release.highlights || []),
    ...["new", "improved", "fixed"].flatMap((kind) => release.changes?.[kind] || []),
    ...(release.upgrade || []),
    ...(release.risks || []),
  ];
}

function normalizeReleaseTargets(release) {
  for (const item of releaseItems(release)) {
    if (!Array.isArray(item.targets)) continue;
    item.targets.sort((a, b) => releaseTargetOrder.indexOf(a) - releaseTargetOrder.indexOf(b));
  }
  const targets = new Set(releaseItems(release).flatMap((item) => item.targets || []));
  release.surfaces = [
    ...releaseTargetOrder.filter((target) => targets.has(target)),
    ...[...targets].filter((target) => !releaseTargetOrder.includes(target)).sort(),
  ];
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
      // Release notes need deterministic structured output, not hidden chain of
      // thought. Thinking tokens share the generation budget with content and
      // can otherwise leave response_format JSON empty or truncated.
      thinking: { type: "disabled" },
      temperature: 0,
      max_tokens: 8000,
      response_format: { type: "json_object" },
      messages: [
        {
          role: "system",
          content: `You are Reasonix's release editor. Return one JSON object with a \"release\" property. Write factual, user-facing product release notes in equivalent English and Simplified Chinese. Group changes by user outcome, not by commit. Never invent capabilities, migrations, risks, PR numbers, contributors, URLs, or metrics. Every highlight and change must cite one or more supplied PR numbers. Use this exact release shape:
{
  \"version\": \"semver\", \"date\": \"YYYY-MM-DD\", \"channel\": \"stable|prerelease\", \"targetingVersion\": 1,
  \"title\": {\"en\":\"\",\"zh\":\"\"}, \"summary\": {\"en\":\"\",\"zh\":\"\"},
  \"surfaces\": [\"desktop\",\"cli\"],
  \"guides\": [{\"title\":{\"en\":\"\",\"zh\":\"\"},\"body\":{\"en\":\"\",\"zh\":\"\"},\"href\":\"https://...\"}],
  \"highlights\": [{\"kind\":\"new|improved|fixed|security\",\"targets\":[\"desktop\",\"cli\"],\"title\":{\"en\":\"\",\"zh\":\"\"},\"body\":{\"en\":\"\",\"zh\":\"\"},\"refs\":[123]}],
  \"changes\": {\"new\":[],\"improved\":[],\"fixed\":[]},
  \"upgrade\": [{\"level\":\"info|warning\",\"targets\":[\"desktop\"],\"title\":{\"en\":\"\",\"zh\":\"\"},\"body\":{\"en\":\"\",\"zh\":\"\"},\"refs\":[123]}],
  \"risks\": [{\"targets\":[\"cli\"],\"title\":{\"en\":\"\",\"zh\":\"\"},\"body\":{\"en\":\"\",\"zh\":\"\"},\"refs\":[123]}],
  \"contributors\": [], \"links\": {\"github\":\"https://...\",\"compare\":\"https://...\",\"download\":\"https://...\"}
}
Every highlight, change, upgrade note, and risk must have a non-empty \"targets\" array using only this canonical order: desktop, cli, site, service. Use each PR's targetHints as deterministic candidates, then choose the user-visible delivery target supported by its labels, changed files, title, and body. Shared product-core behavior belongs to both desktop and cli. Website-only work belongs to site; hosted workers and release infrastructure belong to service and must not be marked as a client update merely because a client file accompanies the server change. Set top-level surfaces to the canonical union of all item targets. Return guides only for supplied documentation URLs. Mention upgrade action or risk only when explicitly supported; otherwise use empty arrays. Output JSON only.`,
        },
        { role: "user", content: `Create the release record from these public GitHub sources:\n${JSON.stringify(payload)}` },
      ],
    }),
  });
  if (!response.ok) throw new Error(`DeepSeek API failed: ${response.status} ${await response.text()}`);
  const data = await response.json();
  const choice = data.choices?.[0];
  try {
    return extractJson(choice?.message?.content);
  } catch (error) {
    if (!retry) {
      throw new Error(`${error.message} (finish_reason=${choice?.finish_reason || "unknown"})`);
    }
    return askDeepSeek(payload, false);
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (!args.version) throw new Error("--version is required");
  const version = normalizeVersion(args.version);
  const catalog = await loadCatalog();
  const channel = version.includes("-") ? "prerelease" : "stable";
  const baseVersion = version.split("-")[0];
  const previousRecord = channel === "stable"
    ? catalog.releases.find((release) => release.version !== version && release.channel === "stable")
    : catalog.releases.find(
        (release) =>
          release.version !== version &&
          release.channel === "prerelease" &&
          release.baseVersion === baseVersion,
      ) || catalog.releases.find((release) => release.channel === "stable");
  const previous = args.from || previousRecord?.version;
  if (!previous) throw new Error("--from is required when no previous release exists");
  const previousVersion = normalizeVersion(previous);
  const previousIsPreview = previousVersion.includes("-");
  const from = previous.match(/^(?:desktop-|npm-)?v/)
    ? previous
    : previousIsPreview
      ? `v${previousVersion}`
      : `desktop-v${previousVersion}`;
  const to = args.to || "HEAD";
  const repository = repositoryName();
  const commits = commitRange(from, to);
  if (!commits.length) throw new Error(`no commits found in ${from}..${to}`);
  const pulls = await collectPullRequests(repository, commits);
  if (!pulls.length) throw new Error(`no pull requests found in ${from}..${to}`);
  const docLinks = collectDocLinks(from, repository, to);
  const date = args.date || new Date().toISOString().slice(0, 10);
  const tag = args.tag || (channel === "prerelease" ? `v${version}` : `desktop-v${version}`);
  const source = {
    version,
    date,
    channel,
    range: `${from}..${to}`,
    pullRequests: pulls,
    documentationUrls: docLinks,
  };
  const release = await askDeepSeek(source);
  release.targetingVersion = 1;
  release.version = version;
  release.releaseId = version;
  release.baseVersion = baseVersion;
  release.date = date;
  release.channel = source.channel;
  release.status = "reviewed";
  release.previousRelease = previousVersion;
  const previewOrdinal = version.match(/-preview\.([1-9][0-9]*)$/)?.[1];
  if (channel === "prerelease") {
    if (!previewOrdinal) throw new Error("Preview release version must use MAJOR.MINOR.PATCH-preview.N");
    release.builds = {
      cli: `v${version}`,
      desktop: `v${baseVersion}-preview.${previewOrdinal}`,
      npm: `${baseVersion}-canary.${previewOrdinal}`,
    };
  } else {
    release.builds = {
      cli: `v${version}`,
      desktop: `v${version}`,
      npm: version,
    };
  }
  release.contributors = [...new Set(pulls.map((pull) => pull.author).filter(Boolean))];
  release.links = {
    github: `https://github.com/${repository}/releases/tag/${tag}`,
    compare: `https://github.com/${repository}/compare/${from}...${tag}`,
    download: channel === "prerelease"
      ? "https://reasonix.io/?download=desktop&channel=preview#start"
      : "https://reasonix.io/?download=desktop&channel=stable#start",
  };
  release.guides = (release.guides || []).filter((guide) => docLinks.includes(guide.href));
  normalizeReleaseTargets(release);
  assertGroundedRefs(release, new Set(pulls.map((pull) => pull.number)));
  validateCatalog({ schemaVersion: 1, releases: [release] });
  await upsertRelease(release);
  console.log(`Generated bilingual release notes for v${version} from ${pulls.length} pull request(s).`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
