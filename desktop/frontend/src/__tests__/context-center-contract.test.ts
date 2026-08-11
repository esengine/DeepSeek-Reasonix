import { readFileSync } from "node:fs";

function ok(value: unknown, message: string) {
  if (!value) throw new Error(message);
}

const source = readFileSync(new URL("../components/MemoryPanel.tsx", import.meta.url), "utf8");

ok(source.includes("function memoryFactKey"), "Context Center must use a stable fact identity helper");
ok(source.includes("app.MemoryRevisionsForTab"), "Context Center must load revision history for the selected workspace");
ok(source.includes("app.RestoreMemoryRevisionForTab"), "Context Center must restore a selected revision");
ok(source.includes("view.lastRecall"), "Context Center must explain the last automatic recall");
ok(source.includes("view.conflicts"), "Context Center must explain project-over-global memory resolution");
ok(source.includes("view.instructionDiagnostics"), "Context Center must surface instruction diagnostics");
ok(source.includes("d.precedence"), "Context Center must show resolved instruction precedence");

console.log("context-center-contract: ok");
