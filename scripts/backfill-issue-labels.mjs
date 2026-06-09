#!/usr/bin/env node
// Backfill area/platform/severity labels on existing open issues via the DeepSeek
// API — the one-off companion to .github/workflows/issue-auto-label.yml (which
// only fires on new issues). Keep the label sets and prompt in sync with that
// workflow. GitHub access uses the local `gh` CLI (must be authenticated).
//
// Usage:
//   DEEPSEEK_API_KEY=... node scripts/backfill-issue-labels.mjs [options]
// Options:
//   --dry-run          print what would change, apply nothing
//   --only-unlabeled   skip issues that already have an area label
//   --limit N          process at most N issues (default 200)

import { execFileSync } from 'node:child_process';

const args = process.argv.slice(2);
const dryRun = args.includes('--dry-run');
const onlyUnlabeled = args.includes('--only-unlabeled');
const li = args.indexOf('--limit');
const limit = li >= 0 ? parseInt(args[li + 1], 10) : 200;

const KEY = process.env.DEEPSEEK_API_KEY;
if (!KEY) {
  console.error('DEEPSEEK_API_KEY is not set');
  process.exit(1);
}

const AREA = ['agent', 'mcp', 'config', 'updater', 'provider', 'desktop', 'tui', 'skills', 'rendering'];
const PLATFORM = ['windows', 'macos', 'linux'];
const SEVERITY = ['crash', 'data-loss', 'security'];
const ALLOWED = new Set([...AREA, ...PLATFORM, ...SEVERITY]);

const SYSTEM = [
  'You categorize GitHub issues for Reasonix, a Go-based AI coding agent with a Wails desktop app and a terminal UI.',
  'Pick labels ONLY from these fixed sets. Never invent labels.',
  'area (0-2, the affected subsystem):',
  '  agent: core agent loop / tool-calling / reasoning',
  '  mcp: MCP servers, plugins, codegraph',
  '  config: configuration, setup wizard, .toml/.env',
  '  updater: auto-update, installer, release packaging',
  '  provider: model providers, model selection/switching',
  '  desktop: Wails desktop GUI',
  '  tui: terminal UI / CLI',
  '  skills: skills system',
  '  rendering: terminal rendering / flicker / repaint',
  'platform (only if clearly specific to one OS): windows, macos, linux',
  'severity (only if clearly applicable):',
  '  crash: app crashes, hangs, or freezes',
  '  data-loss: loss of sessions, config, or history',
  '  security: credential/secret exposure or a security flaw',
  'Be conservative: omit a label when unsure. The issue may be in Chinese.',
  'Reply with JSON only: {"area":[],"platform":[],"severity":[]}',
].join('\n');

function gh(args) {
  return execFileSync('gh', args, { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });
}

async function classify(title, body) {
  const res = await fetch('https://api.deepseek.com/chat/completions', {
    method: 'POST',
    headers: { Authorization: `Bearer ${KEY}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({
      model: 'deepseek-chat',
      temperature: 0,
      response_format: { type: 'json_object' },
      messages: [
        { role: 'system', content: SYSTEM },
        { role: 'user', content: `Title: ${title}\n\nBody:\n${body}` },
      ],
    }),
  });
  if (!res.ok) throw new Error(`DeepSeek API ${res.status}: ${await res.text()}`);
  const data = await res.json();
  const parsed = JSON.parse(data.choices[0].message.content);
  return [...(parsed.area || []), ...(parsed.platform || []), ...(parsed.severity || [])].filter((l) => ALLOWED.has(l));
}

const issues = JSON.parse(
  gh(['issue', 'list', '--state', 'open', '--limit', String(limit), '--json', 'number,title,body,labels']),
);
console.log(`${issues.length} open issues; dryRun=${dryRun} onlyUnlabeled=${onlyUnlabeled}`);

let changed = 0;
for (const it of issues) {
  const existing = it.labels.map((l) => l.name);
  if (onlyUnlabeled && existing.some((l) => AREA.includes(l))) {
    continue;
  }
  let labels;
  try {
    labels = await classify(it.title, (it.body || '').slice(0, 4000));
  } catch (e) {
    console.warn(`#${it.number}: classify failed: ${e.message}`);
    continue;
  }
  if (!labels.some((l) => AREA.includes(l))) labels.push('needs-triage');
  const toAdd = labels.filter((l) => !existing.includes(l));
  if (!toAdd.length) {
    console.log(`#${it.number}: nothing new`);
    continue;
  }
  if (dryRun) {
    console.log(`#${it.number}: would add ${toAdd.join(', ')}  — ${it.title.slice(0, 50)}`);
  } else {
    gh(['issue', 'edit', String(it.number), ...toAdd.flatMap((l) => ['--add-label', l])]);
    console.log(`#${it.number}: +${toAdd.join(', ')}`);
  }
  changed++;
}
console.log(`Done. ${changed} issue(s) ${dryRun ? 'would be' : ''} updated.`);
