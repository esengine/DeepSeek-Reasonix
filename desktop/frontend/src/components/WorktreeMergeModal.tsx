import { useState, useEffect, useCallback } from "react";
import { GitMerge, GitBranch, AlertTriangle, CheckCircle2, FileText, Loader2, ArrowRight } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { WorktreeMergeInspection, WorktreeMergeResult } from "../lib/types";
import { ModalCloseButton } from "./ModalCloseButton";

interface WorktreeMergeModalProps {
  tabId: string;
  isOpen: boolean;
  onClose: () => void;
  onMerged?: (result: WorktreeMergeResult) => void;
}

export function WorktreeMergeModal({
  tabId,
  isOpen,
  onClose,
  onMerged,
}: WorktreeMergeModalProps) {
  const t = useT();
  const [loading, setLoading] = useState(true);
  const [merging, setMerging] = useState(false);
  const [inspection, setInspection] = useState<WorktreeMergeInspection | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [autoCommitDirty, setAutoCommitDirty] = useState(true);
  const [removeWorktree, setRemoveWorktree] = useState(true);
  const [deleteBranch, setDeleteBranch] = useState(true);

  const fetchInspection = useCallback(async () => {
    if (!tabId) return;
    setLoading(true);
    setError(null);
    try {
      const res = await app.InspectWorktreeMerge(tabId);
      setInspection(res);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [tabId]);

  useEffect(() => {
    if (isOpen) {
      void fetchInspection();
    }
  }, [isOpen, fetchInspection]);

  const handleMerge = async () => {
    if (!tabId || merging) return;
    setMerging(true);
    setError(null);
    try {
      const res = await app.MergeWorktreeBack(tabId, autoCommitDirty, removeWorktree, deleteBranch);
      if (res.merged) {
        onMerged?.(res);
        onClose();
      } else {
        setError(res.error || t("worktree.mergeFailed"));
      }
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setMerging(false);
    }
  };

  if (!isOpen) return null;

  return (
    <div className="management-modal-backdrop" onClick={onClose}>
      <div
        className="management-modal recovery-lineage-dialog worktree-merge-modal"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-labelledby="worktree-merge-title"
      >
        <div className="management-modal__head">
          <div>
            <div id="worktree-merge-title" className="management-modal__title">
              <GitMerge size={16} aria-hidden="true" />
              <span>{t("worktree.mergeTitle")}</span>
            </div>
            <div className="management-modal__summary">
              {t("worktree.mergeSubtitle")}
            </div>
          </div>
          <div className="management-modal__actions">
            <ModalCloseButton label={t("common.close")} onClick={onClose} />
          </div>
        </div>

        <div className="worktree-merge-modal__body" style={{ padding: "18px 20px", display: "flex", flexDirection: "column", gap: "16px" }}>
          {loading ? (
            <div style={{ display: "flex", alignItems: "center", justifyContent: "center", padding: "40px 0", gap: "10px", color: "var(--fg-faint)" }}>
              <Loader2 className="spin" size={18} />
              <span>{t("worktree.inspecting")}</span>
            </div>
          ) : error ? (
            <div style={{ display: "flex", alignItems: "center", gap: "10px", padding: "12px 14px", background: "var(--bg-warn)", border: "1px solid var(--border-warn)", borderRadius: "8px", color: "var(--fg-warn)" }}>
              <AlertTriangle size={16} />
              <span>{error}</span>
            </div>
          ) : inspection ? (
            <>
              {/* Branch Route Card */}
              <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "12px 16px", background: "var(--bg-elev)", borderRadius: "8px", border: "1px solid var(--border)" }}>
                <div style={{ display: "flex", alignItems: "center", gap: "8px", fontSize: "var(--text-sm)", fontWeight: 600 }}>
                  <GitBranch size={14} className="text-muted" />
                  <span style={{ color: "var(--primary)" }}>{inspection.worktreeBranch || "worktree"}</span>
                </div>
                <ArrowRight size={14} style={{ color: "var(--fg-faint)" }} />
                <div style={{ display: "flex", alignItems: "center", gap: "8px", fontSize: "var(--text-sm)", fontWeight: 600 }}>
                  <GitBranch size={14} className="text-muted" />
                  <span>{inspection.targetBranch || "main"}</span>
                </div>
              </div>

              {/* Stats Bar */}
              <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: "10px" }}>
                <div style={{ padding: "10px", background: "var(--bg-elev)", borderRadius: "6px", textAlign: "center", border: "1px solid var(--border)" }}>
                  <div style={{ fontSize: "var(--text-xs)", color: "var(--fg-faint)" }}>{t("worktree.filesChanged")}</div>
                  <div style={{ fontSize: "var(--text-base)", fontWeight: 700, marginTop: "2px" }}>{inspection.filesChanged}</div>
                </div>
                <div style={{ padding: "10px", background: "var(--bg-elev)", borderRadius: "6px", textAlign: "center", border: "1px solid var(--border)" }}>
                  <div style={{ fontSize: "var(--text-xs)", color: "var(--fg-faint)" }}>{t("worktree.insertions")}</div>
                  <div style={{ fontSize: "var(--text-base)", fontWeight: 700, marginTop: "2px", color: "var(--fg-success, #10b981)" }}>+{inspection.insertions}</div>
                </div>
                <div style={{ padding: "10px", background: "var(--bg-elev)", borderRadius: "6px", textAlign: "center", border: "1px solid var(--border)" }}>
                  <div style={{ fontSize: "var(--text-xs)", color: "var(--fg-faint)" }}>{t("worktree.deletions")}</div>
                  <div style={{ fontSize: "var(--text-base)", fontWeight: 700, marginTop: "2px", color: "var(--fg-danger, #ef4444)" }}>-{inspection.deletions}</div>
                </div>
              </div>

              {/* Conflict Status */}
              {inspection.hasConflicts ? (
                <div style={{ display: "flex", alignItems: "flex-start", gap: "10px", padding: "12px 14px", background: "rgba(239, 68, 68, 0.1)", border: "1px solid rgba(239, 68, 68, 0.3)", borderRadius: "8px", color: "var(--fg-danger, #ef4444)" }}>
                  <AlertTriangle size={16} style={{ flexShrink: 0, marginTop: "2px" }} />
                  <div>
                    <div style={{ fontWeight: 600, fontSize: "var(--text-sm)" }}>{t("worktree.conflictDetected")}</div>
                    <div style={{ fontSize: "var(--text-xs)", marginTop: "2px", opacity: 0.9 }}>{t("worktree.conflictNotice", { count: inspection.conflictFiles?.length || 1 })}</div>
                  </div>
                </div>
              ) : (
                <div style={{ display: "flex", alignItems: "center", gap: "10px", padding: "10px 14px", background: "rgba(16, 185, 129, 0.1)", border: "1px solid rgba(16, 185, 129, 0.25)", borderRadius: "8px", color: "var(--fg-success, #10b981)", fontSize: "var(--text-sm)" }}>
                  <CheckCircle2 size={16} />
                  <span>{t("worktree.cleanMergeReady")}</span>
                </div>
              )}

              {/* Changed Files List */}
              {inspection.changedFiles && inspection.changedFiles.length > 0 && (
                <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
                  <div style={{ fontSize: "var(--text-xs)", fontWeight: 600, color: "var(--fg-faint)" }}>{t("worktree.changedFilesList")}</div>
                  <div style={{ maxHeight: "140px", overflowY: "auto", background: "var(--bg-elev)", borderRadius: "6px", border: "1px solid var(--border)", padding: "6px 8px" }}>
                    {inspection.changedFiles.map((file, idx) => (
                      <div key={idx} style={{ display: "flex", alignItems: "center", gap: "6px", padding: "4px 6px", fontSize: "var(--text-xs)", color: "var(--fg)" }}>
                        <FileText size={12} className="text-muted" />
                        <span style={{ fontFamily: "var(--font-mono)", wordBreak: "break-all" }}>{file}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Options */}
              <div style={{ display: "flex", flexDirection: "column", gap: "8px", padding: "10px 12px", background: "var(--bg-elev)", borderRadius: "6px", border: "1px solid var(--border)" }}>
                <label style={{ display: "flex", alignItems: "center", gap: "8px", fontSize: "var(--text-xs)", cursor: "pointer" }}>
                  <input
                    type="checkbox"
                    checked={autoCommitDirty}
                    onChange={(e) => setAutoCommitDirty(e.target.checked)}
                  />
                  <span>{t("worktree.optionAutoCommit")}</span>
                </label>
                <label style={{ display: "flex", alignItems: "center", gap: "8px", fontSize: "var(--text-xs)", cursor: "pointer" }}>
                  <input
                    type="checkbox"
                    checked={removeWorktree}
                    onChange={(e) => {
                      setRemoveWorktree(e.target.checked);
                      if (!e.target.checked) setDeleteBranch(false);
                    }}
                  />
                  <span>{t("worktree.optionRemoveWorktree")}</span>
                </label>
                {removeWorktree && (
                  <label style={{ display: "flex", alignItems: "center", gap: "8px", fontSize: "var(--text-xs)", paddingLeft: "20px", cursor: "pointer" }}>
                    <input
                      type="checkbox"
                      checked={deleteBranch}
                      onChange={(e) => setDeleteBranch(e.target.checked)}
                    />
                    <span>{t("worktree.optionDeleteBranch")}</span>
                  </label>
                )}
              </div>
            </>
          ) : null}
        </div>

        <div style={{ display: "flex", alignItems: "center", justifyContent: "flex-end", gap: "10px", padding: "14px 20px", borderTop: "1px solid var(--border)", background: "var(--bg-elev)" }}>
          <button
            type="button"
            className="btn btn--secondary"
            onClick={onClose}
            disabled={merging}
          >
            {t("common.cancel")}
          </button>
          <button
            type="button"
            className="btn btn--primary"
            onClick={() => void handleMerge()}
            disabled={loading || merging || !inspection || inspection.hasConflicts}
            style={{ display: "flex", alignItems: "center", gap: "6px" }}
          >
            {merging ? (
              <>
                <Loader2 className="spin" size={14} />
                <span>{t("worktree.merging")}</span>
              </>
            ) : (
              <>
                <GitMerge size={14} />
                <span>{t("worktree.confirmMerge")}</span>
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
}
