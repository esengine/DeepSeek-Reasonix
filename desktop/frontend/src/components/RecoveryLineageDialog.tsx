import { useState } from "react";
import { createPortal } from "react-dom";
import { GitBranch, Trash2, X } from "lucide-react";
import { app } from "../lib/bridge";
import { asArray } from "../lib/array";
import type { ProjectTopicKey } from "../lib/sessionCatalogTypes";
import type { RecoveryCleanupResult, RecoveryLineageView } from "../lib/types";
import { useT } from "../lib/i18n";

interface RecoveryLineageDialogProps {
  topic: ProjectTopicKey;
  initial: RecoveryLineageView;
  onClose: () => void;
  onChanged: () => Promise<void> | void;
  onOpenVersion?: (path: string) => Promise<void> | void;
}

function fileName(path: string): string {
  const parts = path.split(/[\\/]/);
  return parts[parts.length - 1] || path;
}

function roleKey(role: string): "recovery.role.covered_copy" | "recovery.role.adopted" | "recovery.role.preferred" | "recovery.role.diverged" | "recovery.role.normal" {
  if (role === "covered_copy") return "recovery.role.covered_copy";
  if (role === "adopted") return "recovery.role.adopted";
  if (role === "preferred") return "recovery.role.preferred";
  if (role === "diverged") return "recovery.role.diverged";
  return "recovery.role.normal";
}

export function RecoveryLineageDialog({ topic, initial, onClose, onChanged, onOpenVersion }: RecoveryLineageDialogProps) {
  const t = useT();
  const [view, setView] = useState(initial);
  const [preview, setPreview] = useState<RecoveryCleanupResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<RecoveryCleanupResult | null>(null);

  const choose = async (path: string) => {
    if (busy) return;
    setBusy(true);
    try {
      await app.ChooseRecoveryBranch({ ...topic, path });
      setView(await app.GetRecoveryLineage(topic));
      await onChanged();
    } finally {
      setBusy(false);
    }
  };

  const openVersion = async (path: string) => {
    if (busy || !onOpenVersion) return;
    setBusy(true);
    try {
      await onOpenVersion(path);
      onClose();
    } finally {
      setBusy(false);
    }
  };

  const clean = async (apply: boolean) => {
    if (busy) return;
    setBusy(true);
    try {
      const next = await app.CleanRecoveryLineage({ ...topic, apply });
      if (!apply) {
        setPreview(next);
        return;
      }
      setResult(next);
      setPreview(null);
      setView(await app.GetRecoveryLineage(topic));
      await onChanged();
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
              {t("recovery.lineageSummary", { branches: view.branchCount, unresolved: view.unresolved })}
            </div>
          </div>
          <button type="button" className="icon-btn" onClick={onClose} aria-label={t("common.close")}><X size={16} /></button>
        </header>
        <div className="recovery-lineage-dialog__body">
          {asArray(view.members).map((member) => (
            <div className="recovery-lineage-dialog__member" key={member.path}>
              <div className="recovery-lineage-dialog__member-name" title={member.path}>{fileName(member.path)}</div>
              <div className="recovery-lineage-dialog__member-meta">
                <span>{t(roleKey(member.role))}</span>
                <span>{t(member.turns === 1 ? "history.turnOne" : "history.turnOther", { n: member.turns })}</span>
                {(member.open || member.running) && <span>{t("recovery.inUse")}</span>}
                {member.canonical && <span>{t("recovery.role.preferred")}</span>}
                {onOpenVersion && (
                  <button type="button" className="recovery-lineage-dialog__choose" disabled={busy} onClick={() => void openVersion(member.path)}>
                    {t("recovery.openVersion")}
                  </button>
                )}
                {member.role !== "covered_copy" && !member.canonical && (
                  <button type="button" className="recovery-lineage-dialog__choose" disabled={busy} onClick={() => void choose(member.path)}>
                    {t("recovery.chooseBranch")}
                  </button>
                )}
              </div>
            </div>
          ))}
          {view.members.length === 0 && <div className="management-modal__summary">{t("recovery.lineageEmpty")}</div>}
          {preview && (
            <div className="recovery-lineage-dialog__notice" role="status">
              {t("recovery.cleanupConfirm", { count: preview.eligible })}
            </div>
          )}
          {result && (
            <div className="recovery-lineage-dialog__notice" role="status">
              {t("recovery.cleanupResult", { moved: result.moved, busy: result.busy, kept: result.kept })}
            </div>
          )}
        </div>
        <footer className="modal__actions recovery-lineage-dialog__actions">
          <button type="button" className="btn" onClick={onClose}>{t("common.close")}</button>
          {view.cleanupEligible > 0 && !preview && (
            <button type="button" className="btn" disabled={busy} onClick={() => void clean(false)}>
              <Trash2 size={14} /> {t("recovery.previewCleanup")}
            </button>
          )}
          {preview && preview.eligible > 0 && (
            <button type="button" className="btn btn--danger" disabled={busy} onClick={() => void clean(true)}>
              <Trash2 size={14} /> {t("recovery.applyCleanup")}
            </button>
          )}
        </footer>
      </section>
    </div>,
    document.body,
  );
}
