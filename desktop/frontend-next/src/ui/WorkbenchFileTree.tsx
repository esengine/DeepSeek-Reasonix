import { useMemo, useState, type MouseEvent } from "react";
import { t } from "../i18n";
import type { WorkspaceChange } from "../port/port";
import { workspaceDrag } from "./filedrag";
import { fileKind } from "./fileKind";
import { StudioIcon } from "./StudioIcon";
import type { TreeRow } from "./workbench_tree";

/** The workspace's files as a tree, each changed one marked with its git status. */
export function WorkbenchFileTree({ rows, collapsed, workspaceRoot, changes, current, onFolder, onFile, onMenu }: {
  rows: TreeRow[];
  collapsed: Set<string>;
  workspaceRoot?: string;
  changes: WorkspaceChange[];
  current: string;
  onFolder: (path: string) => void;
  onFile: (path: string) => void;
  onMenu: (e: MouseEvent<HTMLElement>, path: string, folder: boolean) => void;
}) {
  const status = useMemo(() => new Map(changes.map((c) => [c.path, c.status])), [changes]);
  const [folded, setFolded] = useState(false);
  return (
    <>
    <div className="workbench-explorer-head workbench-files-head">
      <button type="button" className="workbench-section-fold" data-action="workbench.section-fold" data-target="files" aria-expanded={!folded}
        aria-label={t(folded ? "workbench.expand_section" : "workbench.collapse_section", { name: t("common.files") })} onClick={() => setFolded(!folded)}>
        <StudioIcon name={folded ? "chevron" : "down"} />
      </button>
      <span>{t("common.files")}</span>
    </div>
    <div className="workbench-filelist" hidden={folded}>
      {rows.map((row) =>
        row.kind === "folder" ? (
          <button
            className="workbench-tree-row"
            data-action-click="workbench.folder"
            {...workspaceDrag(workspaceRoot, row.path)}
            data-target={row.path}
            key={`d:${row.path}`}
            style={{ paddingInlineStart: 8 + row.depth * 14 }}
            aria-expanded={!collapsed.has(row.path)}
            onClick={() => onFolder(row.path)}
            data-action-contextmenu="workbench.menu"
            onContextMenu={(e) => onMenu(e, row.path, true)}
          >
            <StudioIcon
              name={collapsed.has(row.path) ? "chevron" : "down"}
            />
            <StudioIcon name="folder" />
            <span>{row.name}</span>
          </button>
        ) : (
          <button
            className="workbench-tree-row"
            data-action-click="workbench.file"
            {...workspaceDrag(workspaceRoot, row.path)}
            data-target={row.path}
            key={row.path}
            style={{ paddingInlineStart: 8 + row.depth * 14 }}
            title={row.path}
            aria-current={current === row.path ? "page" : undefined}
            onClick={() => onFile(row.path)}
            data-action-contextmenu="workbench.menu"
            onContextMenu={(e) => onMenu(e, row.path, false)}
          >
            <span />
            <StudioIcon name="file" data-kind={fileKind(row.path) || undefined} />
            <span>{row.name}</span>
            {status.has(row.path) && <em>{status.get(row.path) || "M"}</em>}
          </button>
        ),
      )}
      {!rows.length && (
        <p className="workbench-no-files">{t("workbench.no_matching_files")}</p>
      )}
    </div>
    </>
  );
}
