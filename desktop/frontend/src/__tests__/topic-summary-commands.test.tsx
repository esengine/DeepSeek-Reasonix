import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useTopicSummary } from "../app-runtime/useTopicSummary";
import type { TabMeta } from "../lib/types";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function tab(topicId: string, scope = "project", workspaceRoot = "/repo"): TabMeta {
  return { id: `tab-${topicId}`, scope, workspaceRoot, topicId } as TabMeta;
}

const requests: { scope: string; workspaceRoot: string; topicId: string }[] = [];
const gates = new Map<string, ReturnType<typeof deferred<{ turns?: number }>>>();
let failWith: Error | null = null;
window.go = {
  main: {
    App: {
      GetTopicSummary: (request: { scope: string; workspaceRoot: string; topicId: string }) => {
        requests.push(request);
        if (failWith) return Promise.reject(failWith);
        const gate = gates.get(request.topicId);
        return gate ? gate.promise : Promise.resolve({ turns: request.topicId.length });
      },
    },
  },
} as unknown as typeof window.go;

let states!: ReturnType<typeof useTopicSummary>;
function Probe({ target, revision = 0 }: { target?: TabMeta; revision?: number }) {
  states = useTopicSummary({ activeTab: target, revision });
  return null;
}
const paint = (props?: { target?: TabMeta; revision?: number }) => act(async () => root.render(<Probe {...props} />));

try {
  await paint();
  assert.equal(states.activeTopicTurns, undefined, "no active tab yields no turns");
  assert.equal(requests.length, 0, "no active tab issues no summary request");

  await paint({ target: tab("alpha") });
  assert.equal(states.activeTopicTurns, 5, "a topic target resolves its turn count");
  assert.deepEqual(requests, [{ scope: "project", workspaceRoot: "/repo", topicId: "alpha" }],
    "project topics fetch with their workspace root");

  await paint({ target: tab("alpha"), revision: 1 });
  assert.equal(requests.length, 2, "a project revision refetches the same topic identity");
  assert.equal(states.activeTopicTurns, 5, "refetch keeps the resolved turns");

  gates.set("beta", deferred());
  gates.set("gamma", deferred());
  await paint({ target: tab("beta"), revision: 2 });
  await paint({ target: tab("gamma"), revision: 3 });
  await act(async () => { gates.get("beta")!.resolve({ turns: 99 }); });
  assert.equal(states.activeTopicTurns, 5, "a superseded topic identity cannot overwrite the committed turns");
  await act(async () => { gates.get("gamma")!.resolve({ turns: 7 }); });
  assert.equal(states.activeTopicTurns, 7, "the newest topic identity owns the committed turns");
  assert.deepEqual(requests.map((r) => r.topicId), ["alpha", "alpha", "beta", "gamma"], "every identity change fetches exactly once");

  failWith = new Error("summary offline");
  await paint({ target: tab("delta"), revision: 4 });
  assert.equal(states.activeTopicTurns, undefined, "a failed fetch clears the turns");
  failWith = null;

  await paint({ target: tab("global-1", "global") });
  assert.deepEqual(requests.at(-1), { scope: "global", workspaceRoot: "", topicId: "global-1" },
    "global topics fetch without a workspace root");

  await act(async () => root.unmount());
  console.log("topic summary commands: identity memo, single-flight fetch, revision refetch and failure clearing passed");
} finally { dom.window.close(); }
