import { useLayoutEffect, useRef } from "react";
import { useT } from "../lib/i18n";
import { writeClipboardText } from "../lib/clipboard";
import { workspaceBasename as basename } from "../lib/workspacePanelFormat";
import { useToast } from "../lib/toast";
import { Tooltip } from "./Tooltip";

export interface WorkspacePathBreadcrumb {
  label: string;
  full: string;
  /** Project-relative path this crumb stands for (e.g. "Project/src"). */
  relative: string;
}

export function buildWorkspacePathBreadcrumbs(cwd: string | undefined, path: string): WorkspacePathBreadcrumb[] {
  if (!path) return [];
  const root = basename(cwd ?? "");
  const parts = path.split("/").filter(Boolean);
  const crumbs: WorkspacePathBreadcrumb[] = [];
  let currentPath = "";
  for (const part of parts) {
    currentPath += `${currentPath ? "/" : ""}${part}`;
    crumbs.push({
      label: part,
      full: `${cwd ?? ""}/${currentPath}`,
      relative: `${root}/${currentPath}`,
    });
  }
  // Prepend the project root unless the path already starts with it, so the
  // breadcrumb reads "Project › dir › file" (the last segment is the file
  // name, matching the copyable project-relative path).
  if (parts[0] !== root) crumbs.unshift({ label: root, full: cwd ?? "", relative: root });
  return crumbs;
}

export function WorkspacePathBreadcrumbs({ crumbs }: { crumbs: WorkspacePathBreadcrumb[] }) {
  const t = useT();
  const { showToast } = useToast();
  const pathRef = useRef<HTMLSpanElement>(null);
  // Long paths overflow horizontally (scrollbar hidden); the right end (file
  // name) is the priority, so snap there before first paint and again after
  // async fonts/layout settle (rAF + fonts.ready). Only re-snaps when the
  // crumbs actually change, so manual scrolling is not fought.
  // Alignment uses the last crumb's box, not el.scrollWidth: WebKit computes
  // scrollWidth wrong for flex containers with overflow-x (it can omit part
  // of the flex content), which would leave the file-name suffix cut off.
  const crumbKey = crumbs.map((crumb) => crumb.label).join("/");
  useLayoutEffect(() => {
    const el = pathRef.current;
    if (!el) return;
    const alignRight = () => {
      const last = el.lastElementChild as HTMLElement | null;
      if (!last) return;
      const containerLeft = el.getBoundingClientRect().left;
      const lastRight = last.getBoundingClientRect().right;
      el.scrollLeft = Math.max(0, lastRight - containerLeft - el.clientWidth);
    };
    alignRight();
    const raf = requestAnimationFrame(alignRight);
    let disposed = false;
    void document.fonts?.ready?.then(() => {
      if (!disposed) alignRight();
    });
    return () => {
      disposed = true;
      cancelAnimationFrame(raf);
    };
  }, [crumbKey]);
  if (crumbs.length === 0) return null;
  const copyCrumb = (crumb: WorkspacePathBreadcrumb) => {
    void writeClipboardText(crumb.relative);
    showToast(t("diag.copied"), "info");
  };
  return (
    <span ref={pathRef} className="workspace-current-file__path">
      {crumbs.map((crumb, index) => (
        <span
          key={index}
          role="button"
          tabIndex={0}
          className="workspace-current-file__crumb"
          title={`${t("workspace.copyRelativePath")}: ${crumb.relative}`}
          onClick={() => copyCrumb(crumb)}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              copyCrumb(crumb);
            }
          }}
        >
          {index > 0 && <span className="workspace-current-file__crumb-sep" aria-hidden="true">›</span>}
          <Tooltip label={crumb.full}>
            <span>{crumb.label}</span>
          </Tooltip>
        </span>
      ))}
    </span>
  );
}
