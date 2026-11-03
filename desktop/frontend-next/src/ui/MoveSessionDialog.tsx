import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { TreeSession, TreeWorkspace } from "../port/hub";
import { useModalFocus } from "./useModalFocus";

/** Choosing the project a conversation moves to. It says what travels (the
 *  conversation and the labels the person set) and what does not (what the old
 *  project allowed: the new one decides that for itself), and that the paths
 *  recorded inside the conversation are left as they were. */
export function MoveSessionDialog({ session, label, projects, busy, error, note, onMove, onClose }: {
  session: TreeSession;
  label: string;
  projects: TreeWorkspace[];
  busy: boolean;
  error: string;
  note: string;
  onMove: (target: string) => void;
  onClose: () => void;
}) {
  const sheet = useRef<HTMLDivElement>(null);
  useModalFocus(sheet);
  const choose = useRef<HTMLSelectElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  useEffect(() => { (choose.current ?? cancel.current)?.focus(); }, []);
  const [target, setTarget] = useState(projects[0]?.root ?? "");
  return createPortal(
    <div ref={sheet} className="pickveil" role="dialog" aria-modal="true" aria-labelledby="movess-t">
      <form className="pickcard movess" data-action="session.move-to" data-target={session.path} onSubmit={(ev) => { ev.preventDefault(); if (target) onMove(target); }}>
        <h2 id="movess-t">{t("moveSessionDialog.moveToAnotherProject", { name: label })}</h2>
        <p className="picknote">{t("moveSessionDialog.onlyTheConversationItselfAnd")}</p>
        <p className="picknote">{t("moveSessionDialog.checkpointsRewindSnapshotsBackground")}</p>
        {projects.length === 0 ? (
          <p className="picknote">{t("moveSessionDialog.thereIsNoOtherProject")}</p>
        ) : (
          <select ref={choose} aria-label={t("moveSessionDialog.targetProject")} data-action-change="session.move-to-target" value={target} disabled={busy} onChange={(ev) => setTarget(ev.target.value)}>
            {projects.map((p) => <option key={p.root} value={p.root}>{`${p.name} — ${p.root}`}</option>)}
          </select>
        )}
        {error && <p className="why" role="alert">{error}</p>}
        {note && <p className="picknote" role="status">{note}</p>}
        <div className="pickact">
          <button ref={cancel} type="button" data-action="session.move-to-cancel" disabled={busy} onClick={onClose}>{note ? t("common.close") : t("common.cancel")}</button>
          {!note && <button type="submit" className="pickgo" data-action="session.move-to" data-target={session.path} disabled={busy || !target}>{t("moveSessionDialog.move")}</button>}
        </div>
      </form>
    </div>,
    document.body,
  );
}
