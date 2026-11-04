import { Fragment } from "react";
import type { ReactNode } from "react";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

export type FileMode = "file" | "diff" | "read" | "preview" | "document";

/** The open file's path, the views it can be seen in, and save. A document that
 *  renders (Markdown, a page) leads with that view and calls the editor
 *  "编辑"; any other file has only its text. */
export function WorkbenchFileBar({
  path,
  mode,
  onMode,
  readable,
  previewable,
  canSave,
  readOnlyIn,
  onSave,
  openWith,
  onMore,
  document = false,
  source = true,
}: {
  path: string;
  mode: FileMode;
  onMode: (mode: FileMode) => void;
  readable: boolean;
  previewable: boolean;
  canSave: boolean;
  // Set for a file read from a granted folder: the folder, shown as why it cannot be saved.
  readOnlyIn?: string;
  onSave: () => void;
  openWith?: ReactNode;
  // Opens the file's menu (add to the conversation, copy, reveal) under the "…" button.
  onMore?: (anchor: HTMLElement) => void;
  document?: boolean;
  source?: boolean;
}) {
  const view = (value: FileMode, label: string) => (
    <button data-action="workbench.mode" data-value={value} aria-pressed={mode === value} onClick={() => onMode(value)}>
      {label}
    </button>
  );
  return (
    <div className="workbench-filebar">
      <strong title={path}>
        {path.split("/").slice(0, -1).map((part, i) => <Fragment key={i}><span>{part}</span><i aria-hidden="true">/</i></Fragment>)}
        <b>{path.split("/").at(-1)}</b>
      </strong>
      <div role="group">
        {readable && view("read", t("workbench.read"))}
        {document && view("document", t("common.preview"))}
        {previewable && view("preview", t("workbench.run_interactively"))}
        {source && view("file", !readOnlyIn && (readable || previewable || document) ? t("common.edit") : t("common.files"))}
        {source && !readOnlyIn && view("diff", "Diff")}
      </div>
      {openWith}
      {readOnlyIn ? (
        <span className="workbench-readonly" title={readOnlyIn}>{t("workbench.read_only_from_a_folder")}</span>
      ) : source ? (
        <button className="workbench-save" data-action="workbench.save" data-target={path} disabled={!canSave} onClick={onSave}>
          <StudioIcon name="check" />
          {t("common.save")}
        </button>
      ) : null}
      {onMore && (
        <button className="workbench-reveal" data-action="workbench.file-more" data-target={path} aria-haspopup="menu" aria-label={t("common.more")} title={t("common.more")}
          onClick={(e) => { e.stopPropagation(); onMore(e.currentTarget); }}>
          <StudioIcon name="more" />
        </button>
      )}
    </div>
  );
}
