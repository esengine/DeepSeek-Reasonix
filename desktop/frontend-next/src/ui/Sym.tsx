import { StudioIcon, type StudioIconName } from "./StudioIcon";

const BY_TOOL: Record<string, StudioIconName> = {
  todo_write: "tasks", submit_plan: "tasks", code_index: "fileCode",
  read_file: "file", grep: "search", glob: "fileSearch", ls: "folder", lsp_diagnostics: "activity",
  web_search: "search", web_fetch: "globe",
  bash: "terminal", bash_output: "terminal", kill_shell: "stop", wait: "clock",
  use_capability: "tools", update_goal: "target", complete_step: "complete", complete_subtask: "complete",
  remember: "bookmark", edit_file: "fileEdit", write_file: "fileEdit", multi_edit: "fileEdit",
  notebook_edit: "notebook", move_file: "moveFile", delete_range: "trash", delete_symbol: "trash",
  review_report: "fileSearch", task: "fork", fleet: "network", read_only_task: "fork",
  read_subagent_result: "file", ask: "question", await_user: "question", compress: "collapseAll",
  guardian_assessment: "shield", parallel_tasks: "fork", read_only_skill: "fork",
  run_skill: "play", list_subagents: "list", install_skill: "download", forget: "archive",
  recall: "search", memory: "brain", history: "history", list_sessions: "list",
  read_session: "chat", read_skill: "book", docs: "book", slash_command: "terminal",
  install_source: "download", conclude_no_changes: "complete", conclude_blocked: "fail",
};

export const iconFor = (tool: string): StudioIconName => Object.hasOwn(BY_TOOL, tool) ? BY_TOOL[tool] : tool.startsWith("mcp__") ? "plug" : "tools";

export function Sym({ icon, done }: { icon: StudioIconName; done?: boolean }) {
  return <span className="sym" data-done={done ? "" : undefined}><StudioIcon name={icon} /></span>;
}
