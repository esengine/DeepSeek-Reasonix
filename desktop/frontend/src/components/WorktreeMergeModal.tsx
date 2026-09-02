import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, ArrowRight, CheckCircle2, FileText, GitBranch, GitMerge, Loader2 } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { WorktreeMergeBlocker, WorktreeMergeInspection, WorktreeMergeResult } from "../lib/types";
import { ModalCloseButton } from "./ModalCloseButton";
import "./WorktreeMergeModal.css";

interface WorktreeMergeModalProps {
  tabId: string;
  isOpen: boolean;
  onClose: () => void;
  onMerged?: (result: WorktreeMergeResult) => Promise<void> | void;
}

function inspectionIdentity(inspection: WorktreeMergeInspection): string {
  return JSON.stringify({
    worktreeRoot: inspection.worktreeRoot,
    sourceRoot: inspection.sourceRoot,
    worktreeBranch: inspection.worktreeBranch,
    targetBranch: inspection.targetBranch,
    worktreeHead: inspection.worktreeHead,
    worktreeStateToken: inspection.worktreeStateToken,
    targetHead: inspection.targetHead,
    worktreeDirty: inspection.worktreeDirty,
    sourceDirty: inspection.sourceDirty,
    hasConflicts: inspection.hasConflicts,
    blockers: inspection.blockers.map((item) => item.code),
  });
}

function BlockerList({ blockers }: { blockers: WorktreeMergeBlocker[] }) {
  if (blockers.length === 0) return null;
  return (
    <div className="worktree-merge__blockers" role="status">
      {blockers.map((blocker, index) => (
        <div className="worktree-merge__blocker" key={`${blocker.code}-${index}`}>
          <AlertTriangle size={15} aria-hidden="true" />
          <div>
            <div>{blocker.message}</div>
            {blocker.paths.length > 0 && <div className="worktree-merge__paths">{blocker.paths.join(", ")}</div>}
          </div>
        </div>
      ))}
    </div>
  );
}

