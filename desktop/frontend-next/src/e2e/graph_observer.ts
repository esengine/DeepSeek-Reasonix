import { deliver } from "./shell_shim";
import { SsePort } from "../port/sse";
import { ExecutionStore } from "../state/execution";
import type { WireEvent } from "../port/wire";

// Run by benchmarks/runtime-resume against a live kernel: it attaches the real
// client to a resumed session and prints every visible state the run graph
// passed through. The probe asks what the user could have seen, not what the
// state settled on — a history replayed as if it were happening now agrees on
// the second and is wrong about the first.

declare const process: {
  argv: string[];
  env: Record<string, string | undefined>;
  exit(code: number): never;
};

interface Entry {
  phase: string;
  origin: string;
  sessionId: string;
  states: Record<string, string>;
  interruptions: { execution: string; kind: string }[];
  identityUnknown: string[];
}

const arg = (name: string, fallback = ""): string => {
  const at = process.argv.indexOf("--" + name);
  return at >= 0 && at + 1 < process.argv.length ? process.argv[at + 1] : fallback;
};

const base = arg("base");
const prompt = arg("prompt");
// A deliberate defect, run to prove the probe can see it. "publish" is driven
// on the kernel side; this one is the frontend half — the line that used to
// feed the graph out of the recorded trajectory.
const sabotage = arg("sabotage");
const deadlineMs = Number(arg("deadline", "60000"));

const store = new ExecutionStore();
const port = new SsePort(base, "");
const trace: Entry[] = [];
// What each delta the view folded named, whether or not it moved the state. A
// republished history changes nothing visible and is still the kernel telling
// this session that finished work is happening now.
const deltas: string[][] = [];

function fold(ev: WireEvent) {
  if (ev.kind === "graph_delta" && ev.graph) {
    deltas.push([...(ev.graph.nodes ?? []).map((n) => n.id), ...(ev.graph.edges ?? []).map((e) => e.to)]);
  }
  store.onEvent(ev);
}

function record() {
  const s = store.read();
  const states: Record<string, string> = {};
  for (const n of s.graph.nodes) states[n.id] = n.state ?? "";
  trace.push({
    phase: s.phase,
    origin: s.origin,
    sessionId: s.sessionId,
    states,
    interruptions: s.interruptions.map((i) => ({ execution: i.execution, kind: i.kind })),
    identityUnknown: [...s.identityUnknown],
  });
}

// The shell's pump: one SSE connection, every frame forwarded onto the bus
// verbatim. It runs before the client attaches, the way the window's pump
// outlives any one page load.
async function pump(): Promise<void> {
  const res = await fetch(base + "/events", { headers: { accept: "text/event-stream" } });
  const body = res.body;
  if (!body) throw new Error("/events answered without a stream");
  const decoder = new TextDecoder();
  const reader = body.getReader();
  let held = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) return;
    held += decoder.decode(value, { stream: true });
    const lines = held.split("\n");
    held = lines.pop() ?? "";
    for (const line of lines) {
      if (line.startsWith("data:")) deliver(line.slice(5).trim());
    }
  }
}

const settled = () => new Promise((r) => setTimeout(r, 50));

async function until(what: string, ready: () => boolean): Promise<void> {
  const stop = Date.now() + deadlineMs;
  while (Date.now() < stop) {
    if (ready()) return;
    await settled();
  }
  throw new Error("timed out waiting for " + what);
}

async function main() {
  void pump().catch(() => {});
  record();
  store.subscribe(record);
  const read = () => port.executionGraph();
  const stop = port.subscribe(
    (ev) => fold(ev),
    () => void store.recoverFromGap(read),
    () => store.bootstrap(read),
  );

  if (sabotage === "trajectory") {
    const replayed = await port.trajectory();
    for (const ev of replayed) fold(ev);
  }

  await until("the run graph to load", () => store.read().phase === "live");
  const historical = new Set(store.read().graph.nodes.map((n) => n.id));

  if (prompt) {
    const res = await fetch(base + "/submit", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ input: prompt }),
    });
    if (!res.ok) throw new Error("/submit answered " + res.status);
    // Work that starts now is the positive control: if the view only ever
    // accepted snapshots, everything above would still pass and nothing would
    // ever move again.
    const settledStates = new Set(["completed", "failed", "adopted", "skipped", "cancelled"]);
    await until("work started after the resume to settle", () =>
      store.read().graph.nodes.some((n) => !historical.has(n.id) && settledStates.has(n.state ?? "")),
    );
  }
  stop();
  console.log(JSON.stringify({ trace, deltas }));
}

main().then(
  () => process.exit(0),
  (err: unknown) => {
    console.log(JSON.stringify({ trace, deltas, err: String(err) }));
    process.exit(1);
  },
);
