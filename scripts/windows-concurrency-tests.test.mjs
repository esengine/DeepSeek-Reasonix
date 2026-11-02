import assert from 'node:assert/strict';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';
import { readInventory, verifyInventory } from './windows-concurrency-tests.mjs';

test('long invalid subtest suffix is rejected without repeated slash partitioning', () => {
  const dir = mkdtempSync(join(tmpdir(), 'concurrency-inventory-'));
  try {
    const inventory = tests => JSON.stringify([{ package: 'reasonix/internal/example', tests }]);
    const file = join(dir, 'names.json');
    const valid = ['Test0', 'Test0/a/b', 'Test0/!/', 'Test0//'];
    writeFileSync(file, inventory(valid));
    assert.deepEqual([...readInventory(dir).values()][0], new Set(valid));
    writeFileSync(file, inventory(['Test0', `Test0/${'!/'.repeat(10000)} `]));
    const script = `import assert from 'node:assert/strict';
      import { readInventory } from ${JSON.stringify(new URL('./windows-concurrency-tests.mjs', import.meta.url).href)};
      assert.throws(() => readInventory(process.argv[1]), /invalid test/);`;
    const result = spawnSync(process.execPath, ['--input-type=module', '-e', script, dir], { timeout: 5000, encoding: 'utf8' });
    assert.ifError(result.error);
    assert.equal(result.status, 0, result.stderr);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('additive manifests keep package-qualified requirements', () => {
  const dir = mkdtempSync(join(tmpdir(), 'concurrency-inventory-'));
  try {
    const a = 'reasonix/internal/first', b = 'reasonix/internal/second';
    writeFileSync(join(dir, 'one.json'), JSON.stringify([{ package: a, tests: ['TestShared', 'TestShared/case'] }]));
    writeFileSync(join(dir, 'two.json'), JSON.stringify([{ package: b, tests: ['TestShared'] }]));
    const required = readInventory(dir);
    const events = [
      { Package: a, Action: 'pass', Test: 'TestShared/case' },
      { Package: a, Action: 'pass', Test: 'TestShared' },
      { Package: a, Action: 'pass' },
      { Package: b, Action: 'pass', Test: 'TestShared' },
      { Package: b, Action: 'pass' },
    ];
    const raw = values => values.map(value => JSON.stringify(value)).join('\r\n');
    assert.deepEqual(verifyInventory(required, raw(events)), { requiredTests: 3, packages: 2 });
    for (let i = 0; i < events.length; i++) {
      assert.throws(() => verifyInventory(required, raw(events.filter((_, index) => index !== i))));
      assert.throws(() => verifyInventory(required, raw(events.map((event, index) => index === i ? { ...event, Action: 'skip' } : event))));
    }
    assert.throws(() => verifyInventory(required, raw([...events, { Package: a, Action: 'fail' }])));
    assert.throws(() => verifyInventory(required, raw([...events, events[0]])));
    assert.throws(() => verifyInventory(required, '{'));
    writeFileSync(join(dir, 'two.json'), JSON.stringify([{ package: b, tests: ['TestOrphan/case'] }]));
    assert.throws(() => readInventory(dir));
    writeFileSync(join(dir, 'two.json'), JSON.stringify([{ package: '../other', tests: ['TestShared'] }]));
    assert.throws(() => readInventory(dir));
    writeFileSync(join(dir, 'two.json'), '[]');
    assert.throws(() => readInventory(dir));
    rmSync(join(dir, 'one.json'));
    rmSync(join(dir, 'two.json'));
    assert.throws(() => readInventory(dir));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
