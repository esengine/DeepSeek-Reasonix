// Per-tool presentation helpers. The kernel forwards every tool call the same way
// (name + raw-JSON args + output); these turn that generic payload into the
// recognizable one-liner, inline diff, and collapsed outcome each tool deserves —
// the recognizable "card" vocabulary the desktop uses. Kept pure (no React, no
// highlight.js) so ToolCard stays a renderer and the main bundle stays light.

import { diffLines } from "./diff";
import { t } from "./i18n";
import { extToLang } from "./lang";
import type { DictKey } from "../locales/en";

export interface ToolDiff {
  original: string;
  modified: string;
  lang: string;
  label?: string; // multi_edit labels each step ("edit 1", …)
}

function parse(args: string): Record<string, unknown> {
  try {
    return JSON.parse(args) as Record<string, unknown>;
  } catch {
    return {};
  }
}

function str(a: Record<string, unknown>, key: string): string {
  return typeof a[key] === "string" ? (a[key] as string) : "";
}

// subjectOf pulls the most informative one-liner out of a call's args — the
// command for bash, the pattern for search, the path for file tools, the
// description for a sub-task — so the collapsed row reads at a glance.
export function subjectOf(name: string, args: string): string {
  const a = parse(args);
  switch (name) {
    case "bash":
      return str(a, "command");
    case "grep":
    case "glob":
      return str(a, "pattern") || str(a, "path");
    case "web_fetch":
      return str(a, "url");
    case "task":
      return str(a, "description") || str(a, "prompt");
    case "move_file": {
      const src = str(a, "source_path");
      const dst = str(a, "destination_path");
      return src && dst ? `${src} -> ${dst}` : src || dst;
    }
    case "remember":
      return str(a, "name") || str(a, "description");
    case "todo_write":
    case "exit_plan_mode":
      return ""; // these get dedicated cards, not a subject line
    default:
      return str(a, "path") || str(a, "file_path");
  }
}

// diffsFor returns the before/after pairs a writer tool's card renders inline:
// edit_file is one pair, write_file is an all-add (empty original), multi_edit is
// one pair per step. Returns [] for non-writers, so the card folds args/output
// away instead.
export function diffsFor(name: string, args: string): ToolDiff[] {
  const a = parse(args);
  const lang = extToLang(str(a, "path") || str(a, "file_path"));
  if (name === "edit_file") {
    if (typeof a.old_string === "string" && typeof a.new_string === "string") {
      return [{ original: a.old_string, modified: a.new_string, lang }];
    }
  }
  if (name === "write_file" && typeof a.content === "string") {
    return [{ original: "", modified: a.content, lang }];
  }
  if (name === "multi_edit" && Array.isArray(a.edits)) {
    const out: ToolDiff[] = [];
    (a.edits as unknown[]).forEach((e, i) => {
      const step = e as Record<string, unknown>;
      if (typeof step?.old_string === "string" && typeof step?.new_string === "string") {
        out.push({ original: step.old_string, modified: step.new_string, lang, label: `edit ${i + 1}` });
      }
    });
    return out;
  }
  return [];
}

export type TodoStatus = "pending" | "in_progress" | "completed";

export interface Todo {
  content: string;
  status: TodoStatus | string;
  activeForm?: string;
  level?: number; // 0 = phase, 1 = sub-step of the phase above it
}

// parseTodos pulls the task list out of a todo_write call's args.
export function parseTodos(args: string): Todo[] {
  try {
    const a = JSON.parse(args) as { todos?: Todo[] };
    return Array.isArray(a.todos) ? a.todos : [];
  } catch {
    return [];
  }
}

function plusMinus(original: string, modified: string): { add: number; del: number } {
  let add = 0;
  let del = 0;
  for (const r of diffLines(original, modified)) {
    if (r.type === "add") add++;
    else if (r.type === "del") del++;
  }
  return { add, del };
}

// lineCount counts lines, ignoring a single trailing newline so "a\n" reads as 1.
function lineCount(s: string): number {
  if (!s) return 0;
  const t = s.endsWith("\n") ? s.slice(0, -1) : s;
  return t === "" ? 0 : t.split("\n").length;
}

function nonEmptyLines(s: string): number {
  return s.split("\n").filter((l) => l.trim() !== "").length;
}

// countOf renders a localized "N <noun>" using the singular/plural key pair (zh
// collapses both to one form). Lives here, not the dict, so the counted phrasing
// stays a translation concern.
function countOf(n: number, one: DictKey, other: DictKey): string {
  return t(n === 1 ? one : other, { n });
}

// summarize derives the one-line outcome shown under a finished card (the "⎿"
// secondary line) — counts from the args for writers, from the output for
// readers. "" means there's nothing worth a summary line.
export function summarize(name: string, args: string, output?: string, error?: string): string {
  if (error) return "";
  const a = parse(args);
  switch (name) {
    case "write_file":
      return countOf(lineCount(str(a, "content")), "tool.lineOne", "tool.lineOther");
    case "edit_file": {
      if (typeof a.old_string === "string" && typeof a.new_string === "string") {
        const { add, del } = plusMinus(a.old_string, a.new_string);
        return `+${add} -${del}`;
      }
      return "";
    }
    case "multi_edit": {
      const edits = Array.isArray(a.edits) ? (a.edits as Record<string, unknown>[]) : [];
      let add = 0;
      let del = 0;
      for (const e of edits) {
        if (typeof e?.old_string === "string" && typeof e?.new_string === "string") {
          const pm = plusMinus(e.old_string, e.new_string);
          add += pm.add;
          del += pm.del;
        }
      }
      return `${countOf(edits.length, "tool.editOne", "tool.editOther")} · +${add} -${del}`;
    }
  }

  if (!output) return "";
  switch (name) {
    case "read_file": {
      if (output.startsWith("(empty file)")) return t("tool.emptyFile");
      const arrows = (output.match(/→/g) || []).length;
      return countOf(arrows || lineCount(output), "tool.lineOne", "tool.lineOther");
    }
    case "grep":
      return countOf(nonEmptyLines(output), "tool.matchOne", "tool.matchOther");
    case "glob":
      return countOf(nonEmptyLines(output), "tool.fileOne", "tool.fileOther");
    case "ls":
      return countOf(nonEmptyLines(output), "tool.entryOne", "tool.entryOther");
    case "web_fetch":
      return output.split("\n", 1)[0].slice(0, 80);
    case "bash":
      return output.trim() === "" ? t("tool.noOutput") : countOf(lineCount(output), "tool.lineOne", "tool.lineOther");
    default:
      return "";
  }
}
