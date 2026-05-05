/** Applies an MCP append-drift mid-session: registers each new tool in the loop's registry + prefix, and returns an updated summary. Immutable — does not mutate the input `target`. */

import type { CacheFirstLoop } from "../../loop.js";
import { registerSingleMcpTool } from "../../mcp/registry.js";
import type { McpTool } from "../../mcp/types.js";
import type { JSONSchema, ToolSpec } from "../../types.js";
import type { McpServerSummary } from "./slash/types.js";

export function applyMcpAppend(
  loop: CacheFirstLoop,
  target: McpServerSummary,
  addedTools: McpTool[],
): McpServerSummary {
  const accepted: McpTool[] = [];
  for (const mcpTool of addedTools) {
    if (!mcpTool.name) continue;
    const registeredName = registerSingleMcpTool(mcpTool, target.bridgeEnv);
    if (!registeredName) continue;
    const spec: ToolSpec = {
      type: "function",
      function: {
        name: registeredName,
        description: mcpTool.description ?? "",
        parameters: mcpTool.inputSchema as unknown as JSONSchema,
      },
    };
    loop.prefix.addTool(spec);
    accepted.push(mcpTool);
  }
  if (accepted.length === 0 || !target.report.tools.supported) return target;

  const merged = [...target.report.tools.items, ...accepted];
  return {
    ...target,
    toolCount: merged.length,
    report: {
      ...target.report,
      tools: {
        supported: true as const,
        items: merged,
      },
    },
  };
}
