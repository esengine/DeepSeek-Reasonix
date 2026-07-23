#!/usr/bin/env node

import { readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
export const defaultCatalogPath = resolve(repoRoot, "release-notes/releases.json");

const localizedFields = ["title", "body"];
const changeKinds = ["new", "improved", "fixed"];
const itemKinds = new Set(["new", "improved", "fixed", "security"]);

function invariant(condition, message) {
  if (!condition) throw new Error(message);
}

function isObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function validateLocalized(value, path) {
  invariant(isObject(value), `${path} must be an object`);
  for (const lang of ["en", "zh"]) {
    invariant(typeof value[lang] === "string" && value[lang].trim(), `${path}.${lang} must be a non-empty string`);
  }
}

function validateRefs(refs, path) {
  if (refs === undefined) return;
  invariant(Array.isArray(refs), `${path} must be an array`);
  for (const ref of refs) invariant(Number.isInteger(ref) && ref > 0, `${path} contains invalid PR number ${ref}`);
}

function validateItem(item, path, { kind = false, href = false, level = false } = {}) {
  invariant(isObject(item), `${path} must be an object`);
  for (const field of localizedFields) validateLocalized(item[field], `${path}.${field}`);
  if (kind) invariant(itemKinds.has(item.kind), `${path}.kind is invalid`);
  if (href) invariant(typeof item.href === "string" && /^https:\/\//.test(item.href), `${path}.href must be HTTPS`);
  if (level) invariant(item.level === "info" || item.level === "warning", `${path}.level is invalid`);
  validateRefs(item.refs, `${path}.refs`);
}

function semverParts(version) {
  const match = String(version).match(/^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$/);
  invariant(match, `invalid version ${version}`);
  return [Number(match[1]), Number(match[2]), Number(match[3]), match[4] || ""];
}

function compareVersionsDesc(a, b) {
  const aa = semverParts(a);
  const bb = semverParts(b);
  for (let i = 0; i < 3; i += 1) {
    if (aa[i] !== bb[i]) return bb[i] - aa[i];
  }
  if (aa[3] === bb[3]) return 0;
  if (!aa[3]) return -1;
  if (!bb[3]) return 1;
  return String(bb[3]).localeCompare(String(aa[3]), "en", { numeric: true });
}

export function validateCatalog(catalog) {
  invariant(isObject(catalog), "catalog must be an object");
  invariant(catalog.schemaVersion === 1, "catalog.schemaVersion must be 1");
  invariant(Array.isArray(catalog.releases) && catalog.releases.length > 0, "catalog.releases must not be empty");

  const versions = new Set();
  for (const [index, release] of catalog.releases.entries()) {
    const path = `releases[${index}]`;
    invariant(isObject(release), `${path} must be an object`);
    semverParts(release.version);
    invariant(!versions.has(release.version), `duplicate version ${release.version}`);
    versions.add(release.version);
    invariant(/^\d{4}-\d{2}-\d{2}$/.test(release.date), `${path}.date must use YYYY-MM-DD`);
    invariant(release.channel === "stable" || release.channel === "prerelease", `${path}.channel is invalid`);
    validateLocalized(release.title, `${path}.title`);
    validateLocalized(release.summary, `${path}.summary`);
    invariant(Array.isArray(release.surfaces) && release.surfaces.length > 0, `${path}.surfaces must not be empty`);
    invariant(new Set(release.surfaces).size === release.surfaces.length, `${path}.surfaces contains duplicates`);
    invariant(Array.isArray(release.guides), `${path}.guides must be an array`);
    release.guides.forEach((item, itemIndex) => validateItem(item, `${path}.guides[${itemIndex}]`, { href: true }));
    invariant(Array.isArray(release.highlights) && release.highlights.length > 0, `${path}.highlights must not be empty`);
    release.highlights.forEach((item, itemIndex) => validateItem(item, `${path}.highlights[${itemIndex}]`, { kind: true }));
    invariant(isObject(release.changes), `${path}.changes must be an object`);
    for (const changeKind of changeKinds) {
      invariant(Array.isArray(release.changes[changeKind]), `${path}.changes.${changeKind} must be an array`);
      release.changes[changeKind].forEach((item, itemIndex) =>
        validateItem(item, `${path}.changes.${changeKind}[${itemIndex}]`),
      );
    }
    invariant(Array.isArray(release.upgrade), `${path}.upgrade must be an array`);
    release.upgrade.forEach((item, itemIndex) => validateItem(item, `${path}.upgrade[${itemIndex}]`, { level: true }));
    invariant(Array.isArray(release.risks), `${path}.risks must be an array`);
    release.risks.forEach((item, itemIndex) => validateItem(item, `${path}.risks[${itemIndex}]`));
    invariant(Array.isArray(release.contributors), `${path}.contributors must be an array`);
    invariant(isObject(release.links), `${path}.links must be an object`);
    for (const link of ["github", "compare", "download"]) {
      invariant(typeof release.links[link] === "string" && /^https:\/\//.test(release.links[link]), `${path}.links.${link} must be HTTPS`);
    }
  }

  const sorted = [...catalog.releases].sort((a, b) => compareVersionsDesc(a.version, b.version));
  invariant(
    sorted.every((release, index) => release.version === catalog.releases[index].version),
    "catalog.releases must be sorted newest first",
  );
  return catalog;
}

export async function loadCatalog(path = defaultCatalogPath) {
  const catalog = JSON.parse(await readFile(path, "utf8"));
  return validateCatalog(catalog);
}

export function releaseForVersion(catalog, version) {
  const normalized = String(version).replace(/^(?:desktop-|npm-)?v/, "");
  const release = catalog.releases.find((entry) => entry.version === normalized);
  invariant(release, `release notes for v${normalized} are missing`);
  return release;
}

function localized(value, lang) {
  return value[lang] || value.en;
}

function refsSuffix(refs = []) {
  if (!refs.length) return "";
  return ` (${refs.map((ref) => `[#${ref}](https://github.com/esengine/DeepSeek-Reasonix/pull/${ref})`).join(", ")})`;
}

function renderItems(items, lang) {
  return items
    .map((item) => `- **${localized(item.title, lang)}** — ${localized(item.body, lang)}${refsSuffix(item.refs)}`)
    .join("\n");
}

export function renderGitHubRelease(release, lang = "zh") {
  const isZh = lang === "zh";
  const lines = [
    `> ${localized(release.summary, lang)}`,
    "",
    isZh
      ? `[English →](https://reasonix.io/changelog/v${release.version}/?lang=en) · [网页版完整更新日志 →](https://reasonix.io/changelog/v${release.version}/)`
      : `[中文 →](https://reasonix.io/changelog/v${release.version}/?lang=zh) · [Full release notes →](https://reasonix.io/changelog/v${release.version}/?lang=en)`,
    "",
  ];

  if (release.guides.length) {
    lines.push(`## ${isZh ? "使用攻略" : "Guides"}`, "");
    for (const guide of release.guides) {
      lines.push(`- [**${localized(guide.title, lang)}**](${guide.href}) — ${localized(guide.body, lang)}`);
    }
    lines.push("");
  }

  lines.push(
    `## ${isZh ? "概览" : "Overview"}`,
    "",
    `**Reasonix v${release.version} — ${localized(release.title, lang)}**`,
    "",
    localized(release.summary, lang),
    "",
    `${isZh ? "发布日期" : "Released"}：${release.date}`,
    "",
    `## ${isZh ? "重点内容" : "Highlights"}`,
    "",
    renderItems(release.highlights, lang),
    "",
  );

  const headings = {
    new: isZh ? "新功能" : "New",
    improved: isZh ? "改进" : "Improved",
    fixed: isZh ? "修复" : "Fixed",
  };
  for (const kind of changeKinds) {
    const items = release.changes[kind];
    if (!items.length) continue;
    lines.push(`## ${headings[kind]}`, "", renderItems(items, lang), "");
  }

  lines.push(`## ${isZh ? "升级提醒" : "Upgrade notes"}`, "");
  if (release.upgrade.length) lines.push(renderItems(release.upgrade, lang));
  else lines.push(isZh ? "本版本无需手动迁移。" : "No manual migration is required.");
  lines.push("");

  lines.push(`## ${isZh ? "风险提示" : "Risk notes"}`, "");
  if (release.risks.length) lines.push(renderItems(release.risks, lang));
  else lines.push(isZh ? "当前没有需要额外操作的已知风险。" : "There are no known risks requiring extra action.");
  lines.push("");

  if (release.contributors.length) {
    lines.push(
      `## ${isZh ? "致谢" : "Thanks"}`,
      "",
      `${isZh ? "感谢本版本的贡献者" : "Thanks to the contributors in this release"}：${release.contributors
        .map((name) => `[@${name}](https://github.com/${name})`)
        .join("、")}`,
      "",
    );
  }

  lines.push(
    `## ${isZh ? "下载与安装" : "Download and install"}`,
    "",
    `- [${isZh ? "官网按平台下载" : "Platform downloads"}](${release.links.download})`,
    `- [${isZh ? "查看完整差异" : "Full comparison"}](${release.links.compare})`,
    "",
  );
  return `${lines.join("\n").trim()}\n`;
}

export async function upsertRelease(release, path = defaultCatalogPath) {
  const catalog = await loadCatalog(path);
  const next = {
    ...catalog,
    releases: [release, ...catalog.releases.filter((entry) => entry.version !== release.version)].sort((a, b) =>
      compareVersionsDesc(a.version, b.version),
    ),
  };
  validateCatalog(next);
  await writeFile(path, `${JSON.stringify(next, null, 2)}\n`);
  return next;
}

function parseArgs(argv) {
  const [command = "validate", ...rest] = argv;
  const values = { command };
  for (let i = 0; i < rest.length; i += 1) {
    const arg = rest[i];
    if (!arg.startsWith("--")) throw new Error(`unexpected argument ${arg}`);
    values[arg.slice(2)] = rest[++i];
  }
  return values;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const catalogPath = args.catalog ? resolve(args.catalog) : defaultCatalogPath;
  const catalog = await loadCatalog(catalogPath);
  if (args.command === "validate") {
    console.log(`Validated ${catalog.releases.length} release note(s).`);
    return;
  }
  if (args.command !== "render") throw new Error(`unknown command ${args.command}`);
  invariant(args.version, "render requires --version");
  invariant(args.output, "render requires --output");
  const release = releaseForVersion(catalog, args.version);
  const output = resolve(args.output);
  await writeFile(output, renderGitHubRelease(release, args.lang === "en" ? "en" : "zh"));
  console.log(`Rendered v${release.version} release notes to ${output}`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
