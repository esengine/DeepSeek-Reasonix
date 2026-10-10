import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

export type FileMode = "file" | "diff" | "read" | "preview";

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
  onSave,
}: {
  path: string;
  mode: FileMode;
  onMode: (mode: FileMode) => void;
  readable: boolean;
  previewable: boolean;
  canSave: boolean;
  onSave: () => void;
}) {
  const view = (value: FileMode, label: string) => (
    <button data-action="workbench.mode" data-value={value} aria-pressed={mode === value} onClick={() => onMode(value)}>
      {label}
    </button>
  );
  return (
    <div className="workbench-filebar">
      <strong>{path}</strong>
      <div role="group">
        {readable && view("read", t("阅读"))}
        {previewable && view("preview", t("预览"))}
        {view("file", readable || previewable ? t("编辑") : t("文件"))}
        {view("diff", "Diff")}
      </div>
      <button className="workbench-save" data-action="workbench.save" data-target={path} disabled={!canSave} onClick={onSave}>
        <StudioIcon name="check" />
        {t("保存")}
      </button>
    </div>
  );
}
