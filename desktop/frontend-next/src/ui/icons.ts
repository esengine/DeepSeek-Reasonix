import { t } from "../i18n";

// The spec names a call by what it is — Search, Update, Read — and derives the
// running line from its category, so a new tool needs no new copy. The raw id
// is still on the row, in the tag beside the name.
const LABEL: Record<string, string> = {
  web_search: "common.web_search", web_fetch: "chat.fetch_page", task: "chat.delegate_task", bash: "chat.run_command",
  bash_output: "chat.command_output", kill_shell: "chat.stop_command", wait: "chat.wait",
  read_file: "chat.read_file", grep: "chat.search_code", glob: "safety.finding_files", ls: "chat.list_directory",
  edit_file: "chat.edit_file", write_file: "chat.write_file", multi_edit: "chat.edit_files",
  todo_write: "chat.update_plan", remember: "chat.save_memory", use_capability: "chat.call_mcp",
  code_index: "chat.build_index", complete_step: "chat.complete_step", review_report: "chat.review_results",
  update_goal: "chat.update_goal", guardian_assessment: "chat.safety_check", compress: "chat.compact_context",
  // Checked against the kernel's registered names, not guessed: these all ship
  // and were rendering as their raw id under a default icon.
  delete_range: "chat.delete_content", delete_symbol: "chat.delete_symbol", move_file: "chat.move_file",
  notebook_edit: "chat.edit_notebook", submit_plan: "chat.submit_plan", ask: "chat.ask_user",
  fleet: "chat.parallel_tasks", read_only_task: "chat.read_only_task", read_subagent_result: "chat.read_subtask_result",
  complete_subtask: "chat.complete_subtask", lsp_diagnostics: "chat.code_diagnostics",
  lsp_definition: "Definition", lsp_references: "References", lsp_hover: "Hover",
  parallel_tasks: "Tasks", list_subagents: "Subagents",
  run_skill: "Skill", read_only_skill: "Skill", read_skill: "Playbook", install_skill: "Install",
  recall: "Recall", memory: "Memory", forget: "Forget",
  history: "History", list_sessions: "Sessions", read_session: "Session",
  docs: "Docs", context_budget: "Budget", slash_command: "Command", install_source: "Install",
  await_user: "Await", conclude_blocked: "Blocked", conclude_no_changes: "No Changes",
};

const RUNNING: Record<string, string> = {
  plan: "正在写计划…", read: "common.loading", net: "正在联网…", deleg: "正在派活…",
  bash: "正在执行…", write: "正在改写…", mem: "正在写入记忆…", mcp: "正在调 MCP…",
};

// The name slot is a word a person reads, never an identifier: the spec renders
// it at 12.5px/500 in the UI face, where raw snake_case reads as the wrong font
// rather than as a name. A capability call is named for what it is — the id it
// resolved to belongs in the mono tag beside it — and anything else with no
// entry here is title-cased rather than passed through.
export const labelFor = (tool: string) =>
  LABEL[tool] ? t(LABEL[tool]) : (isCapability(tool) ? t("chat.call_mcp") : titleCase(tool));

export const isCapability = (tool: string) => tool === "use_capability" || tool.startsWith("mcp__");

const titleCase = (id: string) =>
  id
    .split(/[_\-.]+/)
    .filter(Boolean)
    .map((w) => w[0].toUpperCase() + w.slice(1))
    .join(" ") || id;

export function runLabelFor(tool: string) {
  return t(RUNNING[categoryOf(tool)] ?? "common.working");
}

// A tool that changes the tree has to read as one: the neutral bucket drops the
// only warning the row carries. The two installs write what later turns run.
const WRITE = new Set(["delete_range", "delete_symbol", "move_file", "notebook_edit", "install_skill", "install_source"]);
// What this turn hands to a sub-agent, plus the calls that read one back: the
// colour is the row's only sign that the work left this context.
const DELEG = new Set([
  "task", "fleet", "read_only_task", "read_subagent_result", "complete_subtask",
  "parallel_tasks", "run_skill", "read_only_skill", "list_subagents",
]);
const READ = new Set([
  "read_file", "grep", "glob", "ls", "code_index", "read_skill",
  "lsp_diagnostics", "lsp_definition", "lsp_references", "lsp_hover",
  "history", "list_sessions", "read_session", "docs", "context_budget", "recall", "memory",
]);

// MCP tools are registered as mcp__<server>__<tool>. Which server answered is
// the one thing a raw id hides and the user needs: it is the difference between
// the agent reading your disk and an external service doing it.
export function mcpOrigin(tool: string): { server: string; tool: string } | null {
  if (!tool.startsWith("mcp__")) return null;
  const at = tool.indexOf("__", 5);
  if (at < 0) return null;
  return { server: tool.slice(5, at), tool: tool.slice(at + 2) };
}

export function categoryOf(tool: string): string {
  if (tool === "web_search" || tool === "web_fetch") return "net";
  if (DELEG.has(tool)) return "deleg";
  if (WRITE.has(tool) || tool.startsWith("edit") || tool.startsWith("write") || tool.startsWith("multi")) return "write";
  if (tool === "use_capability" || tool.startsWith("mcp__")) return "mcp";
  // Saving a fact and deleting one are the same class of change, and neither
  // touches the tree a write colour warns about.
  if (tool === "remember" || tool === "forget") return "mem";
  if (tool === "todo_write" || tool === "submit_plan") return "plan";
  if (tool === "bash" || tool.startsWith("bash_") || tool === "kill_shell") return "bash";
  if (READ.has(tool)) return "read";
  return "sys";
}
