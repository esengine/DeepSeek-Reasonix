// Run: tsx src/__tests__/markdown-worker-client.test.ts
//
// MarkdownWorkerClient protocol behavior: request ids, cancellation (stale
// responses dropped), in-process fallback when Worker is unavailable, dispose
// semantics, and pending-map hygiene across many cycles. Runs without a DOM:
// Node has no Worker global, which also exercises the fallback branch.

import {
  MarkdownWorkerClient,
  type MarkdownParseRequest,
  type MarkdownParseResponse,
  type MarkdownWorkerLike,
} from "../lib/markdownWorkerClient";
import type { MarkdownBlock } from "../lib/markdownPipeline";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

// Node has no Worker global: the client only attempts worker creation when one
// exists. Stub it so worker-path tests reach the injected fake; the fallback
// test below deletes it again.
(globalThis as { Worker?: unknown }).Worker = class {};

/** Manual-response fake worker: tests decide when (or whether) it answers. */
class FakeWorker implements MarkdownWorkerLike {
  onmessage: ((event: MessageEvent<MarkdownParseResponse>) => void) | null = null;
  onerror: ((event: ErrorEvent) => void) | null = null;
  sent: MarkdownParseRequest[] = [];
  terminated = 0;
  postMessage(request: MarkdownParseRequest): void {
    this.sent.push(request);
  }
  respond(id: number, blocks: MarkdownBlock[]): void {
    this.onmessage?.({ data: { id, blocks } } as MessageEvent<MarkdownParseResponse>);
  }
  fail(id: number, error: string): void {
    this.onmessage?.({ data: { id, error } } as MessageEvent<MarkdownParseResponse>);
  }
  crash(): void {
    this.onerror?.(new ErrorEvent("error"));
  }
  terminate(): void {
    this.terminated += 1;
  }
}

const BLOCKS: MarkdownBlock[] = [{ key: "b0", children: [{ type: "text", value: "hi" }] }];

console.log("\nmarkdown worker client");

// ── happy path over a fake worker ────────────────────────────────────────────
{
  const worker = new FakeWorker();
  const client = new MarkdownWorkerClient({ createWorker: () => Promise.resolve(worker) });
  const handle = client.parse("hello");
  await tick();
  eq(worker.sent.length, 1, "request posted to the worker");
  worker.respond(worker.sent[0].id, BLOCKS);
  const blocks = await handle.promise;
  eq(blocks, BLOCKS, "response resolves with parsed blocks");
  eq(client.pendingCount, 0, "pending map drains after a response");
}

// ── cancellation drops the stale response ────────────────────────────────────
{
  const worker = new FakeWorker();
  const client = new MarkdownWorkerClient({ createWorker: () => Promise.resolve(worker) });
  const handle = client.parse("stale");
  await tick();
  handle.cancel();
  const cancelled = await handle.promise;
  eq(cancelled, undefined, "cancelled parse resolves undefined");
  eq(worker.terminated, 1, "cancelling active work terminates the stale parser");
  worker.respond(worker.sent[0].id, BLOCKS); // late response for a dead id
  await tick();
  eq(client.pendingCount, 0, "late response for a cancelled id is dropped");
}

// ── cancel while the worker chunk is still loading ───────────────────────────
{
  let releaseWorker: (worker: FakeWorker) => void = () => {};
  const workerPromise = new Promise<FakeWorker>((resolve) => {
    releaseWorker = resolve;
  });
  const client = new MarkdownWorkerClient({ createWorker: () => workerPromise });
  const handle = client.parse("early cancel");
  handle.cancel();
  releaseWorker(new FakeWorker());
  eq(await handle.promise, undefined, "parse cancelled during worker startup resolves undefined");
  await tick();
  eq(client.pendingCount, 0, "no pending entry survives an early cancel");
}

// ── worker error rejects (callers fall back main-thread) ─────────────────────
{
  const worker = new FakeWorker();
  const client = new MarkdownWorkerClient({ createWorker: () => Promise.resolve(worker) });
  const handle = client.parse("boom");
  await tick();
  worker.fail(worker.sent[0].id, "parse exploded");
  const message = await handle.promise.then(
    () => "resolved",
    (error: Error) => error.message,
  );
  eq(message, "parse exploded", "worker parse error rejects with its message");
  eq(client.pendingCount, 0, "pending map drains after an error");
}

