import { relative, resolve } from "node:path";
import { type EditBlock, toWholeFileEditBlock } from "../../code/edit-blocks.js";
import type { EditMode } from "../../config.js";
import { looksLikeAbsoluteSystemPath, pathIsUnder } from "../../tools/filesystem.js";

export type ReviewGatedEditTool = "edit_file" | "write_file" | "multi_edit";

export function isReviewGatedEditTool(name: string): name is ReviewGatedEditTool {
  return name === "edit_file" || name === "write_file" || name === "multi_edit";
}

function resolveEditRelPath(rawPath: unknown, rootForEdit: string): string | null {
  if (typeof rawPath !== "string" || rawPath.length === 0) return null;

  const absRoot = resolve(rootForEdit);
  if (looksLikeAbsoluteSystemPath(rawPath)) {
    const abs = resolve(rawPath);
    if (!pathIsUnder(abs, absRoot)) return null;
    const rel = relative(absRoot, abs);
    return rel || null;
  }

  let stripped = rawPath;
  while (stripped.startsWith("/") || stripped.startsWith("\\")) {
    stripped = stripped.slice(1);
  }
  return stripped || null;
}

export function buildEditToolBlocks(
  name: string,
  args: Record<string, unknown>,
  rootForEdit: string,
): EditBlock[] | null {
  if (!isReviewGatedEditTool(name)) return null;

  if (name === "multi_edit") {
    const edits = args.edits;
    if (!Array.isArray(edits) || edits.length === 0) return null;
    const blocks: EditBlock[] = [];
    for (const item of edits) {
      if (!item || typeof item !== "object") return null;
      const edit = item as Record<string, unknown>;
      const relPath = resolveEditRelPath(edit.path, rootForEdit);
      if (!relPath || typeof edit.search !== "string" || typeof edit.replace !== "string") {
        return null;
      }
      if (edit.search.length === 0) return null;
      blocks.push({
        path: relPath,
        search: edit.search,
        replace: edit.replace,
        offset: 0,
      });
    }
    return blocks;
  }

  const relPath = resolveEditRelPath(args.path, rootForEdit);
  if (!relPath) return null;

  if (name === "edit_file") {
    const search = typeof args.search === "string" ? args.search : "";
    const replace = typeof args.replace === "string" ? args.replace : "";
    if (!search) return null;
    return [{ path: relPath, search, replace, offset: 0 }];
  }

  const content = typeof args.content === "string" ? args.content : "";
  return [toWholeFileEditBlock(relPath, content, rootForEdit)];
}

export function shouldApplyEditToolImmediately(
  editMode: EditMode,
  turnEditPolicy: "ask" | "apply-all",
): boolean {
  return editMode === "auto" || editMode === "yolo" || turnEditPolicy === "apply-all";
}

export function formatQueuedReviewToolResult(blockCount: number): string {
  const noun = blockCount === 1 ? "edit" : "edits";
  return `Queued ${blockCount} ${noun} for review. No files were changed. Ask the user to run /apply to accept them or /discard to reject them.`;
}
