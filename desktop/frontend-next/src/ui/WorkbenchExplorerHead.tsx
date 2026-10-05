import { useCallback, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import { StudioIcon } from "./StudioIcon";

/** The explorer's title row and the actions that act on the whole workspace.
 *  They sit beside the files rather than in settings: this is the one place
 *  the workspace is already what you are looking at. */
export function WorkbenchExplorerHead({
  port,
  count,
  revealable,
  hidden,
  onHidden,
  onReveal,
}: {
  port: AgentPort;
  count: number;
  revealable: boolean;
  hidden: boolean;
  onHidden: (on: boolean) => void;
  onReveal: () => void;
}) {
  // Which editor opened it, or why none did. The host knows both and says so,
  // because a button that does nothing is indistinguishable from a broken one.
  const [editorNote, setEditorNote] = useState("");
  const openEditor = useCallback(() => {
    void port
      .openInEditor()
      .then(({ editor }) => setEditorNote(t("已在 {app} 中打开", { app: editor })))
      .catch((e) => setEditorNote(reason(e)));
  }, [port]);
  return (
    <div className="workbench-explorer-head">
      <span>{t("资源管理器")}</span>
      <small>{count}</small>
      <button
        className="workbench-open-editor"
        data-action="workbench.hidden"
        aria-pressed={hidden}
        title={t("显示隐藏文件")}
        aria-label={t("显示隐藏文件")}
        onClick={() => onHidden(!hidden)}
      >
        <StudioIcon name={hidden ? "eye" : "eyeoff"} />
      </button>
      <button
        className="workbench-open-editor"
        data-action="workspace.editor"
        title={editorNote || t("在代码编辑器中打开工作区")}
        aria-label={t("在代码编辑器中打开工作区")}
        onClick={openEditor}
      >
        <StudioIcon name="code" />
      </button>
      {revealable && (
        <button
          className="workbench-open-editor"
          data-action="workbench.reveal"
          title={t("在系统文件管理器中显示工作区")}
          aria-label={t("在系统文件管理器中显示工作区")}
          onClick={onReveal}
        >
          <StudioIcon name="reveal" />
        </button>
      )}
    </div>
  );
}
