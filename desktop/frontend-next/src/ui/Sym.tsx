// The spec draws its own marks on a 16-unit grid rather than pulling an icon
// set in, and says why: "一套 16 格网格、1.5 描边的图形，取代原先混了箭头/带圈
// 字符/箱线符的杂牌 Unicode。只有「你」还是字 —— 人说话用字、机器动作用图形，
// 这个对比是有意留的。" Paths are copied from it verbatim; drawing these with a
// 24-grid library instead is what made every card's gutter read differently.
const PATH: Record<string, string> = {
  "↗": "M5 11 11 5M6.6 5H11v4.4",
  "⑃": "M8 3v3.4M8 6.4 4.6 9.9v3.1M8 6.4l3.4 3.5v3.1",
  "±": "M3.4 5.2h4M5.4 3.2v4M8.6 11h4",
  "?": "M5.9 5.7a2.2 2.2 0 1 1 2.9 2.1c-.5.2-.8.6-.8 1.1v.6M8 12.3h.01",
  "◈": "M8 2.8 13.2 8 8 13.2 2.8 8Z",
  "◇": "M4.6 3h6.8v10L8 10.5 4.6 13Z",
  $: "M4.2 5.2 7.1 8l-2.9 2.8M8.6 11.4h3.6",
  "·": "M4.6 2.9h4.1l2.7 2.8v7.4h-6.8ZM8.5 2.9v2.9h2.9",
  "≡": "M2.9 5.1 4.2 6.4l2.2-2.4M8 5.1h5.2M2.9 10.6l1.3 1.3 2.2-2.4M8 10.6h5.2",
  "⌗": "M5.6 3v10M10.4 3v10M3 5.6h10M3 10.4h10",
  "⊟": "M3.1 4.3a1.2 1.2 0 0 1 1.2-1.2h7.4a1.2 1.2 0 0 1 1.2 1.2v7.4a1.2 1.2 0 0 1-1.2 1.2H4.3a1.2 1.2 0 0 1-1.2-1.2ZM5.7 8.1l1.6 1.6 3.1-3.4",
  "⊙": "M7.2 3.1a4.1 4.1 0 1 0 0 8.2 4.1 4.1 0 0 0 0-8.2M10.3 10.3 13 13",
  "⊘": "M3 8h4.1M7.1 8 5.3 6.2M7.1 8l-1.8 1.8M13 8H8.9M8.9 8l1.8-1.8M8.9 8l1.8 1.8",
  "✓": "M3.6 8.3 6.7 11.4 12.4 5",
  "◦": "M8 5.6a2.4 2.4 0 1 0 0 4.8 2.4 2.4 0 0 0 0-4.8",
  "◎": "M8 3.1a4.9 4.9 0 1 0 0 9.8 4.9 4.9 0 0 0 0-9.8M8 6.2a1.8 1.8 0 1 0 0 3.6 1.8 1.8 0 0 0 0-3.6",
  "⊛": "M8 2.6 12.5 4.3v4c0 2.6-1.9 4.3-4.5 5-2.6-.7-4.5-2.4-4.5-5v-4ZM6.2 8.1l1.4 1.4 2.4-2.7",
  // Nothing else in the vocabulary means "stopped": ⊘ is two arrows meeting,
  // which is compression.
  "⊗": "M8 3.1a4.9 4.9 0 1 0 0 9.8 4.9 4.9 0 0 0 0-9.8M5.5 5.5l5 5",
};

// Which mark a tool gets, taken from the spec's own fixture. The write family
// beyond multi_edit is inferred from the same vocabulary — ± is a plus over a
// minus, which is what an edit is.
const BY_TOOL: Record<string, string> = {
  todo_write: "≡", submit_plan: "≡",
  code_index: "⌗",
  read_file: "·", grep: "·", glob: "·", ls: "·", lsp_diagnostics: "·",
  web_search: "↗", web_fetch: "↗",
  bash: "$", bash_output: "$", kill_shell: "$", wait: "$",
  use_capability: "◈",
  update_goal: "◎",
  complete_step: "⊟", complete_subtask: "⊟",
  remember: "◇",
  edit_file: "±", write_file: "±", multi_edit: "±", notebook_edit: "±",
  move_file: "±", delete_range: "±", delete_symbol: "±",
  review_report: "⊙",
  task: "⑃", fleet: "⑃", read_only_task: "⑃", read_subagent_result: "⑃",
  ask: "?", await_user: "?",
  compress: "⊘",
  guardian_assessment: "⊛",
  // Reached through use_capability like the rest. A delegation, a memory write
  // or an install falls through to the read mark without an entry here.
  parallel_tasks: "⑃", read_only_skill: "⑃", run_skill: "⑃", list_subagents: "⑃",
  install_skill: "±",
  forget: "◇",
  recall: "⊙", memory: "⊙", history: "⊙", list_sessions: "⊙",
  read_session: "·", read_skill: "·", docs: "·",
  slash_command: "≡", install_source: "±",
  conclude_no_changes: "⊟", conclude_blocked: "⊗",
};

// The fallback is a neutral dot, not "·": that one draws a page with a folded
// corner, so an unmapped tool claimed to have read a file.
export const glyphFor = (tool: string) => BY_TOOL[tool] ?? (tool.startsWith("mcp__") ? "◈" : "◦");

// The one deliberate exception: a person's turn stays a character.
export function Sym({ glyph, done }: { glyph: string; done?: boolean }) {
  const d = PATH[glyph];
  return (
    <span className="sym" data-done={done ? "" : undefined}>
      {d ? (
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d={d} />
        </svg>
      ) : (
        glyph
      )}
    </span>
  );
}
