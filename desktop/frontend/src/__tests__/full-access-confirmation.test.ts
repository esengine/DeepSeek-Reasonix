import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import {
  fullAccessProjectConfirmationKey,
  hasConfirmedFullAccessForProject,
  rememberFullAccessConfirmationForProject,
} from "../lib/fullAccessConfirmation";

const dom = new JSDOM("", { url: "http://localhost" });
globalThis.localStorage = dom.window.localStorage;

try {
  const project = fullAccessProjectConfirmationKey({ workspacePath: "/repo/project/" });
  const sameProject = fullAccessProjectConfirmationKey({ workspacePath: " /repo/project " });
  const otherProject = fullAccessProjectConfirmationKey({ workspacePath: "/repo/other" });
  const remoteProject = fullAccessProjectConfirmationKey({ workspacePath: "/repo/project", remoteHostId: "host-a" });
  const otherRemoteHost = fullAccessProjectConfirmationKey({ workspacePath: "/repo/project", remoteHostId: "host-b" });

  assert.equal(project, sameProject, "cosmetic trailing separators do not create a new project identity");
  assert.notEqual(project, otherProject, "different project folders remain isolated");
  assert.notEqual(project, remoteProject, "local and remote projects never share confirmation state");
  assert.notEqual(remoteProject, otherRemoteHost, "remote hosts with the same path remain isolated");
  assert.equal(fullAccessProjectConfirmationKey({}), "", "an unknown folder cannot persist an acknowledgement");

  assert.equal(hasConfirmedFullAccessForProject(project), false);
  rememberFullAccessConfirmationForProject(project);
  assert.equal(hasConfirmedFullAccessForProject(project), true, "confirmed projects survive later reads");
  assert.equal(hasConfirmedFullAccessForProject(otherProject), false);
  assert.equal(hasConfirmedFullAccessForProject(remoteProject), false);

  localStorage.setItem("reasonix-full-access-confirmed-projects-v1", "not-json");
  assert.equal(hasConfirmedFullAccessForProject(project), false, "invalid persisted data fails safely");
  rememberFullAccessConfirmationForProject(project);
  assert.equal(hasConfirmedFullAccessForProject(project), true, "invalid persisted data can be repaired by a new confirmation");

  console.log("full access confirmation: project persistence and isolation passed");
} finally {
  dom.window.close();
}
