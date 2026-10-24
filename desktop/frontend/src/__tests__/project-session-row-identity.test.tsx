import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { projectSessionRowKey } from "../lib/projectSessionIdentity";
import type { ProjectNode } from "../lib/types";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const source: ProjectNode = { key: "source", kind: "global_topic", label: "History",
  source: { hostId: "local", sourceKey: "row-identity-source", path: "/history.jsonl" } };
const sourceKey = projectSessionRowKey(source);
const original: ProjectNode = { key: "original", kind: "global_topic", label: "History",
  session: { hostId: "local", sessionId: "row-original" }, identityAliases: [sourceKey] };
const reviewed: ProjectNode = { ...original, key: "reviewed",
  session: { hostId: "local", sessionId: "row-reviewed" } };
assert.equal(projectSessionRowKey(original), sourceKey, "adoption preserves the mounted source row");
assert.notEqual(projectSessionRowKey({ ...source,
  source: { ...source.source!, sourceKey: "unowned-source" }, identityAliases: [sourceKey] }), sourceKey,
  "an unowned source cannot borrow an already canonical key through a secondary alias");
const root = createRoot(document.getElementById("root")!);
const warnings: unknown[][] = [];
const oldError = console.error;
console.error = (...args) => { warnings.push(args); };
try {
  // Old peers/previously loaded pages may still publish a conflicting alias.
  // Reorder siblings and insert/remove a third row as catalog pushes do.
  const other: ProjectNode = { key: "other", kind: "global_topic", label: "Other",
    session: { hostId: "local", sessionId: "row-other" } };
  for (let cycle = 0; cycle < 20; cycle++) {
    const rows = cycle % 2 ? [source, other, reviewed, original] : [original, reviewed];
    await act(async () => root.render(<ul>{rows.map(row =>
      <li key={projectSessionRowKey(row)} data-session={row.session?.sessionId ?? "source"}>{row.label}</li>)}</ul>));
    assert.deepEqual(Array.from(document.querySelectorAll("li"), row => row.getAttribute("data-session")),
      rows.map(row => row.session?.sessionId ?? "source"), "catalog refresh must not multiply mounted rows");
  }
  assert.notEqual(projectSessionRowKey(original), projectSessionRowKey(reviewed), "canonical sessions never share a React key");
  assert.equal(warnings.length, 0, "no duplicate-key or reconciliation warnings");
} finally {
  console.error = oldError;
  await act(async () => root.unmount());
  dom.window.close();
}
console.log("PASS source adoption preserves identity without merging independent canonical rows");
