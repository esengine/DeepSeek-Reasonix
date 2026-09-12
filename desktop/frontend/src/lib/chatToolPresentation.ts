import type { Item } from "./useController";

export type ToolItem = Extract<Item, { kind: "tool" }>;
export type ToolPresentationKind = "search" | "web" | "shell" | "agent" | "file" | "present" | "tool";

const AGENT_TOOLS = new Set(["task", "read_only_task", "parallel_tasks", "fleet", "subagent"]);
const FILE_TOOLS = new Set([
  "read_file", "glob", "grep", "ls", "code_index",
  "write_file", "edit_file", "multi_edit", "move_file", "notebook_edit", "delete_range", "delete_symbol",
]);

/**
 * Resolve a renderer from trusted built-in identities. A plugin whose display
 * name happens to contain "read" or "write" must remain a generic tool.
 */
export function classifyTool(item: ToolItem): ToolPresentationKind {
  if (item.name === "present") return "present";
  if (item.name === "web_search") return "search";
  if (item.name === "web_fetch") return "web";
  if (item.name === "bash" || item.isShell) return "shell";
  if (AGENT_TOOLS.has(item.name)) return "agent";
  if (FILE_TOOLS.has(item.name)) return "file";
  return "tool";
}

export function shellDisplayName(item: ToolItem): string {
  const shell = item.execution?.shell?.trim().toLowerCase();
  if (shell === "powershell" || shell === "pwsh") return "PowerShell";
  if (shell === "git-bash") return "Git Bash";
  if (shell === "bash") return "Bash";
  if (shell === "zsh") return "Zsh";
  if (shell === "sh") return "Shell";
  return "Terminal";
}
