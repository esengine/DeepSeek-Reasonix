// exportSession renders a transcript as Markdown suitable for sharing
// (GitHub issue, blog post, internal wiki). The shape matches what a
// reader would expect: a title and metadata header, then alternating
// "## User" and "## Assistant" sections, with tool calls rendered as
// collapsible-ish <details> blocks. Code blocks keep their language
// fences so pasting into GitHub preserves syntax highlighting.
//
// The function is intentionally pure: it takes the items array and
// optional metadata, returns a string. No DOM, no bridge, no side
// effects. The caller (App / a new ExportMenu component) decides
// where the result goes — copy to clipboard, save via a Wails binding,
// or pipe into a hidden iframe for a print-stylesheet PDF export.

import type { Item } from "./useController";

export interface ExportMeta {
  // model is the model label shown in the header. Optional.
  model?: string;
  // cwd is the workspace folder; falls back to "unknown".
  cwd?: string;
  // startedAt / endedAt are ISO strings; the header renders a human
  // duration. Both optional.
  startedAt?: string;
  endedAt?: string;
  // title overrides the H1; defaults to "Reasonix session".
  title?: string;
}

export function exportToMarkdown(items: Item[], meta: ExportMeta = {}): string {
  const out: string[] = [];
  const title = meta.title ?? "Reasonix session";
  out.push(`# ${title}`);
  out.push("");
  // Metadata block. Each line is a single bullet so a downstream tool
  // (blog generator, internal wiki) can parse it without a custom
  // frontmatter reader.
  const metaLines: string[] = [];
  if (meta.model) metaLines.push(`- model: ${meta.model}`);
  if (meta.cwd) metaLines.push(`- workspace: \`${meta.cwd}\``);
  if (meta.startedAt) metaLines.push(`- started: ${meta.startedAt}`);
  if (meta.endedAt) metaLines.push(`- ended: ${meta.endedAt}`);
  if (meta.startedAt && meta.endedAt) {
    const ms = Date.parse(meta.endedAt) - Date.parse(meta.startedAt);
    if (Number.isFinite(ms) && ms > 0) metaLines.push(`- duration: ${formatDuration(ms)}`);
  }
  if (metaLines.length > 0) out.push(...metaLines, "");

  // Walk the items. Tool calls with a parentId are sub-agent calls; they
  // get rendered under the parent so the markdown reads as a tree, not a
  // flat list. (The Transcript component does the same.)
  const subcalls = new Map<string, Extract<Item, { kind: "tool" }>[]>();
  for (const it of items) {
    if (it.kind === "tool" && it.parentId) {
      const arr = subcalls.get(it.parentId) ?? [];
      arr.push(it);
      subcalls.set(it.parentId, arr);
    }
  }
  for (const it of items) {
    switch (it.kind) {
      case "user":
        out.push("## User", "", it.text, "");
        break;
      case "assistant":
        out.push("## Assistant", "", it.text || "", "");
        break;
      case "tool":
        if (it.parentId) break; // rendered under its parent
        if (it.name === "todo_write") break; // live task list, not part of the export
        if (it.name === "exit_plan_mode") break; // the plan was the approval card
        out.push(...renderTool(it, subcalls.get(it.id)), "");
        break;
      case "phase":
        out.push(`*${it.text}*`, "");
        break;
      case "notice":
        // Notices are ephemeral; skip in the export.
        break;
      case "compaction":
        if (!it.pending) {
          out.push(
            `> Context compacted — ${it.messages} messages, trigger ${it.trigger}.`,
            "",
            "<details><summary>summary</summary>",
            "",
            it.summary,
            "",
            "</details>",
            "",
          );
        }
        break;
    }
  }
  return out.join("\n");
}

function renderTool(
  it: Extract<Item, { kind: "tool" }>,
  subs: Extract<Item, { kind: "tool" }>[] | undefined,
): string[] {
  const out: string[] = [];
  out.push(`### \`${it.name}\``);
  // The args string is a JSON blob for most tools; render it in a JSON
  // code fence so a reader can copy it back into a request. (We don't
  // try to render a pretty form — different tools have wildly different
  // arg shapes, and a JSON fence round-trips.)
  if (it.args && it.args.trim() && it.args.trim() !== "{}") {
    out.push("", "```json", it.args, "```");
  }
  if (it.output) {
    const truncated = it.truncated ? "\n…(output truncated)" : "";
    out.push("", "**output**", "", "```", it.output, "```" + truncated);
  }
  if (it.error) {
    out.push("", "**error**", "", "```", it.error, "```");
  }
  if (subs && subs.length > 0) {
    out.push("", "**sub-calls**");
    for (const s of subs) {
      out.push(...renderTool(s, undefined));
    }
  }
  return out;
}

function formatDuration(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const rs = s % 60;
  if (m < 60) return rs === 0 ? `${m}m` : `${m}m ${rs}s`;
  const h = Math.floor(m / 60);
  const rm = m % 60;
  return rm === 0 ? `${h}h` : `${h}h ${rm}m`;
}

// exportFilename is a tiny helper that derives a filesystem-friendly
// timestamp slug for the default save name: "reasonix-2026-06-04-1530.md".
// Local time (not UTC) matches the user's wall clock and is fine for
// this purpose — files within a session are minutes apart at most.
export function exportFilename(prefix = "reasonix"): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${prefix}-${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}.md`;
}

// copyToClipboard writes the markdown to the system clipboard. A
// separate function (vs. inlined in the menu) so the same code path
// works for the keyboard shortcut, the menu button, and a future
// "Copy as Markdown" cell action in the history drawer.
export async function copyToClipboard(text: string): Promise<boolean> {
  if (typeof navigator === "undefined" || !navigator.clipboard) return false;
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
