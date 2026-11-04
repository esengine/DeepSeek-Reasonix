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
        <h2 id="movess-t">{t("session.move_name_to_another_project", { name: label })}</h2>
        {projects.length === 0 ? (
          <div className="movess-empty">
            <p className="picknote">{t("session.there_is_no_other_project")}</p>
            {onAddProject && <button type="button" className="act" data-action="session.move-to-add" onClick={() => { onClose(); onAddProject(); }}>{t("session.add_a_project")}</button>}
          </div>
        ) : (
          <>
            <div className="movess-label">{t("session.target_project")}</div>
            <div className="movess-list" role="radiogroup" aria-label={t("session.target_project")}>
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
                <h3>{t("session.travels")}</h3>
                <ul>
                  <li>{t("session.the_conversation_itself")}</li>
                  <li>{t("session.the_title_pin_and_archive")}</li>
                  <li>{t("session.paths_recorded_in_the_conversation")}</li>
                </ul>
              </section>
              <section data-part="stays">
                <h3>{t("session.stays_behind")}</h3>
                <ul>
                  <li>{t("session.checkpoints_rewind_snapshots")}</li>
                  <li>{t("session.background_task_logs")}</li>
                  <li>{t("session.queued_messages")}</li>
                </ul>
              </section>
            </div>
            <p className="picknote">{t("session.what_the_old_project_allowed")}</p>
          </>
        )}
        {error && <p className="why" role="alert">{error}</p>}
        {note && <p className="picknote" role="status">{note}</p>}
        <div className="movess-act">
          <button ref={cancel} type="button" className="kbtn" data-size="md" data-variant="ghost" data-action="session.move-to-cancel" disabled={busy} onClick={onClose}>{note ? t("common.close") : t("common.cancel")}</button>
          {!note && <button type="submit" className="kbtn" data-size="md" data-variant="primary" data-action="session.move-to" data-target={session.path} disabled={busy || !target}>{t("session.move")}</button>}
        </div>
      </form>
    </div>,
    document.body,
  );
}
