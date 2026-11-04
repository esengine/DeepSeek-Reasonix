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
  onReveal,
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
  // Present only where the kernel's own machine can show the file.
  onReveal?: () => void;
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
      {onReveal && (
        <button className="workbench-reveal" data-action="workbench.reveal" data-target={path} aria-label={t("在系统文件管理器中显示此文件")} title={t("在系统文件管理器中显示此文件")} onClick={onReveal}>
          <StudioIcon name="reveal" />
        </button>
      )}
      <div role="group">
        {readable && view("read", t("阅读"))}
        {document && view("document", t("预览"))}
        {previewable && view("preview", t("交互运行"))}
        {source && view("file", !readOnlyIn && (readable || previewable || document) ? t("编辑") : t("文件"))}
        {source && !readOnlyIn && view("diff", "Diff")}
      </div>
      {openWith}
      {readOnlyIn ? (
        <span className="workbench-readonly" title={readOnlyIn}>{t("只读 · 来自你授权的文件夹")}</span>
      ) : source ? (
        <button className="workbench-save" data-action="workbench.save" data-target={path} disabled={!canSave} onClick={onSave}>
          <StudioIcon name="check" />
          {t("保存")}
        </button>
      ) : null}
    </div>
  );
}
