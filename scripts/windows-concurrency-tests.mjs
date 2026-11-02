// Focused Windows concurrency contracts live in additive, package-qualified
// inventories. New regressions do not need another workflow step or verifier.
import assert from 'node:assert/strict';
import { closeSync, mkdirSync, openSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

export function readInventory(directory) {
  const files = readdirSync(directory).filter(name => name.endsWith('.json')).sort();
  assert(files.length > 0, 'no Windows concurrency inventories');
  const required = new Map();
  for (const file of files) {
    const groups = JSON.parse(readFileSync(join(directory, file), 'utf8'));
    assert(Array.isArray(groups) && groups.length > 0, `${file}: empty inventory`);
    for (const group of groups) {
      assert(group && /^reasonix\/internal\/(?:[a-z0-9_]+\/)*[a-z0-9_]+$/.test(group.package), `${file}: invalid package`);
      assert(Array.isArray(group.tests) && group.tests.length > 0, `${file}: empty tests`);
      const tests = required.get(group.package) ?? new Set();
      for (const test of group.tests) {
        assert(typeof test === 'string' && /^Test[A-Za-z0-9_]+(?:\/[^\s]+)?$/.test(test), `${file}: invalid test`);
        tests.add(test);
      }
      required.set(group.package, tests);
    }
  }
  for (const tests of required.values()) {
    for (const test of tests) assert(tests.has(test.split('/')[0]), `missing parent for ${test}`);
  }
  return required;
}

export function verifyInventory(required, raw) {
  const passed = new Map();
  const packages = new Set();
  for (const line of raw.split(/\r?\n/).filter(line => line.trim())) {
    const event = JSON.parse(line);
    assert(required.has(event.Package), `unexpected package: ${event.Package}`);
    assert(!['skip', 'fail'].includes(event.Action), `${event.Package}: ${event.Action}: ${event.Test ?? '(package)'}`);
    if (event.Action !== 'pass') continue;
    if (!event.Test) {
      packages.add(event.Package);
      continue;
    }
    const tests = passed.get(event.Package) ?? new Set();
    assert(!tests.has(event.Test), `duplicate pass: ${event.Package}/${event.Test}`);
    tests.add(event.Test);
    passed.set(event.Package, tests);
  }
  let count = 0;
  for (const [pkg, tests] of required) {
    assert(packages.has(pkg), `missing package terminal: ${pkg}`);
    for (const test of tests) {
      assert(passed.get(pkg)?.has(test), `missing test: ${pkg}/${test}`);
      count++;
    }
  }
  return { requiredTests: count, packages: required.size };
}

function main() {
  const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
  const required = readInventory(join(root, '.github/windows-concurrency'));
  const roots = [...new Set([...required.values()].flatMap(tests => [...tests].map(test => test.split('/')[0])))].sort();
  const output = resolve(process.argv[2] ?? join(process.env.RUNNER_TEMP ?? '.', 'concurrency-windows.jsonl'));
  mkdirSync(dirname(output), { recursive: true });
  const fd = openSync(output, 'w');
  let result;
  try {
    result = spawnSync('go', ['test', '-count=1', '-timeout=3m', '-json', '-run', `^(${roots.join('|')})$`, ...required.keys()], {
      cwd: root, stdio: ['ignore', fd, 'inherit'],
    });
  } finally {
    closeSync(fd);
  }
  assert(!result.error, result.error?.message);
  assert.equal(result.status, 0, `go test failed (${result.signal ?? result.status}); see ${output}`);
  console.log(JSON.stringify({ ...verifyInventory(required, readFileSync(output, 'utf8')), output }));
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
