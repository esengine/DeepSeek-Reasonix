import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { TreeSession, TreeWorkspace } from "../port/hub";
import { StudioIcon } from "./StudioIcon";
import { useModalFocus } from "./useModalFocus";

/** Choosing the project a conversation moves to. It says what travels (the
 *  conversation and the labels the person set) and what does not (what the old
 *  project allowed: the new one decides that for itself), and that the paths
 *  recorded inside the conversation are left as they were. */
export function MoveSessionDialog({ session, label, projects, busy, error, note, onMove, onClose, onAddProject }: {
  session: TreeSession;
  label: string;
  projects: TreeWorkspace[];
  busy: boolean;
  error: string;
  note: string;
  onMove: (target: string) => void;
  onClose: () => void;
  onAddProject?: () => void;
}) {
  const sheet = useRef<HTMLDivElement>(null);
  useModalFocus(sheet);
  const cancel = useRef<HTMLButtonElement>(null);
  const [target, setTarget] = useState(projects[0]?.root ?? "");
  useEffect(() => {
    const checked = sheet.current?.querySelector<HTMLInputElement>('input[type="radio"]:checked');
    (checked ?? cancel.current)?.focus();
  }, []);
  return createPortal(
    <div ref={sheet} className="pickveil" role="dialog" aria-modal="true" aria-labelledby="movess-t">
      <form className="pickcard movess" data-action="session.move-to" data-target={session.path} onSubmit={(ev) => { ev.preventDefault(); if (target) onMove(target); }}>
        <h2 id="movess-t">{t("把「{name}」移到另一个项目", { name: label })}</h2>
        {projects.length === 0 ? (
          <div className="movess-empty">
            <p className="picknote">{t("没有别的项目可以移到。先添加一个项目。")}</p>
            {onAddProject && <button type="button" className="act" data-action="session.move-to-add" onClick={() => { onClose(); onAddProject(); }}>{t("添加项目")}</button>}
          </div>
        ) : (
          <>
            <div className="movess-label">{t("目标项目")}</div>
            <div className="movess-list" role="radiogroup" aria-label={t("目标项目")}>
              {projects.map((p) => (
                <label key={p.root} className="movess-row" data-on={p.root === target || undefined}>
                  <input type="radio" name="movess-target" value={p.root} checked={p.root === target} disabled={busy} data-action-change="session.move-to-target" onChange={() => setTarget(p.root)} />
                  <StudioIcon name="folder" />
                  <span className="movess-name"><b>{p.name}</b><small>{p.root}</small></span>
                  {p.root === target && <StudioIcon name="check" className="movess-check" />}
                </label>
              ))}
            </div>
            <div className="movess-cols">
              <section data-part="travels">
                <h3>{t("会带走")}</h3>
                <ul>
                  <li>{t("对话本身")}</li>
                  <li>{t("你设的标题、置顶、归档")}</li>
                  <li>{t("对话里记录的路径保持原样")}</li>
                </ul>
              </section>
              <section data-part="stays">
                <h3>{t("不会带走")}</h3>
                <ul>
                  <li>{t("检查点（回退快照）")}</li>
                  <li>{t("后台任务日志")}</li>
                  <li>{t("排队中的消息")}</li>
                </ul>
              </section>
            </div>
            <p className="picknote">{t("原项目的权限与目标设置不会跟着走，新项目按它自己的规则重新判断。不随会话移动的内容会从原项目移除。")}</p>
          </>
        )}
        {error && <p className="why" role="alert">{error}</p>}
        {note && <p className="picknote" role="status">{note}</p>}
        <div className="movess-act">
          <button ref={cancel} type="button" className="kbtn" data-size="md" data-variant="ghost" data-action="session.move-to-cancel" disabled={busy} onClick={onClose}>{note ? t("关闭") : t("取消")}</button>
          {!note && <button type="submit" className="kbtn" data-size="md" data-variant="primary" data-action="session.move-to" data-target={session.path} disabled={busy || !target}>{t("移动")}</button>}
        </div>
      </form>
    </div>,
    document.body,
  );
}
