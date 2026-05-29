import { describe, expect, it } from "vitest";
import { PauseGate } from "../src/core/pause-gate.js";
import { ToolRegistry } from "../src/tools.js";
import { registerWorkflowTool } from "../src/tools/workflow.js";

const script = `export const meta = { name: "confirm", description: "Confirm" }
const result = await agent({ label: "reader", instruction: "read", type: "verify" })
return result
`;

describe("workflow confirmation", () => {
  it("asks for confirmation before full tool mode", async () => {
    const registry = registerWorkflowTool(new ToolRegistry(), {
      runner: { run: async () => ({ ok: true, output: "OK" }) },
    });
    const gate = new PauseGate();
    let seenPayload: unknown;
    gate.on((request) => {
      seenPayload = request.payload;
      gate.resolve(request.id, { type: "run_once" });
    });

    const result = JSON.parse(
      await registry.dispatch(
        "workflow",
        { script, mode: "run", tool_mode: "full", max_agents: 1 },
        { confirmationGate: gate },
      ),
    );

    expect(result.success).toBe(true);
    expect(seenPayload).toMatchObject({
      name: "confirm",
      mode: "run",
      toolMode: "full",
      maxAgents: 1,
    });
  });

  it("returns a denial when the user rejects the workflow", async () => {
    const registry = registerWorkflowTool(new ToolRegistry(), {
      runner: { run: async () => ({ ok: true, output: "OK" }) },
    });
    const gate = new PauseGate();
    gate.on((request) => gate.resolve(request.id, { type: "deny" }));

    const result = JSON.parse(
      await registry.dispatch(
        "workflow",
        { script, mode: "run", tool_mode: "full", max_agents: 1 },
        { confirmationGate: gate },
      ),
    );

    expect(result).toMatchObject({
      success: false,
      rejectedReason: "workflow-confirmation",
    });
  });
});
