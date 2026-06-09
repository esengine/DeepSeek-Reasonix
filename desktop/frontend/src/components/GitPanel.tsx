import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Check,
  ChevronDown,
  GitBranch,
  RefreshCw,
  RotateCcw,
  Search,
  X,
} from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { cleanGitDiff } from "../lib/diff";
import { CodeViewer } from "./CodeViewer";
import { AnchoredPopover } from "./AnchoredPopover";
import type { GitCommitView, GitCommitDetailView, WorkspaceChangesView } from "../lib/types";

function formatCommitDate(date: string): { relative: string; full: string } {
  const d = new Date(date);
  if (isNaN(d.getTime())) return { relative: date, full: date };
  const now = Date.now();
  const diff = now - d.getTime();
  const mins = Math.floor(diff / 60000);
  const hours = Math.floor(diff / 3600000);
  const days = Math.floor(diff / 86400000);
  const relative =
    mins < 1 ? "just now" : mins < 60 ? `${mins}m ago` : hours < 24 ? `${hours}h ago` : `${days}d ago`;
  const full =
    d.getFullYear() + "-" +
    String(d.getMonth() + 1).padStart(2, "0") + "-" +
    String(d.getDate()).padStart(2, "0") + " " +
    String(d.getHours()).padStart(2, "0") + ":" +
    String(d.getMinutes()).padStart(2, "0");
  return { relative, full };
}

function gitBadge(status: string): { label: string; color: string } {
  if (status === "??") return { label: "U", color: "var(--warn)" };
  if (status.startsWith("D") || status[1] === "D") return { label: "D", color: "var(--err)" };
  if (status.startsWith("A")) return { label: "A", color: "#4caf50" };
  if (status.startsWith("R")) return { label: "R", color: "#a78bfa" };
  return { label: "M", color: "var(--accent)" };
}

