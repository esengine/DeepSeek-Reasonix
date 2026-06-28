import { languageForToolArgs, subjectOf, type ToolFileDiff } from "./tools";
import type { Item } from "./useController";

export interface TurnChangePatch {
  id: string;
  tool: string;
  diff: ToolFileDiff;
  language: string;
}

export interface TurnChangedFile {
  path: string;
  added: number;
  removed: number;
  patches: TurnChangePatch[];
}

export interface TurnChangeSummary {
  added: number;
  removed: number;
  files: TurnChangedFile[];
}

function cleanDiffPath(path: string): string {
  const trimmed = path.trim().split(/\t/, 1)[0]?.trim() ?? "";
  if (!trimmed || trimmed === "/dev/null") return "";
  return trimmed.replace(/^[ab]\//, "");
}

function pathFromUnifiedDiff(diff: string): string {
  let deletedPath = "";
  for (const line of diff.split(/\r?\n/)) {
    if (line.startsWith("+++ ")) {
      const path = cleanDiffPath(line.slice(4));
      if (path) return path;
    }
    if (line.startsWith("--- ")) {
      deletedPath = cleanDiffPath(line.slice(4)) || deletedPath;
    }
  }
  return deletedPath;
}

function pathForTool(item: Extract<Item, { kind: "tool" }>): string {
  const fromArgs = item.args ? subjectOf(item.name, item.args).trim() : "";
  const subject = (fromArgs || item.subject || pathFromUnifiedDiff(item.fileDiff?.diff ?? "")).trim();
  if (!subject) return item.name;
  const arrow = subject.lastIndexOf(" -> ");
  return arrow >= 0 ? subject.slice(arrow + 4).trim() || subject : subject;
}

function hasDiff(diff: ToolFileDiff | undefined): diff is ToolFileDiff {
  return Boolean(diff && (diff.diff.trim() !== "" || diff.added > 0 || diff.removed > 0));
}

export function summarizeTurnChanges(items: readonly Item[], start = 0, end = items.length): TurnChangeSummary | undefined {
  const files = new Map<string, TurnChangedFile>();
  let added = 0;
  let removed = 0;

  for (let i = Math.max(0, start); i < end && i < items.length; i += 1) {
    const item = items[i];
    if (item.kind !== "tool" || item.status !== "done" || !hasDiff(item.fileDiff)) continue;
    const path = pathForTool(item);
    let file = files.get(path);
    if (!file) {
      file = { path, added: 0, removed: 0, patches: [] };
      files.set(path, file);
    }
    file.added += item.fileDiff.added;
    file.removed += item.fileDiff.removed;
    file.patches.push({
      id: `${item.id}-${file.patches.length}`,
      tool: item.name,
      diff: item.fileDiff,
      language: languageForToolArgs(item.args),
    });
    added += item.fileDiff.added;
    removed += item.fileDiff.removed;
  }

  if (files.size === 0) return undefined;
  return { added, removed, files: Array.from(files.values()) };
}
