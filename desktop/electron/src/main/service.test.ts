import assert from "node:assert/strict";
import type { ChildProcess } from "node:child_process";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import { test } from "node:test";
import type { ServiceState } from "../shared/ipc.js";
import { validateHelloResult, type HelloResult } from "./handshake.js";
import { RestartBudget } from "./restartBudget.js";
import { ServiceSupervisor } from "./service.js";

const silent = { info() {}, warn() {}, error() {} };
const tick = async (times = 4) => {
  for (let i = 0; i < times; i++) await new Promise((resolve) => setImmediate(resolve));
};

class FakeChild extends EventEmitter {
  stdin = new PassThrough();
  stdout = new PassThrough();
  stderr = new PassThrough();
  alive = true;
  requests: Array<{ id: number; method: string; params: unknown }> = [];
  private buffered = "";

  constructor(readonly generation: string, readonly behaviour: { helloError?: { code: number; message: string }; exitOnStdinEnd?: boolean }) {
    super();
    this.stdin.on("data", (chunk: Buffer) => {
      this.buffered += chunk.toString("utf8");
      let index: number;
      while ((index = this.buffered.indexOf("\n")) >= 0) {
        const line = this.buffered.slice(0, index);
        this.buffered = this.buffered.slice(index + 1);
        this.handle(JSON.parse(line) as { id?: number; method?: string; params?: unknown });
      }
    });
    this.stdin.on("end", () => {
      if (this.behaviour.exitOnStdinEnd !== false) this.exit(0, null);
    });
  }

  kill(): boolean {
    this.exit(null, "SIGKILL");
    return true;
  }

  exit(code: number | null, signal: NodeJS.Signals | null): void {
    if (!this.alive) return;
    this.alive = false;
    this.emit("exit", code, signal);
  }

  send(frame: Record<string, unknown>): void {
    this.stdout.write(JSON.stringify({ jsonrpc: "2.0", ...frame }) + "\n");
  }

  event(name: string, generation = this.generation): void {
    this.send({ method: "desktop/event", params: { seq: 1, generation, name, args: [{ ok: true }] } });
  }

  private handle(frame: { id?: number; method?: string; params?: unknown }): void {
    if (typeof frame.id !== "number" || typeof frame.method !== "string") return;
    this.requests.push({ id: frame.id, method: frame.method, params: frame.params });
    if (frame.method === "desktop/hello") {
      if (this.behaviour.helloError) {
        this.send({ id: frame.id, error: this.behaviour.helloError });
        return;
      }
      this.send({
        id: frame.id,
        result: {
          protocolVersion: 1,
          contractDigest: "sha256:abc",
          service: { version: "dev", channel: "dev", commit: "dev", pid: 1 },
          runtimeGeneration: this.generation,
          resources: { origin: "http://127.0.0.1:1", token: "t" },
          window: { width: 1000, height: 700, minWidth: 760, minHeight: 480, frameless: false, zoomFactor: 1 },
        },
      });
      return;
    }
    if (frame.method === "desktop/invoke") {
      const params = frame.params as { method: string; args: unknown[] };
      if (params.method === "Fail") this.send({ id: frame.id, error: { code: -32000, message: "workspace not found", data: { method: "Fail" } } });
      else this.send({ id: frame.id, result: { method: params.method, args: params.args } });
      return;
    }
    this.send({ id: frame.id, result: {} });
  }
}

function harness(options: { children?: FakeChild[]; budget?: RestartBudget; onState?(state: ServiceState): void } = {}) {
  const spawned: FakeChild[] = [];
  const states: ServiceState[] = [];
  const events: string[] = [];
  const ready: Array<{ generation: string; restarted: boolean }> = [];
  const failures: string[] = [];
  let index = 0;
  const supervisor = new ServiceSupervisor(
    {
      binary: "fake",
      args: ["--host-rpc"],
      env: {},
      onStderr: () => undefined,
      log: silent,
      budget: options.budget ?? new RestartBudget(),
      exitGraceMs: 10,
      spawn: () => {
        const child = options.children?.[index++] ?? new FakeChild(`g-${spawned.length + 1}`, {});
        spawned.push(child);
        return child as unknown as ChildProcess;
      },
    },
    {
      hello: async (client) => validateHelloResult(await client.request("desktop/hello", {}, 1000)),
      onRequest: async () => ({}),
      onEvent: (frame) => events.push(`${frame.generation}:${frame.name}`),
      onState: (state) => { states.push(state); options.onState?.(state); },
      onReady: (hello: HelloResult, restarted) => { ready.push({ generation: hello.runtimeGeneration, restarted }); },
      onFailed: (error) => failures.push(error instanceof Error ? error.message : String(error)),
    },
  );
  return { supervisor, spawned, states, events, ready, failures };
}

