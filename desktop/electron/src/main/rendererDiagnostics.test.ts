import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import test, { type TestContext } from 'node:test';
import { RendererDiagnostics } from './rendererDiagnostics.js';

const profile = {
  nodes: [{ id: 1, callFrame: { functionName: 'render', url: 'reasonix://app/main.js', lineNumber: 12 } }],
  samples: [1, 1], timeDeltas: [10_000, 10_000], startTime: 0, endTime: 20_000,
};
const frames = [{ label: 'render', samples: 2, selfMs: 20 }];

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

// Resolve bounded command chains without wall-clock sleeps. Timers remain under test control.
async function flush() {
  for (let i = 0; i < 30; i++) await Promise.resolve();
}

class DebuggerDouble extends EventEmitter {
  attached = false;
  attaches = 0;
  detaches = 0;
  commands: string[] = [];
  command: (method: string) => Promise<unknown> = async method =>
    method === 'Profiler.stop' ? { profile } : {};

  isAttached() { return this.attached; }
  attach() { this.attached = true; this.attaches++; }
  detach() { this.attached = false; this.detaches++; this.emit('detach'); }
  sendCommand(method: string) { this.commands.push(method); return this.command(method); }
}

function setup(t: TestContext, options: {
  durationMs?: number; cooldownMs?: number; maxCaptures?: number; commandTimeoutMs?: number;
  analyse?: () => Promise<typeof frames>;
  onInvalidated?: (listener: () => void) => () => void;
} = {}) {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'], now: 1_000_000 });
  const debug = new DebuggerDouble();
  let foreground = true;
  let destroyed = false;
  let devTools = false;
  let invalidated: (() => void) | undefined;
  let analysed = 0;
  const target = { debugger: debug, isDestroyed: () => destroyed, isDevToolsOpened: () => devTools };
  const owner = new RendererDiagnostics({
    target: () => target,
    isForeground: () => foreground,
    onInvalidated: cb => { invalidated = cb; return () => { invalidated = undefined; }; },
    analyse: async received => { assert.deepEqual(received, profile); analysed++; return frames; },
    now: () => Date.now(),
    ...options,
  });
  t.after(() => owner.dispose());
  return {
    owner, debug,
    foreground: (value: boolean) => { foreground = value; },
    destroyed: (value: boolean) => { destroyed = value; },
    devTools: (value: boolean) => { devTools = value; },
    invalidate: () => invalidated?.(),
    analysed: () => analysed,
    subscribed: () => invalidated !== undefined,
    advance: async (ms: number) => { t.mock.timers.tick(ms); await flush(); },
  };
}

test('construction has no profiling cost and never attaches until requested', t => {
  const { debug } = setup(t);
  assert.equal(debug.attaches, 0);
  assert.deepEqual(debug.commands, []);
});

test('inactive or destroyed renderers cannot start a profile', async t => {
  const fixture = setup(t);
  fixture.foreground(false);
  assert.equal((await fixture.owner.capture()).status, 'inactive');
  fixture.foreground(true);
  fixture.destroyed(true);
  assert.equal((await fixture.owner.capture()).status, 'unavailable');
  assert.equal(fixture.debug.attaches, 0);
});

test('an existing debugger or open DevTools is never taken over', async t => {
  const fixture = setup(t);
  fixture.debug.attached = true;
  assert.equal((await fixture.owner.capture()).status, 'unavailable');
  fixture.debug.attached = false;
  fixture.devTools(true);
  assert.equal((await fixture.owner.capture()).status, 'unavailable');
  assert.equal(fixture.debug.attaches, 0);
  assert.equal(fixture.debug.detaches, 0);
  assert.deepEqual(fixture.debug.commands, []);
});

test('only one capture runs and the default capture stops after five seconds', async t => {
  const fixture = setup(t);
  const result = fixture.owner.capture();
  await flush();
  assert.equal((await fixture.owner.capture()).status, 'busy');
  assert.equal(fixture.debug.attaches, 1);
  assert.equal(fixture.debug.commands.filter(command => command === 'Profiler.start').length, 1);
  await fixture.advance(4999);
  assert.equal(fixture.debug.commands.includes('Profiler.stop'), false);
  await fixture.advance(1);
  const captured = await result;
  assert.equal(captured.status, 'captured');
  assert.deepEqual(captured.frames, frames);
  assert.equal(fixture.analysed(), 1);
  assert.equal(fixture.debug.attached, false);
  assert.equal(fixture.debug.detaches, 1);
});

