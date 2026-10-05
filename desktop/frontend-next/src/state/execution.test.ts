import { describe, expect, it } from "vitest";
import { ExecutionStore } from "./execution";
import type { ExecutionGraphRead, GraphNode } from "../port/wire";

const view = (over: Partial<ExecutionGraphRead> = {}): ExecutionGraphRead => ({
  schemaVersion: 1,
  sessionId: "s1",
  watermark: 100,
  graph: { nodes: [], edges: [] },
  ...over,
});

const node = (id: string, over: Partial<GraphNode> = {}): GraphNode => ({ id, kind: "worker", ...over });

// A read the test lands by hand, which is the only way to put a delta between
// the request and its answer — the window every case here is about.
const gate = () => {
  let land!: (v: ExecutionGraphRead) => void;
  let refuse!: (why: Error) => void;
  const read = new Promise<ExecutionGraphRead>((res, rej) => {
    land = res;
    refuse = rej;
  });
  return { read: () => read, land, refuse };
};

const stateOf = (store: ExecutionStore, id: string) => {
  const at = store.read().graph.at.get(id);
  return at === undefined ? undefined : store.read().graph.nodes[at].state;
};

const ids = (store: ExecutionStore) => store.read().graph.nodes.map((n) => n.id);

describe("the execution read model", () => {
  it("replaces the model rather than merging into it", async () => {
    const store = new ExecutionStore();
    await store.bootstrap(async () =>
      view({
        graph: { nodes: [node("a"), node("b")] },
        interruptions: [{ execution: "a", kind: "interrupted-during-execution" }],
        identityUnknown: ["b"],
      }),
    );
    await store.bootstrap(async () => view({ watermark: 200, graph: { nodes: [node("c")] } }));

    expect(ids(store)).toEqual(["c"]);
    // Both lists shrink as well as grow, and only the snapshot ever carries
    // them: a merge would keep an interruption the rebuild has since resolved.
    expect(store.read().interruptions).toEqual([]);
    expect(store.read().identityUnknown).toEqual([]);
  });

  it("folds a delta the snapshot already holds without forking or reverting it", async () => {
    const store = new ExecutionStore();
    await store.bootstrap(async () =>
      view({
        graph: {
          nodes: [node("b", { state: "completed", label: "survey" })],
          edges: [{ from: "a", to: "b", kind: "depends" }],
        },
      }),
    );
    store.applyDelta(
      { nodes: [{ id: "b", state: "completed" }], edges: [{ from: "a", to: "b", kind: "depends" }] },
      101,
    );

    expect(store.read().graph.nodes).toHaveLength(1);
    expect(store.read().graph.nodes[0]).toMatchObject({ state: "completed", label: "survey" });
    expect(store.read().graph.edges).toHaveLength(1);
  });

  // The transport holds frames only for the first read; a gap happens with the
  // stream running, so this model has to hold its own.
  it("holds a delta that lands while the authority is being re-read", async () => {
    const store = new ExecutionStore();
    await store.bootstrap(async () => view({ watermark: 50, graph: { nodes: [node("a")] } }));

    const g = gate();
    const done = store.recoverFromGap(g.read);
    store.applyDelta({ nodes: [{ id: "b", state: "running" }] }, 101);
    g.land(view({ watermark: 100, graph: { nodes: [node("a"), node("b", { state: "queued" })] } }));
    await done;

    expect(ids(store)).toEqual(["a", "b"]);
    // The snapshot was rebuilt at 100 and knows nothing of 101; dropping the
    // held delta would leave the node showing a wait it has already left.
    expect(stateOf(store, "b")).toBe("running");
  });

  // The other side of the same cut: duplicates are safe, older facts are not.
  it("drops a held delta the snapshot was rebuilt past", async () => {
    const store = new ExecutionStore();
    await store.bootstrap(async () => view({ watermark: 50, graph: { nodes: [node("a", { state: "running" })] } }));

    const g = gate();
    const done = store.recoverFromGap(g.read);
    store.applyDelta({ nodes: [{ id: "a", state: "running" }] }, 90);
    g.land(view({ watermark: 100, graph: { nodes: [node("a", { state: "completed" })] } }));
    await done;

    expect(stateOf(store, "a")).toBe("completed");
  });

  it("refuses an answer for a conversation it has left", async () => {
    const store = new ExecutionStore();
    const first = gate();
    const pending = store.bootstrap(first.read);

    store.resetForSession();
    await store.bootstrap(async () => view({ sessionId: "B", watermark: 300, graph: { nodes: [node("b1")] } }));
    first.land(view({ sessionId: "A", watermark: 100, graph: { nodes: [node("a1")] } }));
    await pending;

    expect(store.read().sessionId).toBe("B");
    expect(ids(store)).toEqual(["b1"]);
  });

  it("drops what it held when the answer names another conversation", async () => {
    const store = new ExecutionStore();
    await store.bootstrap(async () => view({ sessionId: "A", watermark: 100, graph: { nodes: [node("a1")] } }));

    const g = gate();
    const done = store.recoverFromGap(g.read);
    // Numbered past the cut, so only its conversation can disqualify it.
    store.applyDelta({ nodes: [{ id: "a2", state: "running" }] }, 900);
    g.land(view({ sessionId: "B", watermark: 500, graph: { nodes: [node("b1")] } }));
    await done;

    expect(ids(store)).toEqual(["b1"]);
  });

  it("keeps an unrecorded identity apart from an inherited one", async () => {
    const store = new ExecutionStore();
    await store.bootstrap(async () =>
      view({ graph: { nodes: [node("a"), node("b")] }, identityUnknown: ["a"] }),
    );

    expect(store.read().identityUnknown).toEqual(["a"]);
    // Nothing in the stream carries the list, so a delta cannot claim a node
    // either way — which is what keeps "not observed" from becoming "inherited".
    store.applyDelta({ nodes: [{ id: "b", model: "" }] }, 101);
    expect(store.read().identityUnknown).toEqual(["a"]);
  });

  it("shows nothing from the conversation it left while the next one loads", async () => {
    const store = new ExecutionStore();
    await store.bootstrap(async () =>
      view({ graph: { nodes: [node("a")] }, interruptions: [{ execution: "a", kind: "interrupted-before-start" }] }),
    );

    store.resetForSession();
    expect(store.read().phase).toBe("loading");
    expect(ids(store)).toEqual([]);
    expect(store.read().interruptions).toEqual([]);
  });

  it("says a snapshot replaced the model, not that work just started", async () => {
    const store = new ExecutionStore();
    await store.bootstrap(async () => view({ graph: { nodes: [node("a", { state: "completed" })] } }));
    expect(store.read().origin).toBe("snapshot");

    store.applyDelta({ nodes: [{ id: "b", state: "running" }] }, 101);
    expect(store.read().origin).toBe("delta");
  });

  it("answers the transport with the frame its snapshot was read at", async () => {
    const store = new ExecutionStore();
    await expect(store.bootstrap(async () => view({ watermark: 412 }))).resolves.toBe(412);
  });

  // Answering zero would tell the transport the read succeeded, and it would
  // resume from the start of the log.
  it("refuses when the authority cannot be read, and folds what it held", async () => {
    const store = new ExecutionStore();
    const g = gate();
    const boot = store.bootstrap(g.read);
    store.applyDelta({ nodes: [{ id: "a", state: "running" }] }, 5);
    g.refuse(new Error("unreadable"));

    await expect(boot).rejects.toThrow("unreadable");
    expect(store.read().phase).toBe("live");
    expect(ids(store)).toEqual(["a"]);
  });

  it("tells its readers when the state moved, and stops when they leave", async () => {
    const store = new ExecutionStore();
    let changes = 0;
    const off = store.subscribe(() => changes++);
    await store.bootstrap(async () => view({ graph: { nodes: [node("a")] } }));
    expect(changes).toBeGreaterThan(0);

    // A delta carrying nothing hands back the state it was given, so the reader
    // keeps the memos it holds on that object.
    const held = store.read();
    const quiet = changes;
    store.applyDelta({}, 101);
    expect(store.read()).toBe(held);
    expect(changes).toBe(quiet);

    off();
    store.applyDelta({ nodes: [{ id: "z", state: "running" }] }, 102);
    expect(changes).toBe(quiet);
    expect(ids(store)).toContain("z");
  });
});