// Static style constants — avoid object recreation on every render
const changeRowInnerStyle = { display: "flex", alignItems: "baseline", gap: 6, flex: 1, minWidth: 0, overflow: "hidden" } as const;
const changeNameStyle = { fontWeight: 400, fontSize: 12.5, flexShrink: 0 } as const;
const changePathStyle = { color: "var(--fg-faint)", fontSize: 10.5, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" } as const;
const discardBtnStyle = { flexShrink: 0, border: "none", background: "transparent", color: "var(--fg-faint)", cursor: "pointer", padding: 2, borderRadius: 3 } as const;
const dotContainerStyle = { width: 12, flexShrink: 0, display: "flex", justifyContent: "center", marginLeft: -7, paddingTop: 10 } as const;
const contentAreaStyle = { flex: 1, minWidth: 0 } as const;
const commitTitleStyle = { fontWeight: 400, fontSize: 13, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" } as const;
const fileNameStyle = { fontWeight: 400, fontSize: 12.5, flexShrink: 0 } as const;
const filePathStyle = { color: "var(--fg-faint)", fontSize: 10.5, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" } as const;

function GitPanel({ refreshKey }: { refreshKey?: number }) {
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
  const [discardingFile, setDiscardingFile] = useState<string | null>(null);
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

  const discardFile = useCallback(
    async (path: string) => {
      setDiscardingFile(path);
      try {
        await app.GitDiscardFile(path);
        await loadChanges();
      } catch {
        // error handled via refresh
      } finally {
        setDiscardingFile(null);
      }
    },
    [loadChanges],
  );

  useEffect(() => {
    void loadGitHistory();
    void loadChanges();
  }, [loadGitHistory, loadChanges, refreshKey]);

  const q = filter.trim().toLowerCase();
  const filteredHistory = useMemo(
    () =>
      gitHistory.filter((c) => {
        if (!q) return true;
        return [c.message, c.author, c.hash].join(" ").toLowerCase().includes(q);
      }),
    [gitHistory, q],
  );
  // Only show files tracked by git (ignore session-only files).
  const gitOnlyFiles = useMemo(
    () => (changes?.files ?? []).filter((f) => f.sources.includes("git")),
    [changes?.files],
  );
  const filteredChanges = useMemo(
    () =>
      gitOnlyFiles.filter((f) => {
        if (!q) return true;
        return [f.path, f.gitStatus ?? ""].join(" ").toLowerCase().includes(q);
      }),
    [gitOnlyFiles, q],
  );

  return (
    <div className="workspace-files" style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      <div className="workspace-search" style={{ margin: "6px 10px 8px" }}>
        <Search size={14} />
        <input
          placeholder="Filter commits & files…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        {filter && (
          <button className="workspace-iconbtn" onClick={() => setFilter("")}>
            <X size={12} />
          </button>
        )}
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
        {/* Working tree changes */}
        {changes && (
          <div style={{ marginBottom: 12 }}>
            <div className="mem-section__title" style={{ padding: "8px 4px 4px", margin: 0 }}>
              Changes ({filteredChanges.length})
            </div>
            {filteredChanges.length === 0 ? (
              <div className="workspace-empty" style={{ fontSize: 12, padding: "4px 0" }}>
                {q ? "No matching files" : "No changes"}
              </div>
            ) : (
              filteredChanges.map((f) => {
                const { label, color } = gitBadge(f.gitStatus ?? "");
                const isDiscarding = discardingFile === f.path;
                const canDiscard = f.gitStatus && f.gitStatus !== "??" && !f.gitStatus.startsWith("A") && !f.gitStatus.includes("D");
                return (
                  <div
                    key={f.path}
                    className="workspace-changes__file"
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 6,
                      padding: "3px 6px",
                      borderRadius: 4,
                      overflow: "hidden",
                      cursor: "pointer",
                    }}
                  >
                    <span style={changeRowInnerStyle}>
                      <span style={changeNameStyle}>{f.path.split("/").pop()}</span>
                      <span style={changePathStyle}>{f.path}</span>
                    </span>
                    {canDiscard && (
                      <button
                        type="button"
                        onClick={(e) => { e.stopPropagation(); void discardFile(f.path); }}
                        disabled={isDiscarding}
                        title="Discard changes"
                        className="workspace-discard-btn"
                        style={discardBtnStyle}
                      >
                        <RotateCcw size={12} />
                      </button>
                    )}
                    <span style={{
                      fontSize: 10.5,
                      fontWeight: 700,
                      color,
                      minWidth: 16,
                      textAlign: "center",
                      fontFamily: "var(--mono)",
                      flexShrink: 0,
                    }}>{label}</span>
                  </div>
                );
              })
            )}
          </div>
        )}

        {/* Commit history */}
        <div className="mem-section__title" style={{ padding: "4px 2px", margin: 0, borderTop: "1px solid var(--border-soft)" }}>
          History ({filteredHistory.length})
        </div>
        {loadingHistory ? (
          <div className="workspace-empty">{t("workspace.loading")}</div>
        ) : filteredHistory.length === 0 ? (
          <div className="workspace-empty">{t("workspace.noChanges")}</div>
        ) : (
          <div style={{ borderLeft: "2px solid var(--border-soft)", marginLeft: 6, paddingBottom: 4 }}>
            {filteredHistory.map((commit, idx) => {
              const d = formatCommitDate(commit.date);
              const isExpanded = expandedCommit === commit.hash;
              const isLast = idx === filteredHistory.length - 1;
              return (
              <div key={commit.hash} style={{ display: "flex", alignItems: "flex-start", paddingBottom: isLast ? 0 : 8 }}>
                {/* dot — centered on the 2px borderLeft line */}
                <div style={dotContainerStyle}>
                  <span style={{
                    width: isExpanded ? 10 : 8,
                    height: isExpanded ? 10 : 8,
                    borderRadius: "50%",
                    background: isExpanded ? "var(--accent)" : "var(--fg-faint)",
                    flexShrink: 0,
                    transition: "all 0.12s",
                  }} />
                </div>

                {/* Content */}
                <div style={contentAreaStyle}>
                  <button
                    type="button"
                    onClick={() => void toggleCommit(commit.hash)}
                    style={{
                      display: "flex",
                      flexDirection: "column",
                      width: "100%",
                      border: "none",
                      background: isExpanded ? "var(--card-bg)" : "transparent",
                      borderRadius: 6,
                      padding: "4px 6px 2px",
                      textAlign: "left",
                      font: "inherit",
                      color: "var(--fg)",
                      cursor: "pointer",
                    }}
                  >
                    <span style={commitTitleStyle}>
                      {commit.message}
                    </span>
                    {isExpanded && (
                      <div style={{ marginTop: 6, paddingTop: 6, borderTop: "1px solid var(--border-soft)", fontSize: 12 }}>
                        <div style={{ color: "var(--fg-faint)", marginBottom: 8 }}>
                          <span>{commit.author}, {d.relative} <span style={{ opacity: 0.7 }}>({d.full})</span> · <span style={{ fontFamily: "var(--mono)", fontSize: 11 }}>{commit.hash.substring(0, 7)}</span></span>
                        </div>
                        {loadingCommit ? (
                          <div className="workspace-empty">{t("workspace.loading")}</div>
                        ) : commitDetail?.diff ? (
                          <CodeViewer value={cleanGitDiff(commitDetail.diff)} language="diff" />
                        ) : commitDetail?.files ? (
                          <div className="workspace-git-history__files">
                            {commitDetail.files.map((file) => (
                              <div key={file} className="workspace-git-history__file">
                                <span style={fileNameStyle}>{file.split("/").pop()}</span>
                                <span style={filePathStyle}>{file.split("/").slice(0, -1).join("/")}</span>
                              </div>
                            ))}
                          </div>
                        ) : (
                          <div className="workspace-empty">No details available</div>
                        )}
                      </div>
                    )}
                  </button>
                </div>
              </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

export { GitPanel };
