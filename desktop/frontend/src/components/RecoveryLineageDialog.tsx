import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { GitBranch, Pencil, X } from "lucide-react";
import { app } from "../lib/bridge";
import type { ProjectTopicKey } from "../lib/sessionCatalogTypes";
import type { RecoveryLineageMember, RecoveryLineageView } from "../lib/types";
import { useT } from "../lib/i18n";
import { useToast } from "../lib/toast";
import { normalizeRecoveryLineageView, userVisibleRecoveryVersions } from "../lib/sessionRecoveryVersions";

interface RecoveryLineageDialogProps {
  topic: ProjectTopicKey;
  initial: RecoveryLineageView;
  onClose: () => void;
  onChanged: (view: RecoveryLineageView) => Promise<void> | void;
  onOpenVersion?: (member: RecoveryLineageMember) => Promise<void> | void;
}

function versionActivityAt(member: RecoveryLineageMember): number {
  return member.lastActivityAt || member.createdAt || 0;
}

export function RecoveryLineageDialog({ topic, initial, onClose, onChanged, onOpenVersion }: RecoveryLineageDialogProps) {
  const t = useT();
  const { showToast } = useToast();
  const [view, setView] = useState(() => normalizeRecoveryLineageView(initial));
  const [busy, setBusy] = useState(false);
  const [editingPath, setEditingPath] = useState("");
  const [noteDraft, setNoteDraft] = useState("");
  const members = useMemo(() => userVisibleRecoveryVersions(view), [view]);

  useEffect(() => setView(normalizeRecoveryLineageView(initial)), [initial]);

  const refresh = async () => {
    const next = normalizeRecoveryLineageView(await app.GetRecoveryLineage(topic));
    setView(next);
    await onChanged(next);
    return next;
  };

  const choose = async (path: string) => {
    if (busy) return;
    setBusy(true);
    try {
      await app.ChooseRecoveryBranch({ ...topic, path });
      await refresh();
    } finally {
      setBusy(false);
    }
  };

  const openVersion = async (member: RecoveryLineageMember) => {
    if (busy || !onOpenVersion) return;
    setBusy(true);
    try {
      await onOpenVersion(member);
      onClose();
    } finally {
      setBusy(false);
    }
  };

  const startNoteEdit = (member: RecoveryLineageMember) => {
    if (busy) return;
    setEditingPath(member.path);
    setNoteDraft(member.versionNote || "");
  };

  const saveNote = async (member: RecoveryLineageMember) => {
    if (busy || editingPath !== member.path) return;
    setBusy(true);
    try {
      await app.RenameSession(member.path, noteDraft.trim());
      setEditingPath("");
      await refresh();
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error");
    } finally {
      setBusy(false);
    }
  };

  return createPortal(
    <div className="management-modal-backdrop recovery-lineage-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section className="management-modal recovery-lineage-dialog" role="dialog" aria-modal="true" aria-labelledby="recovery-lineage-title">
        <header className="management-modal__head">
          <div>
            <div className="management-modal__title" id="recovery-lineage-title">
              <GitBranch size={17} aria-hidden="true" /> {t("recovery.lineageTitle")}
            </div>
            <div className="management-modal__summary">
              {t("recovery.lineageSummary", { branches: members.length, unresolved: view.unresolved })}
            </div>
          </div>
          <button type="button" className="icon-btn" onClick={onClose} aria-label={t("common.close")}><X size={16} /></button>
        </header>
        <div className="recovery-lineage-dialog__body">
          {members.map((member) => {
            const activityAt = versionActivityAt(member);
            const editing = editingPath === member.path;
            return (
              <article className="recovery-lineage-dialog__member" key={member.path}>
                <div className="recovery-lineage-dialog__member-copy">
                  <div className="recovery-lineage-dialog__member-name">
                    {t(member.canonical ? "recovery.defaultVersion" : "recovery.alternateVersion")}
                    {(member.open || member.running) && <span className="hist-item__badge hist-item__badge--open">{t("recovery.inUse")}</span>}
                  </div>
                  {editing ? (
                    <div className="recovery-lineage-dialog__note-editor">
                      <input
                        autoFocus
                        value={noteDraft}
                        onChange={(event) => setNoteDraft(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter") void saveNote(member);
                          if (event.key === "Escape") setEditingPath("");
                        }}
                        placeholder={t("recovery.versionNotePlaceholder")}
                      />
                      <button type="button" className="btn btn--small" disabled={busy} onClick={() => void saveNote(member)}>{t("common.save")}</button>
                      <button type="button" className="btn btn--small" disabled={busy} onClick={() => setEditingPath("")}>{t("common.cancel")}</button>
                    </div>
                  ) : (
                    <button type="button" className="recovery-lineage-dialog__version-note" disabled={busy} onClick={() => startNoteEdit(member)}>
                      <Pencil size={12} aria-hidden="true" />
                      {member.versionNote?.trim() || t("recovery.addVersionNote")}
                    </button>
                  )}
                  <div className="recovery-lineage-dialog__preview">{member.preview?.trim() || t("recovery.versionPreviewEmpty")}</div>
                  <div className="recovery-lineage-dialog__member-meta">
                    <span>{t(member.turns === 1 ? "history.turnOne" : "history.turnOther", { n: member.turns })}</span>
                    {activityAt > 0 && <span>{new Date(activityAt).toLocaleString()}</span>}
                  </div>
                </div>
                <div className="recovery-lineage-dialog__member-actions">
                  {onOpenVersion && (
                    <button type="button" className="btn btn--small btn--primary" disabled={busy} onClick={() => void openVersion(member)}>
                      {t("recovery.openVersion")}
                    </button>
                  )}
                  {!member.canonical && (
                    <button type="button" className="btn btn--small" disabled={busy} onClick={() => void choose(member.path)}>
                      {t("recovery.chooseBranch")}
                    </button>
                  )}
                </div>
              </article>
            );
          })}
          {members.length === 0 && <div className="management-modal__summary">{t("recovery.lineageEmpty")}</div>}
        </div>
        <footer className="modal__actions recovery-lineage-dialog__actions">
          <button type="button" className="btn" onClick={onClose}>{t("common.close")}</button>
        </footer>
      </section>
    </div>,
    document.body,
  );
}