for (const reason of ['cancel', 'invalidate', 'dispose'] as const) {
  test(`${reason} ends the active recording and never starts another automatically`, async t => {
    const fixture = setup(t);
    const result = fixture.owner.capture();
    await flush();
    if (reason === 'invalidate') fixture.invalidate();
    else fixture.owner[reason]();
    await flush();
    assert.equal((await result).status, 'cancelled');
    assert.equal(fixture.debug.attached, false);
    assert.equal(fixture.debug.commands.filter(command => command === 'Profiler.stop').length, 1);
    assert.equal(fixture.analysed(), 0);
    await fixture.advance(60_000);
    assert.equal(fixture.debug.attaches, 1);
    if (reason === 'dispose') assert.equal(fixture.subscribed(), false);
  });
}

test('cooldown and lifetime capture limits bound repeated diagnostics', async t => {
  const fixture = setup(t, { durationMs: 100, cooldownMs: 1000, maxCaptures: 2 });
  const first = fixture.owner.capture();
  await flush();
  await fixture.advance(100);
  assert.equal((await first).status, 'captured');
  assert.equal((await fixture.owner.capture()).status, 'cooldown');
  await fixture.advance(1001);
  const second = fixture.owner.capture();
  await flush();
  await fixture.advance(100);
  assert.equal((await second).status, 'captured');
  await fixture.advance(1001);
  assert.equal((await fixture.owner.capture()).status, 'limit');
  assert.equal(fixture.debug.attaches, 2);
});

test('a rejected CDP command releases the owned debugger and returns failure', async t => {
  const fixture = setup(t);
  fixture.debug.command = async () => { throw new Error('CDP unavailable'); };
  assert.equal((await fixture.owner.capture()).status, 'failed');
  assert.equal(fixture.debug.attached, false);
  assert.equal(fixture.debug.detaches, 1);
  assert.equal(fixture.analysed(), 0);
});

test('a timed out command cannot leave the debugger attached or resume late', async t => {
  const fixture = setup(t, { commandTimeoutMs: 100 });
  const command = deferred<unknown>();
  fixture.debug.command = () => command.promise;
  const result = fixture.owner.capture();
  await flush();
  await fixture.advance(100);
  assert.equal((await result).status, 'failed');
  assert.equal(fixture.debug.attached, false);
  const commandsBeforeLateCompletion = fixture.debug.commands.length;
  command.resolve({});
  await flush();
  await fixture.advance(10_000);
  assert.equal(fixture.debug.commands.length, commandsBeforeLateCompletion);
  assert.equal(fixture.debug.attaches, 1);
});

test('external detach invalidates ownership before a later debugger session appears', async t => {
  const fixture = setup(t);
  const result = fixture.owner.capture();
  await flush();
  fixture.debug.attached = false;
  fixture.debug.emit('detach');
  // DevTools or another debugger may immediately attach after Chromium detaches ours.
  fixture.debug.attached = true;
  await flush();
  assert.equal((await result).status, 'cancelled');
  await fixture.advance(10_000);
  fixture.owner.dispose();
  assert.equal(fixture.debug.attached, true);
  assert.equal(fixture.debug.detaches, 0);
  assert.equal(fixture.debug.commands.includes('Profiler.stop'), false);
  assert.equal(fixture.analysed(), 0);
});

test('cancellation during pending setup prevents late setup from starting recording', async t => {
  const fixture = setup(t);
  const enable = deferred<unknown>();
  fixture.debug.command = method => method === 'Profiler.enable' ? enable.promise : Promise.resolve({});
  const result = fixture.owner.capture();
  await flush();
  fixture.owner.cancel();
  enable.resolve({});
  await flush();
  assert.equal((await result).status, 'cancelled');
  assert.equal(fixture.debug.commands.includes('Profiler.start'), false);
  assert.equal(fixture.debug.attached, false);
});

