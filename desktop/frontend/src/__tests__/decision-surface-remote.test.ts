// Run: tsx src/__tests__/decision-surface-remote.test.ts
import assert from "node:assert/strict";
import { projectDecisionSurface } from "../app-runtime/decisionSurfaceProjection";

const base = { approval: undefined, ask: undefined, mcpInteraction: undefined, extensionForm: undefined, workspaceConflict: null, pendingClose: null, clearContextPending: false } as unknown as Parameters<typeof projectDecisionSurface>[0];
const ask = { id: "1", questions: [{ id: "q1", prompt: "?", options: [{ label: "a" }, { label: "b" }] }] } as unknown as Parameters<typeof projectDecisionSurface>[0]["ask"];
const approval = { id: "call-1", tool: "bash" } as unknown as Parameters<typeof projectDecisionSurface>[0]["approval"];

assert.equal(projectDecisionSurface({ ...base, ask }), "ask", "a local ask docks in the footer");
assert.equal(projectDecisionSurface({ ...base, ask, remote: true }), null, "a remote ask belongs to the remote surface, not a second footer card");
assert.equal(projectDecisionSurface({ ...base, approval, remote: true }), null, "a remote approval is not duplicated in the footer");
assert.equal(projectDecisionSurface({ ...base, ask, remote: true, clearContextPending: true }), "clear_context", "shell decisions still reach the footer on a remote tab");
console.log("decision-surface-remote: 4 passed");
