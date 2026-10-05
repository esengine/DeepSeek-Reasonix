#!/usr/bin/env node
// Renders release-notes/studio/<semver>.md for publication: each #N gains its
// pull request author, or for an issue the pull requests that closed it, and a
// 贡献者 list closes the notes. The source file is never rewritten.

import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { annotateMarkdown, markdownRefs } from "./markdown-refs.mjs";
import {
  contributorLogins,
  creditSuffix,
  githubRefLookup,
  reportWarnings,
  resolveCredits,
  tokenFromEnvironment,
  unresolvedError,
} from "./release-credits.mjs";

export function renderStudioNotes(markdown, credits) {
  const body = annotateMarkdown(markdown, (ref) => creditSuffix(credits.get(ref))).trimEnd();
  const contributors = contributorLogins(markdownRefs(markdown), credits);
  if (!contributors.length) return `${body}\n`;
  return `${body}\n\n## 贡献者\n\n感谢本版本的贡献者：${contributors.map((login) => `@${login}`).join("、")}\n`;
}

async function main() {
  const [input, output] = process.argv.slice(2);
  if (!input || !output) throw new Error("usage: studio-release-notes.mjs <notes.md> <output.md>");
  const markdown = await readFile(input, "utf8");
  const refs = markdownRefs(markdown);
  let credits = new Map();
  let failures = [];
  if (refs.length) {
    const lookup = githubRefLookup({ token: tokenFromEnvironment() });
    let warnings;
    ({ credits, failures, warnings } = await resolveCredits(refs, lookup));
    reportWarnings(warnings);
  }
  await writeFile(output, renderStudioNotes(markdown, credits));
  if (failures.length) throw unresolvedError(failures);
  console.log(`Rendered ${input} to ${output}: ${new Set(refs).size} reference(s), ${credits.size} resolved`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(error.code ? `${error.code}: ${error.message}` : error.message);
    process.exitCode = 1;
  });
}