test('a stop timeout releases the debugger and ignores a late profile reply', async t => {
  const fixture = setup(t, { durationMs: 100, commandTimeoutMs: 50 });
  const stop = deferred<unknown>();
  fixture.debug.command = method => method === 'Profiler.stop' ? stop.promise : Promise.resolve({});
  const result = fixture.owner.capture();
  await flush();
  await fixture.advance(100);
  await fixture.advance(50);
  assert.equal((await result).status, 'failed');
  assert.equal(fixture.debug.attached, false);
  stop.resolve({ profile });
  await flush();
  assert.equal(fixture.analysed(), 0);
  assert.equal(fixture.debug.detaches, 1);
});

test('invalidation during analysis discards stale frames and keeps captures single flight', async t => {
  const analysis = deferred<typeof frames>();
  const fixture = setup(t, { durationMs: 100, analyse: () => analysis.promise });
  const result = fixture.owner.capture();
  await flush();
  await fixture.advance(100);
  assert.equal((await fixture.owner.capture()).status, 'busy');
  fixture.invalidate();
  analysis.resolve(frames);
  await flush();
  const cancelled = await result;
  assert.equal(cancelled.status, 'cancelled');
  assert.equal(cancelled.frames, undefined);
  assert.equal(fixture.debug.attached, false);
});

test('analysis failure releases the debugger without exposing an incomplete profile', async t => {
  const fixture = setup(t, {
    durationMs: 100,
    analyse: async () => { throw new Error('analysis worker unavailable'); },
  });
  const result = fixture.owner.capture();
  await flush();
  await fixture.advance(100);
  assert.equal((await result).status, 'failed');
  assert.equal(fixture.debug.attached, false);
  assert.equal(fixture.debug.detaches, 1);
});

test('cancellation during stop retains single flight and discards the completed profile', async t => {
  const fixture = setup(t, { durationMs: 100 });
  const stop = deferred<unknown>();
  fixture.debug.command = method => method === 'Profiler.stop' ? stop.promise : Promise.resolve({});
  const result = fixture.owner.capture();
  await flush();
  await fixture.advance(100);
  fixture.owner.cancel();
  assert.equal((await fixture.owner.capture()).status, 'busy');
  stop.resolve({ profile });
  await flush();
  assert.equal((await result).status, 'cancelled');
  assert.equal(fixture.analysed(), 0);
  assert.equal(fixture.debug.detaches, 1);
});

test('listener registration failure returns failure and does not permanently occupy the owner', async t => {
  const fixture = setup(t, {
    cooldownMs: 0,
    onInvalidated: () => { throw new Error('renderer exited during registration'); },
  });
  assert.equal((await fixture.owner.capture()).status, 'failed');
  assert.equal((await fixture.owner.capture()).status, 'failed');
  assert.equal(fixture.debug.attaches, 0);
});

test('listener cleanup failure cannot skip debugger cleanup or replace the capture result', async t => {
  const fixture = setup(t, {
    durationMs: 100,
    onInvalidated: () => () => { throw new Error('renderer exited during cleanup'); },
  });
  const result = fixture.owner.capture();
  await flush();
  await fixture.advance(100);
  assert.equal((await result).status, 'captured');
  assert.equal(fixture.debug.attached, false);
  assert.equal(fixture.debug.detaches, 1);
  assert.equal((await fixture.owner.capture()).status, 'cooldown');
});

test('a late cancellation for an old request cannot interrupt a newer capture', async t => {
  const fixture = setup(t, { durationMs: 100, cooldownMs: 1000 });
  const old = fixture.owner.capture('old');
  await flush();
  await fixture.advance(100);
  assert.equal((await old).status, 'captured');
  await fixture.advance(1001);

  const current = fixture.owner.capture('new');
  await flush();
  fixture.owner.cancel('old');
  await flush();
  assert.equal(fixture.debug.attached, true);
  assert.equal((await fixture.owner.capture('another')).status, 'busy');
  await fixture.advance(99);
  assert.equal(fixture.debug.attached, true);
  await fixture.advance(1);
  assert.equal((await current).status, 'captured');
  assert.equal(fixture.analysed(), 2);
  assert.equal(fixture.debug.attached, false);
});

test('a cancellation with the active request identity ends its own capture', async t => {
  const fixture = setup(t);
  const result = fixture.owner.capture('new');
  await flush();
  fixture.owner.cancel('new');
  await flush();
  assert.equal((await result).status, 'cancelled');
  assert.equal(fixture.analysed(), 0);
  assert.equal(fixture.debug.attached, false);
});