export function WorktreeMergeModal({ tabId, isOpen, onClose, onMerged }: WorktreeMergeModalProps) {
  const t = useT();
  const dialogRef = useRef<HTMLElement>(null);
  const requestGeneration = useRef(0);
  const mergingRef = useRef(false);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  const [loading, setLoading] = useState(true);
  const [merging, setMerging] = useState(false);
  const [inspection, setInspection] = useState<WorktreeMergeInspection | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [autoCommitDirty, setAutoCommitDirty] = useState(false);

  const fetchInspection = useCallback(async () => {
    if (!tabId) return null;
    const generation = ++requestGeneration.current;
    setLoading(true);
    setError(null);
    try {
      const result = await app.InspectWorktreeMerge(tabId);
      if (generation !== requestGeneration.current) return null;
      setInspection({
        ...result,
        changedFiles: result.changedFiles ?? [],
        conflictFiles: result.conflictFiles ?? [],
        blockers: result.blockers ?? [],
        cleanupBlockers: result.cleanupBlockers ?? [],
      });
      return result;
    } catch (caught: unknown) {
      if (generation === requestGeneration.current) setError(caught instanceof Error ? caught.message : String(caught));
      return null;
    } finally {
      if (generation === requestGeneration.current) setLoading(false);
    }
  }, [tabId]);

  useEffect(() => {
    if (!isOpen) return;
    setAutoCommitDirty(false);
    void fetchInspection();
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !mergingRef.current) {
        event.preventDefault();
        onCloseRef.current();
      }
      if (event.key !== "Tab") return;
      const focusable = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>("button:not(:disabled), input:not(:disabled), [tabindex]:not([tabindex='-1'])") ?? []);
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    requestAnimationFrame(() => dialogRef.current?.querySelector<HTMLElement>("button, input")?.focus());
    return () => {
      requestGeneration.current++;
      window.removeEventListener("keydown", onKeyDown);
      previousFocus?.focus();
    };
  }, [fetchInspection, isOpen]);

  const canConfirm = useMemo(() => {
    if (!inspection?.available) return false;
    if (inspection.canMerge) return true;
    return inspection.blockers.length > 0 && inspection.blockers.every((blocker) => blocker.code === "worktree_dirty" && autoCommitDirty);
  }, [autoCommitDirty, inspection]);

  const handleMerge = async () => {
    if (!inspection || !tabId || merging || !canConfirm) return;
    const generation = ++requestGeneration.current;
    mergingRef.current = true;
    setMerging(true);
    setError(null);
    try {
      const refreshedRaw = await app.InspectWorktreeMerge(tabId);
      if (generation !== requestGeneration.current) return;
      const refreshed: WorktreeMergeInspection = {
        ...refreshedRaw,
        changedFiles: refreshedRaw.changedFiles ?? [], conflictFiles: refreshedRaw.conflictFiles ?? [],
        blockers: refreshedRaw.blockers ?? [], cleanupBlockers: refreshedRaw.cleanupBlockers ?? [],
      };
      if (inspectionIdentity(refreshed) !== inspectionIdentity(inspection)) {
        setInspection(refreshed);
        setError(t("worktree.stateChanged"));
        return;
      }
      if (!refreshed.targetBranch || !refreshed.targetHead || !refreshed.worktreeHead || !refreshed.worktreeStateToken) {
        setInspection(refreshed);
        setError(refreshed.reason || t("worktree.mergeUnavailable"));
        return;
      }
      const result = await app.MergeWorktreeBack({
        tabId,
        expectedTargetBranch: refreshed.targetBranch,
        expectedTargetHead: refreshed.targetHead,
        expectedWorktreeHead: refreshed.worktreeHead,
        expectedWorktreeStateToken: refreshed.worktreeStateToken,
        autoCommitDirty,
      });
      if (generation !== requestGeneration.current) return;
      if (!result.merged) {
        setError(result.error || t("worktree.mergeFailed"));
        return;
      }
      await onMerged?.(result);
      if (generation === requestGeneration.current) onClose();
    } catch (caught: unknown) {
      if (generation === requestGeneration.current) setError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      if (generation === requestGeneration.current) setMerging(false);
      mergingRef.current = false;
    }
  };

  if (!isOpen) return null;

  return (
    <div className="management-modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget && !merging) onClose(); }}>
      <section ref={dialogRef} className="management-modal worktree-merge-modal" role="dialog" aria-modal="true" aria-labelledby="worktree-merge-title" aria-describedby="worktree-merge-summary">
        <header className="management-modal__head">
          <div>
            <div id="worktree-merge-title" className="management-modal__title worktree-merge__title"><GitMerge size={16} aria-hidden="true" />{t("worktree.mergeTitle")}</div>
            <div id="worktree-merge-summary" className="management-modal__summary">{t("worktree.mergeSubtitle")}</div>
          </div>
          <div className="management-modal__actions"><ModalCloseButton label={t("common.close")} onClick={onClose} disabled={merging} /></div>
        </header>

        <div className="worktree-merge__body">
          {loading ? (
            <div className="worktree-merge__loading" role="status"><Loader2 className="spin" size={18} aria-hidden="true" />{t("worktree.inspecting")}</div>
          ) : inspection ? (
            <>
              <div className="worktree-merge__route">
                <span><GitBranch size={14} aria-hidden="true" />{inspection.worktreeBranch || "worktree"}</span>
                <ArrowRight size={14} aria-hidden="true" />
                <span><GitBranch size={14} aria-hidden="true" />{inspection.targetBranch || "—"}</span>
              </div>
              <div className="worktree-merge__heads"><span>{t("worktree.aheadBehind", { ahead: inspection.aheadCount, behind: inspection.behindCount })}</span><span>{inspection.worktreeHead?.slice(0, 12)} → {inspection.targetHead?.slice(0, 12)}</span></div>
              <div className="worktree-merge__stats">
                <div><span>{t("worktree.filesChanged")}</span><strong>{inspection.filesChanged}</strong></div>
                <div><span>{t("worktree.insertions")}</span><strong className="worktree-merge__positive">+{inspection.insertions}</strong></div>
                <div><span>{t("worktree.deletions")}</span><strong className="worktree-merge__negative">-{inspection.deletions}</strong></div>
              </div>
              {inspection.blockers.length > 0 ? <BlockerList blockers={inspection.blockers} /> : (
                <div className="worktree-merge__ready"><CheckCircle2 size={16} aria-hidden="true" />{inspection.alreadyMerged ? t("worktree.alreadyMerged") : t("worktree.cleanMergeReady")}</div>
              )}
              {inspection.changedFiles.length > 0 && (
                <div className="worktree-merge__files">
                  <div className="worktree-merge__section-title">{t("worktree.changedFilesList")}</div>
                  <div className="worktree-merge__file-list">{inspection.changedFiles.map((file) => <div key={file}><FileText size={12} aria-hidden="true" /><span>{file}</span></div>)}</div>
                </div>
              )}
              {inspection.cleanupBlockers.filter((item) => item.code !== "not_merged").length > 0 && (
                <div className="worktree-merge__cleanup-note">
                  <div className="worktree-merge__section-title">{t("worktree.cleanupBlocked")}</div>
                  <BlockerList blockers={inspection.cleanupBlockers.filter((item) => item.code !== "not_merged")} />
                </div>
              )}
              {inspection.worktreeDirty && (
                <label className="worktree-merge__option">
                  <input type="checkbox" checked={autoCommitDirty} onChange={(event) => setAutoCommitDirty(event.target.checked)} disabled={merging} />
                  <span><strong>{t("worktree.optionAutoCommit")}</strong><small>{t("worktree.optionAutoCommitHint")}</small></span>
                </label>
              )}
            </>
          ) : null}
          {error && <div className="worktree-merge__error" role="alert"><AlertTriangle size={16} aria-hidden="true" /><span>{error}</span><button type="button" onClick={() => void fetchInspection()} disabled={merging}>{t("common.retry")}</button></div>}
        </div>

        <footer className="worktree-merge__actions">
          <button type="button" className="btn btn--secondary" onClick={onClose} disabled={merging}>{t("common.cancel")}</button>
          <button type="button" className="btn btn--primary worktree-merge__confirm" onClick={() => void handleMerge()} disabled={loading || merging || !canConfirm}>
            {merging ? <><Loader2 className="spin" size={14} aria-hidden="true" />{t("worktree.merging")}</> : <><GitMerge size={14} aria-hidden="true" />{t("worktree.confirmMerge")}</>}
          </button>
        </footer>
      </section>
    </div>
  );
}
