import { workspaceBasename as basename, workspaceParentPath as parentPath } from "../lib/workspacePanelFormat";
import { Tooltip } from "./Tooltip";

export interface WorkspacePathBreadcrumb {
  label: string;
  full: string;
}

export function buildWorkspacePathBreadcrumbs(cwd: string | undefined, path: string): WorkspacePathBreadcrumb[] {
  if (!path) return [];
  const root = basename(cwd ?? "");
  const dirParts = parentPath(path).split("/").filter(Boolean);
  const dirCrumbs: WorkspacePathBreadcrumb[] = [];
  let currentPath = "";
  for (const part of dirParts) {
    currentPath += `${currentPath ? "/" : ""}${part}`;
    dirCrumbs.push({ label: part, full: `${cwd ?? ""}/${currentPath}` });
  }
  const crumbs = [{ label: root, full: cwd ?? "" }];
  if (dirParts.length > 0 && dirParts[0] !== root) crumbs.push(...dirCrumbs);
  return crumbs;
}

export function WorkspacePathBreadcrumbs({ crumbs }: { crumbs: WorkspacePathBreadcrumb[] }) {
  if (crumbs.length === 0) return null;
  return (
    <span className="workspace-current-file__path">
      {crumbs.map((crumb, index) => (
        <span key={index} className="workspace-current-file__crumb">
          {index > 0 && <span className="workspace-current-file__crumb-sep" aria-hidden="true">›</span>}
          <Tooltip label={crumb.full}>
            <span>{crumb.label}</span>
          </Tooltip>
        </span>
      ))}
    </span>
  );
}
