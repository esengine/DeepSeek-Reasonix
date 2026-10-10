#!/usr/bin/env node
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { isBotAccount, thanksExclusions } from './release-credits.mjs';

const repo = 'esengine/DeepSeek-Reasonix';
const api = `https://api.github.com/repos/${repo}/contributors?per_page=50&anon=1`;
const startMarker = '<!-- reasonix-top-contributors:start -->';
const endMarker = '<!-- reasonix-top-contributors:end -->';

const headers = {
  Accept: 'application/vnd.github+json',
  'User-Agent': 'reasonix-acknowledgments-updater',
};
if (process.env.GITHUB_TOKEN) {
  headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const res = await fetch(api, { headers });
  if (!res.ok) {
    throw new Error(`GitHub contributors API failed: ${res.status} ${res.statusText}`);
  }

  const contributors = await res.json();
  if (!Array.isArray(contributors) || contributors.length === 0) {
    throw new Error('GitHub contributors API returned no contributors');
  }

  const top = selectTop(contributors, thanksExclusions(repo));
  await updateReadme('README.md', renderTable(top));
  await updateReadme('README.zh-CN.md', renderTable(top));
}

export function selectTop(contributors, excluded, limit = 20) {
  return contributors
    .filter((c) => c && typeof c.login === 'string' && c.login !== '' && typeof c.html_url === 'string' && c.html_url !== '')
    .filter((c) => !isBotAccount(c.login, c.type) && !excluded.has(c.login.toLowerCase()))
    .map((c) => ({ login: c.login, url: c.html_url, commits: Number(c.contributions) || 0 }))
    .sort((a, b) => b.commits - a.commits || a.login.localeCompare(b.login))
    .slice(0, limit)
    .map((row, index) => ({ rank: index + 1, ...row }));
}

export function renderTable(rows) {
  const header = '| Contributor | Contributor | Contributor | Contributor |\n| --- | --- | --- | --- |';
  const cells = rows.map((row) => renderContributor(row));
  const tableRows = [];
  for (let i = 0; i < cells.length; i += 4) {
    tableRows.push(`| ${cells.slice(i, i + 4).join(' | ')} |`);
  }
  return [
    startMarker,
    header,
    ...tableRows,
    endMarker,
  ].join('\n');
}

function renderContributor(row) {
  return `[**${escapeMarkdown(row.login)}**](${row.url})`;
}

async function updateReadme(path, replacement) {
  const original = await readFile(path, 'utf8');
  const start = original.indexOf(startMarker);
  const end = original.indexOf(endMarker);
  if (start === -1 || end === -1 || end < start) {
    throw new Error(`${path} is missing ${startMarker}/${endMarker}`);
  }
  const next = original.slice(0, start) + replacement + original.slice(end + endMarker.length);
  if (next !== original) {
    await writeFile(path, next);
  }
}

function escapeMarkdown(value) {
  return String(value).replace(/[\\|[\]]/g, '\\$&');
}
