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
      .then(({ editor }) => setEditorNote(t("workbench.opened_in_app", { app: editor })))
      .catch((e) => setEditorNote(reason(e)));
  }, [port]);
  return (
    <div className="workbench-explorer-head">
      <span>{t("workbench.explorer")}</span>
      <small>{count}</small>
      <button
        className="workbench-open-editor"
        data-action="workbench.hidden"
        aria-pressed={hidden}
        title={t("workbench.show_hidden_files")}
        aria-label={t("workbench.show_hidden_files")}
        onClick={() => onHidden(!hidden)}
      >
        <StudioIcon name={hidden ? "eye" : "eyeoff"} />
      </button>
      <button
        className="workbench-open-editor"
        data-action="workspace.editor"
        title={editorNote || t("workbench.open_the_workspace_in_a")}
        aria-label={t("workbench.open_the_workspace_in_a")}
        onClick={openEditor}
      >
        <StudioIcon name="code" />
      </button>
      {revealable && (
        <button
          className="workbench-open-editor"
          data-action="workbench.reveal"
          title={t("workbench.show_the_workspace_in_the")}
          aria-label={t("workbench.show_the_workspace_in_the")}
          onClick={onReveal}
        >
          <StudioIcon name="reveal" />
        </button>
      )}
      {openWith}
    </div>
  );
}
