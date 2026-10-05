import { useEffect, useRef, useState } from "react";
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
  const [path, setPath] = useState("");
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    input.current?.focus();
  }, []);

  return createPortal(
    <div className="pickveil" role="dialog" aria-modal="true" aria-labelledby="addws-t">
      <form
        className="pickcard"
        data-action="workspace.add"
        onSubmit={(ev) => {
          ev.preventDefault();
          onSubmit(path);
        }}
      >
        <h2 id="addws-t">{t("添加工作区")}</h2>
        <p className="picknote">{t("无法打开文件夹选择器。请输入内核所在机器上的路径。")}</p>
        <div className="pickpath">
          <input
            ref={input}
            value={path}
            spellCheck={false}
            dir="ltr"
            placeholder="/srv/project"
            aria-label={t("工作区路径")}
            data-action="workspace.add"
            onChange={(ev) => setPath(ev.target.value)}
          />
          <button type="submit" className="pickgo" data-action="workspace.add" disabled={busy || !path.trim()}>
            {t("添加")}
          </button>
        </div>
        <div className="pickact">
          <button type="button" data-action="workspace.add-cancel" disabled={busy} onClick={onClose}>{t("取消")}</button>
        </div>
      </form>
    </div>,
    document.body,
  );
}
