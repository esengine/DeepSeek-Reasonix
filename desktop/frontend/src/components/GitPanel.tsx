import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Check,
  ChevronDown,
  ChevronRight,
  FileText,
  GitBranch,
  RefreshCw,
  Search,
  X,
} from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { cleanGitDiff } from "../lib/diff";
import { CodeViewer } from "./CodeViewer";
import { AnchoredPopover } from "./AnchoredPopover";
import type { GitCommitView, GitCommitDetailView, WorkspaceChangesView } from "../lib/types";

function formatCommitDate(date: string): string {
  // ISO → locale, or "X days ago"
  const d = new Date(date);
  if (isNaN(d.getTime())) return date;
  const now = Date.now();
  const diff = now - d.getTime();
  const days = Math.floor(diff / 86400000);
  if (days === 0) return "today";
  if (days === 1) return "yesterday";
  if (days < 14) return `${days} days ago`;
  return d.toLocaleDateString();
}

function GitPanel() {
  const t = useT();
  const [gitHistory, setGitHistory] = useState<GitCommitView[]>([]);
  const [loadingHistory, setLoadingHistory] = useState(false);
  const [expandedCommit, setExpandedCommit] = useState<string | null>(null);
  const [commitDetail, setCommitDetail] = useState<GitCommitDetailView | null>(null);
  const [loadingCommit, setLoadingCommit] = useState(false);
  const [changes, setChanges] = useState<WorkspaceChangesView | null>(null);
  const [loadingChanges, setLoadingChanges] = useState(false);
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [branchList, setBranchList] = useState<string[]>([]);
  const [switchingBranch, setSwitchingBranch] = useState(false);
  const [filter, setFilter] = useState("");
  const branchBtnRef = useRef<HTMLButtonElement>(null);
  const changesRequestRef = useRef(0);

  const loadGitHistory = useCallback(async () => {
    setLoadingHistory(true);
    try {
      const result = await app.WorkspaceGitHistory("");
      setGitHistory(result || []);
    } catch {
      setGitHistory([]);
    } finally {
      setLoadingHistory(false);
    }
  }, []);

  const toggleCommit = useCallback((hash: string) => {
    setExpandedCommit((prev) => (prev === hash ? null : hash));
  }, []);

  // Fetch commit detail when expanded
  useEffect(() => {
    if (!expandedCommit) {
      setCommitDetail(null);
      return;
    }
    let live = true;
    setLoadingCommit(true);
    app
      .WorkspaceGitCommitDetail(expandedCommit, "")
      .then((detail) => {
        if (live) setCommitDetail(detail);
      })
      .catch(() => {
        if (live) setCommitDetail(null);
      })
      .finally(() => {
        if (live) setLoadingCommit(false);
      });
    return () => { live = false; };
  }, [expandedCommit]);

  const loadChanges = useCallback(async () => {
    const requestId = changesRequestRef.current + 1;
    changesRequestRef.current = requestId;
    setLoadingChanges(true);
    try {
      const next = await app.WorkspaceChanges();
      if (changesRequestRef.current === requestId) setChanges(next);
    } catch {
      // git not available
    } finally {
      setLoadingChanges(false);
    }
  }, []);

  const openBranchMenu = useCallback(async () => {
    try {
      const branches = await app.GitBranches();
      setBranchList(branches);
    } catch {
      setBranchList([]);
    }
    setBranchMenuOpen((prev) => !prev);
  }, []);

  const switchBranch = useCallback(
    async (branch: string) => {
      if (branch === changes?.gitBranch) return;
      setSwitchingBranch(true);
      try {
        await app.GitCheckout(branch);
        await loadChanges();
        await loadGitHistory();
      } catch {
        // error handled via changes view
      } finally {
        setSwitchingBranch(false);
        setBranchMenuOpen(false);
      }
    },
    [changes?.gitBranch, loadChanges, loadGitHistory],
  );

  useEffect(() => {
    void loadGitHistory();
    void loadChanges();
  }, [loadGitHistory, loadChanges]);

  const q = filter.trim().toLowerCase();
  const filteredHistory = useMemo(
    () =>
      gitHistory.filter((c) => {
        if (!q) return true;
        return [c.message, c.author, c.hash].join(" ").toLowerCase().includes(q);
      }),
    [gitHistory, q],
  );

  return (
    <div className="workspace-files" style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      <div className="workspace-files__tools">
        <div className="workspace-search" style={{ flex: 1 }}>
          <Search size={14} />
          <input
            placeholder="Filter commits…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
          {filter && (
            <button className="workspace-iconbtn" onClick={() => setFilter("")}>
              <X size={12} />
            </button>
          )}
        </div>
      </div>

      {changes?.gitBranch && (
        <div className="workspace-branch-indicator" style={{ margin: "0 10px 6px" }}>
          <GitBranch size={13} />
          <button
            ref={branchBtnRef}
            className="workspace-branch-name"
            onClick={openBranchMenu}
          >
            <span>{changes.gitBranch}</span>
            <ChevronDown size={11} />
          </button>
          <AnchoredPopover
            open={branchMenuOpen}
            anchorRef={branchBtnRef}
            onClose={() => setBranchMenuOpen(false)}
            className="workspace-branch-menu"
            placement="bottom"
            align="start"
            offset={4}
          >
            <div className="workspace-branch-menu__inner">
              {branchList.length === 0 ? (
                <div className="workspace-branch-menu__loading">{t("workspace.loading")}</div>
              ) : (
                branchList.map((b) => (
                  <button
                    key={b}
                    type="button"
                    className={`workspace-branch-menu__item${b === changes.gitBranch ? " workspace-branch-menu__item--active" : ""}`}
                    onClick={() => switchBranch(b)}
                    disabled={switchingBranch}
                  >
                    {b === changes.gitBranch && <Check size={13} />}
                    <span>{b}</span>
                  </button>
                ))
              )}
            </div>
          </AnchoredPopover>
          <button
            className="workspace-branch-refresh"
            onClick={loadChanges}
            disabled={loadingChanges}
            title={t("workspace.refreshChanges")}
          >
            <RefreshCw size={12} className={loadingChanges ? "spinning" : ""} />
          </button>
        </div>
      )}

      {changes && !changes.gitAvailable && changes.gitErr && (
        <div className="workspace-note workspace-note--compact" style={{ margin: "0 10px 6px" }}>
          {t("workspace.gitUnavailable")}
        </div>
      )}

      <div style={{ flex: 1, overflowY: "auto", padding: "0 8px 8px" }}>
        {loadingHistory ? (
          <div className="workspace-empty">{t("workspace.loading")}</div>
        ) : filteredHistory.length === 0 ? (
          <div className="workspace-empty">{t("workspace.noChanges")}</div>
        ) : (
          <div className="workspace-git-history__list">
            {filteredHistory.map((commit) => (
              <div key={commit.hash} className={`workspace-git-history__item${expandedCommit === commit.hash ? " workspace-git-history__item--expanded" : ""}`}>
                <button
                  className="workspace-git-history__head"
                  onClick={() => void toggleCommit(commit.hash)}
                >
                  <div className="workspace-git-history__head-top">
                    {expandedCommit === commit.hash ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                    <span className="workspace-git-history__message">{commit.message}</span>
                  </div>
                  <div className="workspace-git-history__head-bottom">
                    <span className="workspace-git-history__author">{commit.author}</span>
                    <span className="workspace-git-history__date">
                      {formatCommitDate(commit.date)} <span className="workspace-git-history__hash">{commit.hash.substring(0, 7)}</span>
                    </span>
                  </div>
                </button>
                {expandedCommit === commit.hash && (
                  <div className="workspace-git-history__detail">
                    {loadingCommit ? (
                      <div className="workspace-empty">{t("workspace.loading")}</div>
                    ) : commitDetail?.diff ? (
                      <CodeViewer value={cleanGitDiff(commitDetail.diff)} language="diff" />
                    ) : commitDetail?.files ? (
                      <div className="workspace-git-history__files">
                        {commitDetail.files.map((file) => (
                          <div key={file} className="workspace-git-history__file">
                            <FileText size={14} /> {file}
                          </div>
                        ))}
                      </div>
                    ) : (
                      <div className="workspace-empty">No details available</div>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

export { GitPanel };
