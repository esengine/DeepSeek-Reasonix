import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { app } from "../lib/bridge";

const dom = new JSDOM("", { url: "http://localhost/?mock=bench" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage });
try {
  const local = (await app.ListTabs())[0];
  const remote = await app.OpenRemoteProjectTab("demo", "~/app", { sessionName: "intro" });
  assert.ok((await app.ListTabs()).some(tab => tab.id === remote.id), "remote open and ListTabs share the backend catalog");
  await app.SetActiveTab(remote.id);
  assert.deepEqual((await app.ListTabs()).filter(tab => tab.active).map(tab => tab.id), [remote.id]);
  await app.SetRemoteTabModel(remote.id, "fixture/model");
  assert.equal((await app.ListTabs()).find(tab => tab.id === remote.id)?.label, "fixture/model");
  const renewed = await app.OpenRemoteProjectTab("demo", "~/app", { newSession: true });
  assert.equal(renewed.id, remote.id, "remote new session reuses its workspace surface");
  assert.equal((await app.ListTabs()).filter(tab => tab.id === remote.id).length, 1);
  assert.equal((await app.ListTabs()).find(tab => tab.id === remote.id)?.topicTitle, "New session");
  const status = await app.RemoteTabStatus(remote.id) as Record<string, unknown>;
  assert.equal(status.plan, false);
  assert.equal(status.toolApprovalMode, "ask");
  assert.equal(status.goal, "");
  assert.deepEqual((await app.RemoteTabSnapshot(remote.id)).status, status, "snapshot and status share the authoritative composer profile");
  await app.SetActiveTab(local.id);
  assert.deepEqual((await app.ListTabs()).filter(tab => tab.active).map(tab => tab.id), [local.id]);
  await app.CloseRemoteTab(remote.id);
  assert.ok(!(await app.ListTabs()).some(tab => tab.id === remote.id));
  await assert.rejects(app.SetActiveTab(remote.id), /not found/);
  console.log("mock remote catalog: open, refresh, model, new session, selection and close agree");
} finally { dom.window.close(); }
