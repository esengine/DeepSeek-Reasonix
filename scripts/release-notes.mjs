#!/usr/bin/env node

import { readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
export const defaultCatalogPath = resolve(repoRoot, "release-notes/releases.json");

const localizedFields = ["title", "body"];
const changeKinds = ["new", "improved", "fixed"];
const itemKinds = new Set(["new", "improved", "fixed", "security"]);
const releaseChannels = new Set(["stable", "prerelease"]);
const releaseStatuses = new Set(["reviewed", "published"]);
const releaseTargetOrder = ["desktop", "cli", "site", "service"];
const releaseTargets = new Set(releaseTargetOrder);
const releaseTargetingRequiredFrom = "1.31.4";

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

function validateItem(item, path, { kind = false, href = false, level = false, targets = false } = {}) {
  invariant(isObject(item), `${path} must be an object`);
  for (const field of localizedFields) validateLocalized(item[field], `${path}.${field}`);
  if (kind) invariant(itemKinds.has(item.kind), `${path}.kind is invalid`);
  if (href) invariant(typeof item.href === "string" && /^https:\/\//.test(item.href), `${path}.href must be HTTPS`);
  if (level) invariant(item.level === "info" || item.level === "warning", `${path}.level is invalid`);
  if (targets || item.targets !== undefined) {
    invariant(Array.isArray(item.targets) && item.targets.length > 0, `${path}.targets must be a non-empty array`);
    invariant(new Set(item.targets).size === item.targets.length, `${path}.targets contains duplicates`);
    for (const target of item.targets) invariant(releaseTargets.has(target), `${path}.targets contains invalid target ${target}`);
    const sorted = releaseTargetOrder.filter((target) => item.targets.includes(target));
    invariant(sorted.every((target, index) => target === item.targets[index]), `${path}.targets must use canonical order`);
  }
  validateRefs(item.refs, `${path}.refs`);
}

function releaseItems(release) {
  return [
    ...release.highlights,
    ...changeKinds.flatMap((kind) => release.changes[kind]),
    ...release.upgrade,
    ...release.risks,
  ];
}

function itemTargetsUnion(release) {
  const targets = new Set(releaseItems(release).flatMap((item) => item.targets || []));
  return releaseTargetOrder.filter((target) => targets.has(target));
}

function semverParts(version) {
  const match = String(version).match(/^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$/);
  invariant(match, `invalid version ${version}`);
  return [Number(match[1]), Number(match[2]), Number(match[3]), match[4] || ""];
}

function isCoreVersionAtLeast(version, minimum) {
  const current = semverParts(version);
  const floor = semverParts(minimum);
  for (let index = 0; index < 3; index += 1) {
    if (current[index] !== floor[index]) return current[index] > floor[index];
  }
  return true;
}

export function compareVersionsDesc(a, b) {
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
    invariant(releaseChannels.has(release.channel), `${path}.channel is invalid`);
    if (release.releaseId !== undefined) {
      invariant(release.releaseId === release.version, `${path}.releaseId must equal version`);
    }
    if (release.baseVersion !== undefined) {
      semverParts(release.baseVersion);
      invariant(!release.baseVersion.includes("-"), `${path}.baseVersion must be stable semver`);
      invariant(
        release.version === release.baseVersion || release.version.startsWith(`${release.baseVersion}-`),
        `${path}.baseVersion does not match version`,
      );
    }
    if (release.status !== undefined) {
      invariant(releaseStatuses.has(release.status), `${path}.status is invalid`);
    }
    if (release.candidateSha !== undefined) {
      invariant(/^[0-9a-f]{40}$/.test(release.candidateSha), `${path}.candidateSha must be a full commit SHA`);
    }
    if (release.previousRelease !== undefined) {
      semverParts(release.previousRelease);
      invariant(release.previousRelease !== release.version, `${path}.previousRelease must differ from version`);
    }
    if (release.builds !== undefined) {
      invariant(isObject(release.builds), `${path}.builds must be an object`);
      for (const surface of ["cli", "desktop", "npm"]) {
        invariant(
          typeof release.builds[surface] === "string" && release.builds[surface].trim(),
          `${path}.builds.${surface} must be a non-empty string`,
        );
      }
    }
    if (release.status !== undefined) {
      for (const field of ["releaseId", "baseVersion", "previousRelease", "builds"]) {
        invariant(release[field] !== undefined, `${path}.${field} is required for managed release records`);
      }
      if (release.channel === "stable") {
        invariant(release.version === release.baseVersion, `${path}.stable version must equal baseVersion`);
        invariant(release.builds.cli === `v${release.version}`, `${path}.builds.cli does not match Stable version`);
        invariant(release.builds.desktop === `v${release.version}`, `${path}.builds.desktop does not match Stable version`);
        invariant(release.builds.npm === release.version, `${path}.builds.npm does not match Stable version`);
      } else {
        const preview = release.version.match(/^(\d+\.\d+\.\d+)-preview\.([1-9][0-9]*)$/);
        invariant(preview, `${path}.managed prerelease must use MAJOR.MINOR.PATCH-preview.N`);
        invariant(preview[1] === release.baseVersion, `${path}.Preview baseVersion does not match version`);
        invariant(release.builds.cli === `v${release.version}`, `${path}.builds.cli does not match Preview version`);
        invariant(release.builds.desktop === `v${release.version}`, `${path}.builds.desktop does not match Preview version`);
        invariant(
          release.builds.npm === `${release.baseVersion}-canary.${preview[2]}`,
          `${path}.builds.npm does not match Preview ordinal`,
        );
      }
    }
    const targetingRequiredByVersion = isCoreVersionAtLeast(release.version, releaseTargetingRequiredFrom);
    if (targetingRequiredByVersion) {
      invariant(
        release.targetingVersion === 1,
        `${path}.targetingVersion must be 1 for v${releaseTargetingRequiredFrom} and newer`,
      );
    } else if (release.targetingVersion !== undefined) {
      invariant(release.targetingVersion === 1, `${path}.targetingVersion is invalid`);
    }
    const targetsRequired = release.targetingVersion === 1;
    validateLocalized(release.title, `${path}.title`);
    validateLocalized(release.summary, `${path}.summary`);
    invariant(Array.isArray(release.surfaces) && release.surfaces.length > 0, `${path}.surfaces must not be empty`);
    invariant(new Set(release.surfaces).size === release.surfaces.length, `${path}.surfaces contains duplicates`);
    invariant(Array.isArray(release.guides), `${path}.guides must be an array`);
    release.guides.forEach((item, itemIndex) => validateItem(item, `${path}.guides[${itemIndex}]`, { href: true }));
    invariant(Array.isArray(release.highlights) && release.highlights.length > 0, `${path}.highlights must not be empty`);
    release.highlights.forEach((item, itemIndex) =>
      validateItem(item, `${path}.highlights[${itemIndex}]`, { kind: true, targets: targetsRequired }),
    );
    invariant(isObject(release.changes), `${path}.changes must be an object`);
    for (const changeKind of changeKinds) {
      invariant(Array.isArray(release.changes[changeKind]), `${path}.changes.${changeKind} must be an array`);
      release.changes[changeKind].forEach((item, itemIndex) =>
        validateItem(item, `${path}.changes.${changeKind}[${itemIndex}]`, { targets: targetsRequired }),
      );
    }
    invariant(Array.isArray(release.upgrade), `${path}.upgrade must be an array`);
    release.upgrade.forEach((item, itemIndex) =>
      validateItem(item, `${path}.upgrade[${itemIndex}]`, { level: true, targets: targetsRequired }),
    );
    invariant(Array.isArray(release.risks), `${path}.risks must be an array`);
    release.risks.forEach((item, itemIndex) =>
      validateItem(item, `${path}.risks[${itemIndex}]`, { targets: targetsRequired }),
    );
    if (targetsRequired) {
      const derivedTargets = itemTargetsUnion(release);
      invariant(
        derivedTargets.includes("desktop") || derivedTargets.includes("cli"),
        `${path} must contain at least one Desktop or CLI update`,
      );
      invariant(
        derivedTargets.length === release.surfaces.length &&
          derivedTargets.every((target, index) => target === release.surfaces[index]),
        `${path}.surfaces must equal the canonical union of item targets`,
      );
    }
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

function targetLabel(target) {
  return { desktop: "Desktop", cli: "CLI", site: "Site", service: "Service" }[target] || target;
}

function targetsPrefix(item) {
  if (!item.targets?.length) return "";
  return `**[${item.targets.map(targetLabel).join(" · ")}]** `;
}

function renderItems(items, lang) {
  return items
    .map((item) => `- ${targetsPrefix(item)}**${localized(item.title, lang)}** — ${localized(item.body, lang)}${refsSuffix(item.refs)}`)
    .join("\n");
}

function hasTarget(item, target) {
  return item.targets?.includes(target) || false;
}

function targetItemCount(release, target) {
  return releaseItems(release).filter((item) => hasTarget(item, target)).length;
}

function appendTargetSection(lines, release, target, lang) {
  const isZh = lang === "zh";
  const title = target === "desktop"
    ? (isZh ? "桌面端更新" : "Desktop updates")
    : (isZh ? "CLI 端更新" : "CLI updates");
  lines.push(`## ${title}`, "");

  const highlights = release.highlights.filter((item) => hasTarget(item, target));
  if (highlights.length) {
    lines.push(`### ${isZh ? "重点内容" : "Highlights"}`, "", renderItems(highlights, lang), "");
  }
  const headings = {
    new: isZh ? "新增" : "New",
    improved: isZh ? "改进" : "Improved",
    fixed: isZh ? "修复" : "Fixed",
  };
  for (const kind of changeKinds) {
    const items = release.changes[kind].filter((item) => hasTarget(item, target));
    if (items.length) lines.push(`### ${headings[kind]}`, "", renderItems(items, lang), "");
  }

  if (!highlights.length && !changeKinds.some((kind) => release.changes[kind].some((item) => hasTarget(item, target)))) {
    const noTargetedItems = targetItemCount(release, target) === 0;
    lines.push(
      target === "cli" && noTargetedItems
        ? (isZh ? "本版本没有 CLI 相关功能、改进或修复，CLI 用户可按需跳过。" : "This release has no CLI features, improvements, or fixes; CLI users may skip it if preferred.")
        : (isZh ? "此板块没有功能或修复条目；相关说明请查看下方升级提醒和风险提示。" : "No feature or fix entries are listed here; see the upgrade and risk notes below."),
      "",
    );
  }
}

function appendOtherTargetSection(lines, release, lang) {
  const isZh = lang === "zh";
  const isOtherOnly = (item) => item.targets?.length && !hasTarget(item, "desktop") && !hasTarget(item, "cli");
  const highlights = release.highlights.filter(isOtherOnly);
  const changes = Object.fromEntries(changeKinds.map((kind) => [kind, release.changes[kind].filter(isOtherOnly)]));
  if (!highlights.length && !changeKinds.some((kind) => changes[kind].length)) return;

  lines.push(`## ${isZh ? "其他项目更新" : "Other project updates"}`, "");
  if (highlights.length) lines.push(renderItems(highlights, lang), "");
  for (const kind of changeKinds) {
    if (changes[kind].length) lines.push(renderItems(changes[kind], lang), "");
  }
}

export function renderGitHubRelease(release, lang = "zh") {
  const isZh = lang === "zh";
  const isPreview = release.channel === "prerelease";
  const channelLabel = isPreview ? (isZh ? "预览版" : "Preview") : (isZh ? "稳定版" : "Stable");
  const lines = [
    `> ${localized(release.summary, lang)}`,
    "",
    `**${isZh ? "发布渠道" : "Release channel"}：${channelLabel} · v${release.version}**`,
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
  );

  if (release.targetingVersion === 1) {
    lines.push(
      `**Desktop ${targetItemCount(release, "desktop")} · CLI ${targetItemCount(release, "cli")}**`,
      "",
    );
    appendTargetSection(lines, release, "desktop", lang);
    appendTargetSection(lines, release, "cli", lang);
    appendOtherTargetSection(lines, release, lang);
  } else {
    lines.push(`## ${isZh ? "重点内容" : "Highlights"}`, "", renderItems(release.highlights, lang), "");
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
