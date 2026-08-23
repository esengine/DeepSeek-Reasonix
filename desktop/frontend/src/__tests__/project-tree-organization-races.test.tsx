// Run: tsx src/__tests__/project-tree-organization-races.test.tsx

import { JSDOM } from "jsdom";
import React, { StrictMode } from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { useProjectTreeOrganization } from "../components/ProjectTreeOrganization";
import type { ProjectNode, ProjectTreeOrganizationBindings, SessionGroup } from "../lib/types";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  process.stdout.write(`  ${value ? "PASS" : "FAIL"}  ${label}\n`);
  if (value) passed += 1; else failed += 1;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.MouseEvent = dom.window.MouseEvent;
  return dom;
}

const folder: ProjectNode = {
  key: "project-/repo",
  kind: "project",
  label: "Repo",
  root: "/repo",
  children: [],
};

function Harness({ bindings, revision = 0 }: { bindings: ProjectTreeOrganizationBindings; revision?: number }) {
  const organization = useProjectTreeOrganization({
    tree: [folder],
    refresh: async () => {},
    organizationRevision: revision,
    bindings,
  });
  const groups = organization.groupsFor(folder);
  return <>
    <output id="groups">{JSON.stringify(groups)}</output>
    <button id="create" onClick={() => organization.createGroup(folder, "Local")}>create</button>
  </>;
}

async function flush() {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(flush);
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

async function mount(bindings: ProjectTreeOrganizationBindings) {
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  let revision = 0;
  const render = async (nextRevision = revision) => {
    revision = nextRevision;
    await act(async () => {
      root.render(<StrictMode><Harness bindings={bindings} revision={revision} /></StrictMode>);
      await flush();
    });
  };
  await render();
  return { dom, root, render };
}

async function cleanup(dom: JSDOM, root: Root) {
  await act(async () => root.unmount());
  dom.window.close();
}

function legacyBindings(list: () => Promise<SessionGroup[]>): ProjectTreeOrganizationBindings {
  return {
    ReorderTopics: async () => {},
    ListProjectGroups: async () => list(),
    SaveSessionGroups: async () => {},
  };
}

console.log("\nproject tree organization races");

{
  const initial = deferred<SessionGroup[]>();
  const { dom, root } = await mount(legacyBindings(() => initial.promise));
  await act(async () => {
    initial.resolve([{ id: "existing", title: "Existing", topicIds: [] }]);
    await flush();
  });
  await waitFor("StrictMode group load", () => document.getElementById("groups")?.textContent?.includes("Existing") === true);
  ok(true, "StrictMode effect replay does not discard the deferred group load");
  await cleanup(dom, root);
}

{
  const initial = deferred<SessionGroup[]>();
  const { dom, root } = await mount(legacyBindings(() => initial.promise));
  await act(async () => {
    (document.getElementById("create") as HTMLButtonElement).click();
    await flush();
  });
  initial.resolve([{ id: "stale", title: "Stale", topicIds: [] }]);
  await act(flush);
  const text = document.getElementById("groups")?.textContent ?? "";
  ok(text.includes("Local") && !text.includes("Stale"), "a stale initial read cannot overwrite an optimistic mutation");
  await cleanup(dom, root);
}

{
  let state: SessionGroup[] = [];
  let revision = 0;
  let conflictInjected = false;
  const bindings: ProjectTreeOrganizationBindings = {
    ReorderTopics: async () => {},
    ListProjectGroups: async () => structuredClone(state),
    SaveSessionGroups: async (_scope, _root, groups) => { state = structuredClone(groups); revision += 1; },
    GetProjectGroups: async () => ({ groups: structuredClone(state), revision, applied: true }),
    SaveSessionGroupsVersioned: async (_scope, _root, expected, groups) => {
      if (!conflictInjected) {
        conflictInjected = true;
        state = [{ id: "remote", title: "Remote", topicIds: [] }];
        revision += 1;
      }
      if (expected !== revision) return { groups: structuredClone(state), revision, applied: false };
      state = structuredClone(groups);
      revision += 1;
      return { groups: structuredClone(state), revision, applied: true };
    },
  };
  const { dom, root } = await mount(bindings);
  await act(async () => {
    (document.getElementById("create") as HTMLButtonElement).click();
    await flush();
  });
  await waitFor("CAS rebase", () => state.length === 2);
  await waitFor("CAS UI reconciliation", () => {
    const text = document.getElementById("groups")?.textContent ?? "";
    return text.includes("Remote") && text.includes("Local");
  });
  ok(state.some((group) => group.title === "Remote") && state.some((group) => group.title === "Local"),
    "CAS conflict rebases and displays the local mutation without losing the remote group");
  await cleanup(dom, root);
}

{
  let state: SessionGroup[] = [{ id: "one", title: "One", topicIds: ["archived"] }];
  let revision = 1;
  const bindings: ProjectTreeOrganizationBindings = {
    ReorderTopics: async () => {},
    ListProjectGroups: async () => structuredClone(state),
    SaveSessionGroups: async () => {},
    GetProjectGroups: async () => ({ groups: structuredClone(state), revision, applied: true }),
    SaveSessionGroupsVersioned: async () => ({ groups: structuredClone(state), revision, applied: false }),
  };
  const { dom, root, render } = await mount(bindings);
  await waitFor("initial archive membership", () => document.getElementById("groups")?.textContent?.includes("archived") === true);
  state = [{ id: "one", title: "One", topicIds: [] }];
  revision += 1;
  await render(1);
  await waitFor("metadata invalidation", () => !document.getElementById("groups")?.textContent?.includes("archived"));
  ok(true, "metadata revision invalidates loaded groups after archive cleanup");
  await cleanup(dom, root);
}

process.stdout.write(`\nproject-tree-organization-races: ${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
