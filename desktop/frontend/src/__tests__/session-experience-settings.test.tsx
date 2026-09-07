import assert from "node:assert/strict";
import React, { act, useState } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { SessionExperienceSettings } from "../components/SessionExperienceSettings";
import { LocaleProvider } from "../lib/i18n";
import { getSessionExperience } from "../lib/sessionExperience";
import type { SettingsView } from "../lib/types";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage,
  CustomEvent: dom.window.CustomEvent, IS_REACT_ACT_ENVIRONMENT: true });
let backend: SettingsView = { sessionExperience: "standard" } as SettingsView;
let release!: () => void;
let failed = false;
const writes: string[] = [];
Object.assign(window, { go: { main: { App: { SetSessionExperience: async (mode: string) => {
  writes.push(mode);
  await new Promise<void>(resolve => { release = resolve; });
  if (failed) throw new Error("write failed");
  backend = { ...backend, sessionExperience: mode as "deep" | "standard" };
} } } } });
let completion: Promise<boolean>;
let reload!: () => void;
function SettingsHost() {
  const [snapshot, setSnapshot] = useState(backend);
  const [busy, setBusy] = useState(false);
  reload = () => setSnapshot({ ...backend });
  // Exercise the component's shared apply/reload boundary, not a guessed rollback.
  const apply = (write: () => Promise<unknown>) => {
    setBusy(true);
    completion = (async () => {
      try { await write(); return true; } catch { return false; }
      finally { reload(); setBusy(false); }
    })();
    return completion;
  };
  return <SessionExperienceSettings snapshot={snapshot} busy={busy} apply={apply} />;
}
const root = createRoot(document.getElementById("root")!);
const buttons = () => [...document.querySelectorAll<HTMLButtonElement>("[role=radio]")];
try {
  await act(async () => root.render(<LocaleProvider><SettingsHost /></LocaleProvider>));
  assert.equal(buttons().length, 2);
  assert.equal(buttons()[0].getAttribute("aria-checked"), "true");
  await act(async () => buttons()[1].click());
  assert.equal(getSessionExperience(), "deep");
  assert.ok(buttons().every(button => button.disabled));
  await act(async () => { release(); await completion; });
  assert.equal(buttons()[1].getAttribute("aria-checked"), "true");

  failed = true;
  await act(async () => buttons()[0].click());
  assert.equal(getSessionExperience(), "standard");
  await act(async () => { release(); await completion; });
  assert.equal(getSessionExperience(), "deep", "failed write reloads even when backend returns the same previous value");
  assert.equal(buttons()[1].getAttribute("aria-checked"), "true");
  assert.deepEqual(writes, ["deep", "standard"]);

  backend = { ...backend, sessionExperience: undefined };
  await act(async () => reload());
  assert.equal(getSessionExperience(), "standard");
  assert.equal(buttons()[0].getAttribute("aria-checked"), "true");
  assert.equal(buttons()[0].tabIndex, 0, "both segment buttons remain keyboard reachable");
  assert.equal(buttons()[1].tabIndex, 0);
  console.log("session experience controls: success, failure snapshot, busy state, legacy backend and keyboard reachability passed");
} finally { await act(async () => root.unmount()); dom.window.close(); }
