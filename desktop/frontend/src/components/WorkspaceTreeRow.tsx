import type { DragEvent, MouseEvent } from "react";
import { ChevronRight, Folder } from "lucide-react";
import type { DirEntry } from "../lib/types";
import { workspaceBasename as basename, workspaceParentPath as parentPath } from "../lib/workspacePanelFormat";
import { WorkspaceFileIcon } from "./WorkspaceFileIcon";

export interface WorkspaceTreeRowData {
  key: string;
  path: string;
  depth: number;
  entry: DirEntry;
  active: boolean;
  isOpen?: boolean;
  isSearch?: boolean;
  compactPaths?: string[];
  displayName?: string;
}

export function WorkspaceTreeRow({
  row,
  onActivate,
  onDragStart,
  onContextMenu,
}: {
  row: WorkspaceTreeRowData;
  onActivate: (row: WorkspaceTreeRowData) => void;
  onDragStart: (event: DragEvent<HTMLElement>, path: string, isDir: boolean) => void;
  onContextMenu: (event: MouseEvent<HTMLElement>, path: string, isDir: boolean) => void;
}) {
  const { path, depth, entry, isOpen, active, displayName = entry.name } = row;
  if (row.isSearch) {
    const dir = parentPath(path);
    return (
      <button
        className={`workspace-tree__row workspace-tree__row--search${active ? " workspace-tree__row--active" : ""}`}
        data-workspace-path={path}
        draggable
        onDragStart={(event) => onDragStart(event, path, entry.isDir)}
        onClick={() => onActivate(row)}
        onContextMenu={(event) => onContextMenu(event, path, entry.isDir)}
      >
        {entry.isDir ? (
          <Folder size={14} className="workspace-tree__icon workspace-tree__icon--dir" />
        ) : (
          <WorkspaceFileIcon fileName={entry.name} />
        )}
        <span className="workspace-tree__result">
          <span className="workspace-tree__result-name">{basename(path)}</span>
          {dir && <span className="workspace-tree__result-dir">{dir}</span>}
        </span>
      </button>
    );
  }

  return (
    <button
      className={`workspace-tree__row${active ? " workspace-tree__row--active" : ""}`}
      data-workspace-path={path}
      draggable
      onDragStart={(event) => onDragStart(event, path, entry.isDir)}
      onClick={() => onActivate(row)}
      onContextMenu={(event) => onContextMenu(event, path, entry.isDir)}
      style={{ paddingLeft: 8 + depth * 14 }}
    >
      {depth > 0 && (
        <span className="workspace-tree__guides" aria-hidden="true">
          {Array.from({ length: depth }, (_, index) => (
            <span
              className="workspace-tree__guide"
              key={index}
              style={{ left: 14 + index * 14 }}
            />
          ))}
        </span>
      )}
      {entry.isDir ? (
        <ChevronRight
          size={13}
          className={`workspace-tree__chev ${isOpen ? "workspace-tree__chev--open" : ""}`}
          style={{
            transition: "transform 0.15s ease",
            transform: isOpen ? "rotate(90deg)" : "rotate(0deg)",
          }}
        />
      ) : (
        <span className="workspace-tree__chev" />
      )}
      {entry.isDir ? (
        <Folder size={14} className="workspace-tree__icon workspace-tree__icon--dir" />
      ) : (
        <WorkspaceFileIcon fileName={entry.name} />
      )}
      <span className="workspace-tree__name">{displayName}</span>
    </button>
  );
}
