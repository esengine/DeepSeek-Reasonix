import { useEffect, useRef, useState } from "react";
import { useModalFocus } from "./useModalFocus";
import { createPortal } from "react-dom";
import { t } from "../i18n";

interface Props {
  busy: boolean;
  onSubmit: (path: string) => void;
  onClose: () => void;
}

/** The picker-free path into a workspace. It only appears when the kernel says
 *  it has no native picker, so a desktop shell keeps its familiar dialog. */
export function AddWorkspacePrompt({ busy, onSubmit, onClose }: Props) {
  const sheet = useRef<HTMLDivElement>(null);
  useModalFocus(sheet);
  const [path, setPath] = useState("");
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    input.current?.focus();
  }, []);

  return createPortal(
    <div ref={sheet} className="pickveil" role="dialog" aria-modal="true" aria-labelledby="addws-t">
      <form
        className="pickcard"
        data-action="workspace.add"
        onSubmit={(ev) => {
          ev.preventDefault();
          onSubmit(path);
        }}
      >
        <h2 id="addws-t">{t("session.add_a_workspace")}</h2>
        <p className="picknote">{t("session.the_folder_picker_cannot_open")}</p>
        <div className="pickpath">
          <input
            ref={input}
            value={path}
            spellCheck={false}
            dir="ltr"
            placeholder="/srv/project"
            aria-label={t("session.workspace_path")}
            data-action="workspace.add"
            onChange={(ev) => setPath(ev.target.value)}
          />
          <button type="submit" className="pickgo" data-action="workspace.add" disabled={busy || !path.trim()}>
            {t("common.add")}
          </button>
        </div>
        <div className="pickact">
          <button type="button" data-action="workspace.add-cancel" disabled={busy} onClick={onClose}>{t("common.cancel")}</button>
        </div>
      </form>
    </div>,
    document.body,
  );
}
