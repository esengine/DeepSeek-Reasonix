import { useCallback, useState, type ReactNode } from "react";
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
  openWith,
}: {
  port: AgentPort;
  count: number;
  revealable: boolean;
  hidden: boolean;
  onHidden: (on: boolean) => void;
  onReveal: () => void;
  openWith?: ReactNode;
}) {
  // Which editor opened it, or why none did. The host knows both and says so,
  // because a button that does nothing is indistinguishable from a broken one.
  const [editorNote, setEditorNote] = useState("");
  const openEditor = useCallback(() => {
    void port
      .openInEditor()
      .then(({ editor }) => setEditorNote(t("workbenchExplorerHead.openedIn", { app: editor })))
      .catch((e) => setEditorNote(reason(e)));
  }, [port]);
  return (
    <div className="workbench-explorer-head">
      <span>{t("workbenchExplorerHead.explorer")}</span>
      <small>{count}</small>
      <button
        className="workbench-open-editor"
        data-action="workbench.hidden"
        aria-pressed={hidden}
        title={t("workbenchExplorerHead.showHiddenFiles")}
        aria-label={t("workbenchExplorerHead.showHiddenFiles")}
        onClick={() => onHidden(!hidden)}
      >
        <StudioIcon name={hidden ? "eye" : "eyeoff"} />
      </button>
      <button
        className="workbench-open-editor"
        data-action="workspace.editor"
        title={editorNote || t("workbenchExplorerHead.openTheWorkspaceInA")}
        aria-label={t("workbenchExplorerHead.openTheWorkspaceInA")}
        onClick={openEditor}
      >
        <StudioIcon name="code" />
      </button>
      {revealable && (
        <button
          className="workbench-open-editor"
          data-action="workbench.reveal"
          title={t("workbenchExplorerHead.showTheWorkspaceInThe")}
          aria-label={t("workbenchExplorerHead.showTheWorkspaceInThe")}
          onClick={onReveal}
        >
          <StudioIcon name="reveal" />
        </button>
      )}
      {openWith}
    </div>
  );
}
