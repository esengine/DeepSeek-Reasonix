import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { GitBranch, Pencil, X } from "lucide-react";
import { app } from "../lib/bridge";
import { recordFrontendDiagnostic } from "../lib/frontendDiagnosticBridge";
import type { ProjectTopicKey } from "../lib/sessionCatalogTypes";
import type { RecoveryLineageMember, RecoveryLineageView } from "../lib/types";
import { useT, type DictKey } from "../lib/i18n";
import { useToast } from "../lib/toast";
import { normalizeRecoveryLineageView, userVisibleRecoveryVersions } from "../lib/sessionRecoveryVersions";

interface RecoveryLineageDialogProps {
  topic: ProjectTopicKey;
  initial: RecoveryLineageView;
  onClose: () => void;
  onChanged: (view: RecoveryLineageView) => Promise<void> | void;
  onOpenVersion?: (member: RecoveryLineageMember) => Promise<void> | void;
}

/** Heads of one log share a path, so the head id is the version identity. */
function memberKey(member: RecoveryLineageMember): string {
  return member.headId ? `${member.path}#${member.headId}` : member.path;
}

const headKindKeys: Record<string, DictKey> = {
  main: "recovery.headKind.main",
  fork: "recovery.headKind.fork",
  rewind: "recovery.headKind.rewind",
  concurrent: "recovery.headKind.concurrent",
};

function memberTitleKey(member: RecoveryLineageMember): DictKey {
  if (member.headId) return member.selected ? "recovery.currentVersion" : "recovery.alternateVersion";
  return member.canonical ? "recovery.defaultVersion" : "recovery.alternateVersion";
}

function versionActivityAt(member: RecoveryLineageMember): number {
  return member.lastActivityAt || member.createdAt || 0;
}

function failureText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function RecoveryLineageDialog({ topic, initial, onClose, onChanged, onOpenVersion }: RecoveryLineageDialogProps) {
  const t = useT();
  const { showToast } = useToast();
  const [view, setView] = useState(() => normalizeRecoveryLineageView(initial));
  const [busy, setBusy] = useState(false);
  const [editingPath, setEditingPath] = useState("");
  const [noteDraft, setNoteDraft] = useState("");
  const members = useMemo(() => userVisibleRecoveryVersions(view), [view]);
  const heads = view.state === "heads";

  useEffect(() => setView(normalizeRecoveryLineageView(initial)), [initial]);

  const refresh = async () => {
    const next = normalizeRecoveryLineageView(await app.GetRecoveryLineage(topic));
    setView(next);
    await onChanged(next);
    return next;
  };

  const choose = async (member: RecoveryLineageMember) => {
    if (busy) return;
    setBusy(true);
    try {
      await app.ChooseRecoveryBranch({ ...topic, path: member.path, headId: member.headId });
      await refresh();
    } catch (error) {
      recordFrontendDiagnostic("app", "session.recovery-choose-failed", { status: "error" });
      showToast(t("recovery.chooseBranchFailed", { error: failureText(error) }), "error");
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
    } catch (error) {
      recordFrontendDiagnostic("app", "session.recovery-open-failed", { status: "error" });
      showToast(t("recovery.openVersionFailed", { error: failureText(error) }), "error");
    } finally {
      setBusy(false);
    }
  };

  const startNoteEdit = (member: RecoveryLineageMember) => {
    if (busy) return;
    setEditingPath(memberKey(member));
    setNoteDraft(member.versionNote || "");
  };

  const retireCovered = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const result = await app.CleanRecoveryLineage({ scope: topic.scope, workspaceRoot: topic.workspaceRoot, topicId: topic.topicId, apply: true });
      showToast(t("recovery.retireCoveredDone", { removed: result.moved, busy: result.busy }), result.busy > 0 ? "warn" : "info");
      await refresh();
    } catch (error) {
      showToast(t("recovery.retireCoveredFailed", { error: failureText(error) }), "error");
    } finally {
      setBusy(false);
    }
  };

  const saveNote = async (member: RecoveryLineageMember) => {
    if (busy || editingPath !== memberKey(member)) return;
    setBusy(true);
    try {
      if (member.headId) await app.RenameSessionHead(member.path, member.headId, noteDraft.trim());
      else await app.RenameSession(member.path, noteDraft.trim());
      setEditingPath("");
      await refresh();
    } catch (error) {
      showToast(failureText(error), "error");
    } finally {
      setBusy(false);
    }
  };

  return createPortal(
    <div className="management-modal-backdrop recovery-lineage-backdrop" data-app-overlay="" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section className="management-modal recovery-lineage-dialog" role="dialog" aria-modal="true" aria-labelledby="recovery-lineage-title">
        <header className="management-modal__head">
          <div>
            <div className="management-modal__title" id="recovery-lineage-title">
              <GitBranch size={17} aria-hidden="true" /> {t("recovery.lineageTitle")}
            </div>
            <div className="management-modal__summary">
              {heads ? t("recovery.headsSummary", { branches: members.length }) : t("recovery.lineageSummary", { branches: members.length, unresolved: view.unresolved })}
            </div>
          </div>
          <button type="button" className="icon-btn" onClick={onClose} aria-label={t("common.close")}><X size={16} /></button>
        </header>
        <div className="recovery-lineage-dialog__body">
          {members.map((member) => {
            const activityAt = versionActivityAt(member);
            const editing = editingPath === memberKey(member);
            return (
              <article className="recovery-lineage-dialog__member" key={memberKey(member)}>
                <div className="recovery-lineage-dialog__member-copy">
                  <div className="recovery-lineage-dialog__member-name">
                    {member.headName?.trim() || t(memberTitleKey(member))}
                    {member.headKind && headKindKeys[member.headKind] && <span className="hist-item__badge">{t(headKindKeys[member.headKind])}</span>}
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
                    <button type="button" className="btn btn--small" disabled={busy} onClick={() => void choose(member)}>
                      {t(member.headId ? "recovery.makeCurrent" : "recovery.chooseBranch")}
                    </button>
                  )}
                </div>
              </article>
            );
          })}
          {members.length === 0 && <div className="management-modal__summary">{t("recovery.lineageEmpty")}</div>}
        </div>
        <footer className="modal__actions recovery-lineage-dialog__actions">
          {heads && view.cleanupEligible > 0 && (
            <button type="button" className="btn" disabled={busy} onClick={() => void retireCovered()}>
              {t("recovery.retireCovered", { n: view.cleanupEligible })}
            </button>
          )}
          <button type="button" className="btn" onClick={onClose}>{t("common.close")}</button>
        </footer>
      </section>
    </div>,
    document.body,
  );
}
