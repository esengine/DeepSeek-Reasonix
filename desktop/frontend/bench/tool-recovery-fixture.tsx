import React from "react";
import { createRoot } from "react-dom/client";
import { ToolRecoveryPanel } from "../src/components/ToolRecoveryPanel";
import { LocaleProvider } from "../src/lib/i18n";
import type { ToolRecoveryBindings, ToolRecoverySnapshot } from "../src/lib/toolRecovery";
import "../src/styles.css";

const initial: ToolRecoverySnapshot = {
  sessionPath: "fixture", runtimeEpoch: "epoch", revision: "one", retryEnabled: true,
  calls: [{ identity: { attempt_id: "attempt", canonical_tool: "write_file", resource_scope: "fixture.txt", argument_digest: "digest" }, state: "unknown", read_only: false }],
};
let view = structuredClone(initial);
const actions: string[] = [];
let release: ((view: ToolRecoverySnapshot) => void) | undefined;
const bindings: ToolRecoveryBindings = {
  async GetToolRecoveryForTab(tab) { return tab === "new-tab" ? { ...initial, calls: [] } : structuredClone(view); },
  async ResolveToolRecoveryForTab(_, req) {
    actions.push(req.action);
    if (req.action === "inspect") {
      if ((window as any).holdRecovery) return new Promise(resolve => { release = resolve; });
      view = { ...view, revision: "two", calls: view.calls.map(c => ({ ...c, inspection_id: "proof", inspection_state: "unknown", arguments: { path: "fixture.txt", content: "local receipt" } })) };
    } else if (req.action === "confirm") view = { ...view, revision: "three", calls: [] };
    else if (req.action === "reject") view = { ...view, revision: "three", calls: view.calls.map(c => ({ ...c, resolution: "reject" })) };
    return structuredClone(view);
  },
};
const root = createRoot(document.getElementById("root")!);
function render(tab = "fixture-tab") {
  root.render(<LocaleProvider><main className="transcript-shell" style={{ maxWidth: 720, margin: 32, height: 600 }}><div className="transcript"><p>Existing transcript</p></div><ToolRecoveryPanel tabId={tab} sessionKey={tab} running={false} refreshKey={1} bindings={bindings} onResume={() => actions.push("resume")} /></main></LocaleProvider>);
}
Object.assign(window, { recoveryFixture: { actions, render, reset() { view = structuredClone(initial); render(); }, release() { release?.({ ...initial, calls: initial.calls.map(c => ({ ...c, identity: { ...c.identity, canonical_tool: "STALE_RESULT" } })) }); } } });
render();
