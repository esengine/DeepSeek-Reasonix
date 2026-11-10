import { HttpError } from "../port/http_error";
import type { AgentPort } from "../port/port";
import { patchSource, type PatchFailure, type SourceEdit, type Target } from "./htmlsource";

export type PageEditFailure = PatchFailure | "outside" | "conflict" | "failed";

export type PageEditOutcome = { saved: true; path: string } | { saved: false; code: PageEditFailure };

/** Writes edits made on a page back to the file the page was opened from. */
export type PageEditor = (diskPath: string, target: Target, edits: SourceEdit[]) => Promise<PageEditOutcome>;

const flat = (path: string) => path.replaceAll("\\", "/");

/** The workspace-relative path of a disk path inside the workspace, or "". */
export function insideWorkspace(root: string, disk: string): string {
  const base = flat(root).replace(/\/+$/, "");
  const path = flat(disk);
  const windows = /^[A-Za-z]:/.test(base);
  const head = `${base}/`;
  const inside = windows ? path.toLowerCase().startsWith(head.toLowerCase()) : path.startsWith(head);
  if (!inside) return "";
  const rest = path.slice(head.length);
  return rest && !rest.split("/").some((part) => part === ".." || part === "") ? rest : "";
}

export function pageEditor(port: AgentPort): PageEditor {
  return async (disk, target, edits) => {
    try {
      const root = (await port.workspaces()).current;
      const path = root ? insideWorkspace(root, disk) : "";
      if (!path) return { saved: false, code: "outside" };
      const file = await port.workspaceFile(path);
      let content = file.content;
      for (const edit of edits) {
        const next = patchSource(content, target, edit);
        if (!next.ok) return { saved: false, code: next.code };
        content = next.content;
      }
      if (content === file.content) return { saved: true, path };
      await port.saveWorkspaceFile({ ...file, content });
      return { saved: true, path };
    } catch (e) {
      const code = e instanceof HttpError ? e.reason?.code : "";
      return { saved: false, code: code === "workspace.file_changed" ? "conflict" : "failed" };
    }
  };
}
