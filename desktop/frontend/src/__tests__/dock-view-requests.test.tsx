import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useDockViewRequests } from "../app-shell/useDockViewRequests";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
let forwarded: ReturnType<typeof useDockViewRequests>;
function Harness({ scope, view, request }: { scope: string; view: string | null; request: { id: number; path: string } }) {
  const props = { revealPathRequest: request, tabId: "source-session" };
  forwarded = useDockViewRequests(scope, view, props);
  return null;
}
const first = { id: 1, path: "a.ts" };
const next = { id: 2, path: "b.ts" };
const paint = (scope: string, view: string | null, request = first) =>
  act(async () => root.render(<Harness scope={scope} view={view} request={request} />));
await paint("project-a", "view-a");
assert.equal(forwarded!.revealPathRequest, first);
assert(!("tabId" in forwarded!), "forwarding requests must not retain session data or callbacks");
await paint("project-a", "view-a");
assert.equal(forwarded!.revealPathRequest, first);
await paint("project-a", "view-b");
assert.equal(forwarded!.revealPathRequest, null, "another view must not inherit the pending reveal");
await paint("project-a", "view-a");
assert.equal(forwarded!.revealPathRequest, null, "returning must restore navigation rather than replay an old reveal");
await paint("project-a", null, next);
await paint("project-a", "view-b", next);
assert.equal(forwarded!.revealPathRequest, next, "a new command while closed is delivered when the panel opens");
await paint("project-b", "view-b", next);
assert.equal(forwarded!.revealPathRequest, null, "retained command cannot cross projects");
await act(async () => root.unmount());
dom.window.close();
console.log("PASS view-owned reveal requests, remount and project isolation");
