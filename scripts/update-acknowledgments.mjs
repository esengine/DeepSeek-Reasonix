#!/usr/bin/env node
import { readFile, writeFile } from 'node:fs/promises';

const repo = 'esengine/DeepSeek-Reasonix';
const api = `https://api.github.com/repos/${repo}/contributors?per_page=20&anon=1`;
const startMarker = '<!-- reasonix-top-contributors:start -->';
const endMarker = '<!-- reasonix-top-contributors:end -->';

const headers = {
  Accept: 'application/vnd.github+json',
  'User-Agent': 'reasonix-acknowledgments-updater',
};
if (process.env.GITHUB_TOKEN) {
  headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;
}

const res = await fetch(api, { headers });
if (!res.ok) {
  throw new Error(`GitHub contributors API failed: ${res.status} ${res.statusText}`);
}

const contributors = await res.json();
if (!Array.isArray(contributors) || contributors.length === 0) {
  throw new Error('GitHub contributors API returned no contributors');
}

const top = contributors.slice(0, 20).map((c, index) => ({
  rank: index + 1,
  login: typeof c.login === 'string' ? c.login : '',
  name: typeof c.name === 'string' ? c.name : '',
  url: typeof c.html_url === 'string' ? c.html_url : '',
  type: typeof c.type === 'string' ? c.type : '',
  commits: Number(c.contributions) || 0,
}));

await updateReadme('README.md', renderTable(top, 'en'));
await updateReadme('README.zh-CN.md', renderTable(top, 'zh'));

function renderTable(rows, locale) {
  const header = '| Contributor | Contributor | Contributor | Contributor |\n| --- | --- | --- | --- |';
  const cells = rows.map((row) => renderContributor(row, locale));
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

function renderContributor(row, locale) {
  const label = row.login || row.name || `anonymous-${row.rank}`;
  const escaped = escapeMarkdown(label);
  if (row.url) {
    return `[**${escaped}**](${row.url})`;
  }
  const anonymous = locale === 'zh' ? '（anonymous）' : ' (anonymous)';
  return `**${escaped}**${anonymous}`;
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