// ── worker crash rejects stranded requests, next parse retries ───────────────
{
  const worker = new FakeWorker();
  let creations = 0;
  const client = new MarkdownWorkerClient({
    createWorker: () => {
      creations += 1;
      return Promise.resolve(worker);
    },
  });
  const handle = client.parse("stranded");
  await tick();
  worker.crash();
  const message = await handle.promise.then(
    () => "resolved",
    (error: Error) => error.message,
  );
  eq(message, "markdown worker failed", "crashed worker rejects stranded requests");
  const retry = client.parse("again");
  await tick();
  eq(creations, 2, "a fresh parse recreates the worker after a crash");
  worker.respond(worker.sent[worker.sent.length - 1].id, BLOCKS);
  eq(await retry.promise, BLOCKS, "recreated worker serves the retry");
}

// ── in-process fallback when Worker is unavailable ───────────────────────────
{
  delete (globalThis as { Worker?: unknown }).Worker;
  const seen: string[] = [];
  const client = new MarkdownWorkerClient({
    parseInProcess: (text) => {
      seen.push(text);
      return BLOCKS;
    },
  });
  const blocks = await client.parse("fallback text").promise;
  eq(blocks, BLOCKS, "fallback resolves parsed blocks without a Worker");
  eq(seen.join(","), "fallback text", "fallback receives the exact source text");
  (globalThis as { Worker?: unknown }).Worker = class {};
}

// ── dispose settles pending work and terminates the worker ───────────────────
{
  const worker = new FakeWorker();
  const client = new MarkdownWorkerClient({ createWorker: () => Promise.resolve(worker) });
  const first = client.parse("one");
  const second = client.parse("two");
  await tick();
  client.dispose();
  eq(await first.promise, undefined, "dispose settles a pending request (1)");
  eq(await second.promise, undefined, "dispose settles a pending request (2)");
  eq(worker.terminated, 1, "dispose terminates the worker");
  eq(client.pendingCount, 0, "dispose drains the pending map");
  const after = await client.parse("post-dispose").promise;
  eq(after, undefined, "parse after dispose resolves undefined immediately");
}

// ── pending-map hygiene across 100 cycles ────────────────────────────────────
{
  const worker = new FakeWorker();
  const client = new MarkdownWorkerClient({ createWorker: () => Promise.resolve(worker) });
  const unhandled: unknown[] = [];
  const onUnhandled = (reason: unknown) => unhandled.push(reason);
  process.on("unhandledRejection", onUnhandled);
  try {
    for (let cycle = 0; cycle < 100; cycle += 1) {
      const handle = client.parse(`cycle ${cycle}`);
      await tick();
      const id = worker.sent[worker.sent.length - 1].id;
      if (cycle % 3 === 0) {
        handle.cancel();
        worker.respond(id, BLOCKS); // dropped
      } else if (cycle % 3 === 1) {
        worker.respond(id, BLOCKS);
      } else {
        worker.fail(id, `error ${cycle}`);
      }
      await handle.promise.catch(() => {});
    }
    await tick();
    eq(client.pendingCount, 0, "no pending-request growth across 100 mixed cycles");
    eq(unhandled.length, 0, "no unhandled rejections across 100 mixed cycles");
    eq(worker.sent.length, 100, "every cycle issued exactly one request");
  } finally {
    process.off("unhandledRejection", onUnhandled);
  }
}

// ── request ids are monotonic ────────────────────────────────────────────────
{
  const worker = new FakeWorker();
  const client = new MarkdownWorkerClient({ createWorker: () => Promise.resolve(worker) });
  const a = client.parse("a");
  const b = client.parse("b");
  await tick();
  eq(worker.sent.length, 1, "worker queue runs only one parse at a time");
  worker.respond(worker.sent[0].id, BLOCKS);
  await a.promise;
  await tick();
  ok(worker.sent[1].id > worker.sent[0].id, "request ids increase monotonically");
  client.dispose();
  await b.promise;
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
