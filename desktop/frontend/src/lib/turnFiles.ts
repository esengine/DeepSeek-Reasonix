import type { Item } from "./useController";

type ToolItem = Extract<Item, { kind: "tool" }>;
export type TurnFileOperation = "written" | "modified";
export interface TurnFileView {
  path: string;
  toolCallId: string;
  operation: TurnFileOperation;
}

const MUTATION_PATHS: Record<string, { field: string; operation: TurnFileOperation }> = {
  write_file: { field: "path", operation: "written" },
  edit_file: { field: "path", operation: "modified" },
  multi_edit: { field: "path", operation: "modified" },
  notebook_edit: { field: "path", operation: "modified" },
  delete_range: { field: "path", operation: "modified" },
  delete_symbol: { field: "path", operation: "modified" },
  move_file: { field: "destination_path", operation: "written" },
};

export function fileIdentity(path: string): string {
  const slash = path.trim().replaceAll("\\", "/");
  const prefix = slash.startsWith("/") ? "/" : "";
  const segments: string[] = [];
  for (const part of slash.split("/")) {
    if (!part || part === ".") continue;
    if (part === ".." && segments.length && segments[segments.length - 1] !== "..") segments.pop();
    else if (part !== ".." || !prefix) segments.push(part);
  }
  return `${prefix}${segments.join("/")}`;
}

/** Derive file facts only from successful native mutation tools. */
export function deriveTurnFiles(calls: readonly ToolItem[]): TurnFileView[] {
  const files = new Map<string, TurnFileView>();
  for (const call of calls) {
    const definition = MUTATION_PATHS[call.name];
    if (!definition || call.status !== "done" || call.error || call.readOnly) continue;
    if (/\bno changes made\b/i.test(call.output ?? "")) continue;
    let args: Record<string, unknown>;
    try {
      const parsed: unknown = JSON.parse(call.args || "{}");
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) continue;
      args = parsed as Record<string, unknown>;
    } catch { continue; }
    const path = args[definition.field];
    if (typeof path !== "string" || !path.trim()) continue;
    const key = fileIdentity(path);
    if (!key) continue;
    const existing = files.get(key);
    const value = { path: path.trim(), toolCallId: call.id, operation: definition.operation };
    if (existing) files.set(key, { ...value, path: existing.path });
    else files.set(key, value);
  }
  return [...files.values()];
}
