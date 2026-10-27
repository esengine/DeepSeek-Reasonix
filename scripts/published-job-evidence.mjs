import { createHash } from 'node:crypto';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';

const required = [
  'TestPublishedJobChild',
  'TestPublishedJobNoPIDFallback',
  'TestPublishedJobNoPIDFallback/after_wait',
  'TestPublishedJobNoPIDFallback/cancelled_wait',
  'TestPublishedJobUsesKernelTree',
  'TestPublishedJobFallbackControl',
  'TestPublishedJobRecorderAlias',
];
const failures = new Set(required.slice(1, 5));
const [mode, destination] = process.argv.slice(2);

if (mode === 'overlay') {
  const source = resolve('internal/base/proc/run.go');
  let old = readFileSync(source, 'utf8').replaceAll('\r\n', '\n');
  for (const [current, preceding] of [
    ['\tif !job.Tracked() {\n\t\ttracked.setTree(TrackTree(cmd))\n\t}', '\ttracked.setTree(TrackTree(cmd))'],
    ['\tjob := p.job\n\ttree := p.tree', '\tjob := p.job\n\tp.job = nil\n\ttree := p.tree'],
    ['\tif !firstKill && !job.Tracked() {', '\tif !firstKill {'],
    ['\tkilled := p.killed\n\tp.job = job', '\tkilled := p.killed\n\tif !killed {\n\t\tp.job = job\n\t}'],
  ]) {
    if (old.split(current).length !== 2) throw new Error('owner reversal is not unique');
    old = old.replace(current, preceding);
  }
  const sha256 = createHash('sha256').update(old).digest('hex');
  const dir = resolve(destination);
  mkdirSync(dir, { recursive: true });
  const replacement = join(dir, 'run.go');
  writeFileSync(replacement, old);
  writeFileSync(join(dir, 'overlay.json'), JSON.stringify({ Replace: { [source]: replacement } }));
  writeFileSync(join(dir, 'owner-proof.json'), JSON.stringify({ source, sha256 }, null, 2));
} else if (mode === 'positive' || mode === 'negative') {
  const events = readFileSync(destination, 'utf8').trim().split('\n').map(line => JSON.parse(line));
  const pkg = 'reasonix/internal/base/proc';
  const terminals = events.filter(e => e.Package === pkg && ['pass', 'fail', 'skip'].includes(e.Action));
  if (terminals.length !== required.length + 1) throw new Error('unexpected terminal inventory');
  for (const name of [...required, undefined]) {
    const entries = terminals.filter(e => e.Test === name);
    const failed = mode === 'negative' && (name === undefined || failures.has(name));
    if (entries.length !== 1 || entries[0].Action !== (failed ? 'fail' : 'pass')) {
      throw new Error(`unexpected terminal for ${name ?? pkg}`);
    }
  }
  if (events.some(e => e.Package !== pkg || e.Action === 'build-fail')) throw new Error('unexpected package or build failure');
} else {
  throw new Error('expected overlay, positive or negative mode');
}