test("start runs hello then desktop/start and exposes the generation", async () => {
  const h = harness();
  const hello = await h.supervisor.start();
  assert.equal(hello.runtimeGeneration, "g-1");
  assert.deepEqual(h.spawned[0]?.requests.map((r) => r.method), ["desktop/hello", "desktop/start"]);
  assert.equal(h.supervisor.ready, true);
  assert.equal(h.supervisor.generation, "g-1");
  assert.deepEqual(h.states.map((s) => s.phase), ["starting", "ready"]);
  assert.deepEqual(h.ready, [{ generation: "g-1", restarted: false }]);
  assert.deepEqual(await h.supervisor.invoke("OpenProjectTab", ["/p", true]), { method: "OpenProjectTab", args: ["/p", true] });
  await assert.rejects(h.supervisor.invoke("Fail", []), /workspace not found/);
});

test("events from the live generation are forwarded and stale ones dropped", async () => {
  const h = harness();
  await h.supervisor.start();
  h.spawned[0]?.event("agent:event");
  h.spawned[0]?.event("agent:event", "g-old");
  h.spawned[0]?.event("duplicate");
  h.spawned[0]?.send({ method: "desktop/event", params: { seq: 3, generation: "g-1", name: "after-gap", args: [] } });
  h.spawned[0]?.send({ method: "desktop/event", params: { seq: 2, generation: "g-1", name: "late", args: [] } });
  await tick();
  assert.deepEqual(h.events, ["g-1:agent:event", "g-1:after-gap"]);
});

test("a handshake error fails the service without a restart and terminates the process", async () => {
  const child = new FakeChild("g-1", { helloError: { code: -32003, message: "digest differs" } });
  const h = harness({ children: [child] });
  await assert.rejects(h.supervisor.start(), /digest differs/);
  await tick();
  assert.equal(h.supervisor.current.phase, "failed");
  assert.deepEqual(h.failures, ["digest differs"]);
  assert.equal(child.alive, false, "stdin close makes the fake exit");
  assert.equal(h.spawned.length, 1);
});

test("an unexpected exit restarts automatically until the budget is exhausted", async () => {
  const budget = new RestartBudget(2, 60_000);
  const h = harness({ budget });
  await h.supervisor.start();
  h.spawned[0]?.exit(1, null);
  await tick(8);
  assert.equal(h.spawned.length, 2);
  assert.equal(h.supervisor.generation, "g-2");
  assert.deepEqual(h.ready.map((r) => r.restarted), [false, true]);
  h.spawned[1]?.exit(1, null);
  await tick(8);
  assert.equal(h.spawned.length, 3);
  h.spawned[2]?.exit(1, null);
  await tick(8);
  assert.equal(h.spawned.length, 3, "no fourth spawn once the budget is spent");
  assert.equal(h.supervisor.current.phase, "failed");
  assert.match(h.supervisor.current.error ?? "", /automatic restarts exhausted/);
  await assert.rejects(h.supervisor.invoke("X", []), /not running/);
  await h.supervisor.restart();
  assert.equal(h.spawned.length, 4, "a manual restart is always allowed");
  assert.equal(h.supervisor.generation, "g-4");
  assert.deepEqual(h.states.map((s) => s.phase), ["starting", "ready", "restarting", "ready", "restarting", "ready", "failed", "restarting", "ready"]);
});

test("shutdown sends desktop/shutdown, closes stdin and waits for the exit", async () => {
  const h = harness();
  await h.supervisor.start();
  await h.supervisor.shutdown();
  const child = h.spawned[0] as FakeChild;
  assert.deepEqual(child.requests.map((r) => r.method), ["desktop/hello", "desktop/start", "desktop/shutdown"]);
  assert.equal(child.alive, false);
  assert.equal(h.supervisor.current.phase, "exited");
  assert.equal(h.spawned.length, 1, "a deliberate exit never restarts");
});

test("a service that ignores stdin close is killed after the grace period", async () => {
  const child = new FakeChild("g-1", { exitOnStdinEnd: false });
  const h = harness({ children: [child] });
  await h.supervisor.start();
  await h.supervisor.shutdown();
  assert.equal(child.alive, false);
  assert.equal(h.supervisor.current.phase, "exited");
});

test("shutdown fences a hello completion queued before shutdown", async () => {
  const h = harness();
  const starting = h.supervisor.start();
  const rejected = assert.rejects(starting, /cancelled|exited/);
  await h.supervisor.shutdown();
  await rejected;
  assert.deepEqual(h.ready, []);
  assert.deepEqual(h.failures, []);
  assert.equal(h.spawned.length, 1);
  await assert.rejects(h.supervisor.start(), /shutting down/);
});

test("a destroyed renderer throwing during exit notification cannot prevent shutdown", async () => {
  const h = harness({ onState: (state) => { if (state.phase === "exited") throw new Error("Object has been destroyed"); } });
  await h.supervisor.start();
  await h.supervisor.shutdown();
  assert.equal(h.supervisor.current.phase, "exited");
  assert.equal(h.spawned[0].alive, false);
});

test("concurrent restarts share one replacement and shutdown prevents its spawn", async () => {
  const h = harness();
  await h.supervisor.start();
  const a = h.supervisor.restart();
  const b = h.supervisor.restart();
  const rejected = Promise.all([assert.rejects(a, /shutting down/), assert.rejects(b, /shutting down/)]);
  await h.supervisor.shutdown();
  await rejected;
  assert.equal(h.spawned.length, 1);
});
