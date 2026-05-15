import hljs from "highlight.js/lib/common";
import htm from "htm";
import { h } from "preact";
import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { ChatMessage, type ChatMsg, ToolCard } from "../components/chat-internals.js";
import { t, useLang } from "../i18n/index.js";
import { TOKEN, api } from "../lib/api.js";
import { showToast } from "../lib/bus.js";
import { parseHunks } from "../lib/diff-parser.js";
// ChatStatusBar — inlined (mirrors chat.ts pattern)
import {
  type OpenFile,
  type TreeNode,
  getFileIcon,
  useFileTreeState,
  useProjectTree,
} from "../lib/file-tree.js";
import { type LineComment, type LineCommentDraft, useLineComments } from "../lib/line-comments.js";
import { useReviewDiffs } from "../lib/review-diffs.js";

const html = htm.bind(h);

// Diff rendering helpers — render patch to HTML, bypassing Preact VDOM
interface DE {
  kind: "context" | "ins" | "del";
  text: string;
}
interface DP {
  left: string | null;
  right: string | null;
  kind: "context" | "change" | "del" | "ins";
}

function escapeAttr(s: string): string {
  return s.replace(
    /["&<>]/g,
    (c) => ({ '"': "&quot;", "&": "&amp;", "<": "&lt;", ">": "&gt;" })[c]!,
  );
}

function lineDiff(a: string[], b: string[]): DE[] {
  const m = a.length,
    n = b.length;
  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array<number>(n + 1).fill(0));
  for (let i = 1; i <= m; i++)
    for (let j = 1; j <= n; j++)
      dp[i]![j] =
        a[i - 1] === b[j - 1] ? dp[i - 1]![j - 1]! + 1 : Math.max(dp[i - 1]![j]!, dp[i]![j - 1]!);
  const out: DE[] = [];
  let i = m,
    j = n;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && a[i - 1] === b[j - 1]) {
      out.push({ kind: "context", text: a[i - 1]! });
      i--;
      j--;
    } else if (j > 0 && (i === 0 || dp[i]![j - 1]! >= dp[i - 1]![j]!)) {
      out.push({ kind: "ins", text: b[j - 1]! });
      j--;
    } else {
      out.push({ kind: "del", text: a[i - 1]! });
      i--;
    }
  }
  return out.reverse();
}

function pairDiffRows(diff: DE[]): DP[] {
  const rows: DP[] = [];
  let k = 0;
  while (k < diff.length) {
    const e = diff[k]!;
    if (e.kind === "context") {
      rows.push({ left: e.text, right: e.text, kind: "context" });
      k++;
      continue;
    }
    const d: string[] = [],
      ins: string[] = [];
    while (k < diff.length && diff[k]!.kind === "del") d.push(diff[k]!.text), k++;
    while (k < diff.length && diff[k]!.kind === "ins") ins.push(diff[k]!.text), k++;
    const p = Math.max(d.length, ins.length);
    for (let i = 0; i < p; i++)
      rows.push({
        left: d[i] ?? null,
        right: ins[i] ?? null,
        kind: d[i] != null && ins[i] != null ? "change" : d[i] != null ? "del" : "ins",
      });
  }
  return rows;
}

function hE(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function renderDiffHtml(patch: string, style: "unified" | "split"): string {
  const hunks = parseHunks(patch);
  if (hunks.length === 0) return "";
  if (style === "unified") {
    let html = "";
    for (const hunk of hunks) {
      html += `<div class="diff-hunk-header">@@ -${hunk.oldStart},${hunk.oldLines} +${hunk.newStart},${hunk.newLines} @@</div>`;
      for (const line of hunk.lines) {
        const cls = line.type === "add" ? "diff-add" : line.type === "del" ? "diff-del" : "";
        const prefix = line.type === "add" ? "+" : line.type === "del" ? "-" : " ";
        html += `<div class="diff-line ${cls}"><span class="diff-ln-old">${line.oldLineNum ?? ""}</span><span class="diff-ln-new">${line.newLineNum ?? ""}</span><span class="diff-prefix">${prefix}</span><span class="diff-content">${hE(line.content)}</span></div>`;
      }
    }
    return html;
  }
  // Split
  const oldLines: string[] = [],
    newLines: string[] = [];
  for (const hunk of hunks) {
    for (const line of hunk.lines) {
      if (line.type === "ctx") {
        oldLines.push(line.content);
        newLines.push(line.content);
      } else if (line.type === "del") oldLines.push(line.content);
      else newLines.push(line.content);
    }
  }
  const diff = lineDiff(oldLines, newLines);
  const rows = pairDiffRows(diff);
  let oldNum = 1,
    newNum = 1;
  let html = `<div class="edit-diff-head"><div class="edit-diff-side edit-diff-side-old"><span class="edit-diff-marker">−</span> Before</div><div class="edit-diff-side edit-diff-side-new"><span class="edit-diff-marker">+</span> After</div></div><div class="edit-diff-body">`;
  for (const row of rows) {
    html += `<div class="edit-diff-row edit-diff-row-${row.kind}">`;
    html += `<div class="edit-diff-cell edit-diff-cell-old">`;
    if (row.left != null) {
      html += `<span class="edit-diff-ln">${oldNum}</span><span class="edit-diff-marker">${row.kind === "del" || row.kind === "change" ? "−" : " "}</span>${hE(row.left)}`;
      oldNum++;
    }
    html += `</div>`;
    html += `<div class="edit-diff-cell edit-diff-cell-new">`;
    if (row.right != null) {
      html += `<span class="edit-diff-ln">${newNum}</span><span class="edit-diff-marker">${row.kind === "ins" || row.kind === "change" ? "+" : " "}</span>${hE(row.right)}`;
      newNum++;
    }
    html += `</div></div>`;
  }
  html += `</div>`;
  return html;
}

export function ChangesPanel() {
  useLang();
  const { tree, loading } = useProjectTree();
  const {
    expanded,
    openFiles,
    activeFilePath,
    activeFile,
    toggleExpand,
    openFile,
    closeFile,
    setActiveFilePath,
  } = useFileTreeState(tree);
  const {
    comments,
    draft,
    startDraft,
    cancelDraft,
    setDraftContent,
    submitDraft,
    commentsForFile,
    deleteComment,
    editComment,
  } = useLineComments();
  const { diffs, modifiedFiles, modifiedCount, reload } = useReviewDiffs();
  const [diffSource, setDiffSource] = useState<"session" | "git" | "checkpoint">("git");
  const [checkpointList, setCheckpointList] = useState<
    Array<{ id: string; name: string; ago: string; fileCount: number }>
  >([]);
  const [selectedCheckpointId, setSelectedCheckpointId] = useState<string | null>(null);
  const [createName, setCreateName] = useState("");
  const [leftPct, setLeftPct] = useState(30);
  const [rightPct, setRightPct] = useState(30);
  const [showOnlyModified, setShowOnlyModified] = useState(false);
  const [reviewMode, setReviewMode] = useState(true);
  const [diffStyle, setDiffStyle] = useState<"unified" | "split">("unified");
  const [reviewHtml, setReviewHtml] = useState("");

  // When no files are open, show review mode (unless clicking a review file)
  const openingFile = useRef(false);
  useEffect(() => {
    if (openFiles.length === 0 && !openingFile.current) setReviewMode(true);
  }, [openFiles]);

  const diffEndpoint =
    diffSource === "checkpoint"
      ? selectedCheckpointId
        ? `/checkpoint-diffs?id=${selectedCheckpointId}`
        : null
      : diffSource === "git"
        ? "/git-diffs"
        : "/review-diffs";

  // Load checkpoint list when switching to checkpoint mode
  useEffect(() => {
    if (diffSource === "checkpoint") {
      api<Array<{ id: string; name: string; ago: string; fileCount: number }>>("/checkpoints")
        .then((list) => setCheckpointList(list))
        .catch(() => setCheckpointList([]));
    }
  }, [diffSource]);

  useEffect(() => {
    if (diffEndpoint) {
      reload(diffEndpoint);
    } else {
      // No checkpoint selected yet — keep empty
      setReviewHtml(
        `<div class="review-empty">${t("changes.reviewEmpty") || "Select a checkpoint to compare"}</div>`,
      );
    }
    void diffEndpoint; // suppress unused
  }, [diffEndpoint, reload]);

  // Build review HTML from diffs — skips Preact VDOM entirely
  useEffect(() => {
    if (diffs.length === 0) {
      const emptyMsg = t("changes.reviewEmpty") || "No changes to review";
      setReviewHtml(`<div class="review-empty">${emptyMsg}</div>`);
      return;
    }
    setReviewHtml(
      diffs
        .map((diff) => {
          const file = hE(diff.file);
          const chev = '<span class="chev">▸</span>';
          const stat = `<span class="stat"><span class="add">+${diff.additions}</span><span class="rem"> -${diff.deletions}</span></span>`;
          const body = diff.patch
            ? `<div class="review-file-body" style="display:none">${renderDiffHtml(diff.patch, diffStyle)}</div>`
            : "";
          return `<div class="review-file-item" data-file="${escapeAttr(file)}"><div class="review-file-header">${chev}<span class="filename">${escapeAttr(file)}</span>${stat}</div>${body}</div>`;
        })
        .join(""),
    );
  }, [diffs, diffStyle, t]);

  // Expand / collapse all
  const expandAll = useCallback(() => {
    document.querySelectorAll(".review-file-body").forEach((el) => {
      (el as HTMLElement).style.display = "";
    });
    document.querySelectorAll(".review-file-header .chev").forEach((el) => {
      el.textContent = "▾";
    });
  }, []);
  const collapseAll = useCallback(() => {
    document.querySelectorAll(".review-file-body").forEach((el) => {
      (el as HTMLElement).style.display = "none";
    });
    document.querySelectorAll(".review-file-header .chev").forEach((el) => {
      el.textContent = "▸";
    });
  }, []);

  const handleLeftResize = useCallback((delta: number) => {
    setLeftPct((prev) => {
      const containerWidth = window.innerWidth;
      const deltaPct = (delta / containerWidth) * 100;
      return Math.max(15, Math.min(50, prev + deltaPct));
    });
  }, []);

  const handleRightResize = useCallback((delta: number) => {
    setRightPct((prev) => {
      const containerWidth = window.innerWidth;
      const deltaPct = (delta / containerWidth) * 100;
      return Math.max(15, Math.min(50, prev - deltaPct));
    });
  }, []);

  const toggleModifiedFilter = useCallback(() => {
    setShowOnlyModified((prev) => !prev);
  }, []);

  const toggleReviewMode = useCallback(() => {
    setReviewMode((prev) => !prev);
  }, []);

  const openReviewWithFilePicker = useCallback(() => {
    setReviewMode(true);
  }, []);

  const handleOpenFile = useCallback(
    async (filePath: string) => {
      // Try to find node in tree first
      const findInTree = (nodes: TreeNode[], path: string): TreeNode | null => {
        for (const n of nodes) {
          if (n.path === path) return n;
          if (n.children) {
            const found = findInTree(n.children, path);
            if (found) return found;
          }
        }
        return null;
      };
      let node = findInTree(tree, filePath);
      // If not found in tree, construct a minimal node
      if (!node) {
        const parts = filePath.split("/");
        const name = parts[parts.length - 1] || filePath;
        node = { path: filePath, name, isDir: false };
      }
      await openFile(node);
    },
    [tree, openFile],
  );

  // Click review file header
  useEffect(() => {
    const handler = (e: Event) => {
      const header = (e.target as HTMLElement).closest(".review-file-header");
      if (!header) return;
      const item = header.closest(".review-file-item") as HTMLElement | null;
      if (!item) return;
      const filePath = item.getAttribute("data-file");
      if (!filePath) return;

      // Toggle diff body
      const body = item.querySelector(".review-file-body") as HTMLElement | null;
      if (body) {
        const isOpen = body.style.display !== "none";
        body.style.display = isOpen ? "none" : "";
        const chev = header.querySelector(".chev") as HTMLElement | null;
        if (chev) chev.textContent = isOpen ? "▸" : "▾";
      }

      // Only toggle expand/collapse — no navigation to editor
    };
    document.addEventListener("click", handler);
    return () => document.removeEventListener("click", handler);
  }, []);

  const activeFileComments = activeFile ? commentsForFile(activeFile.path) : [];

  return html`
    <div class="changes-layout">
      <div class="changes-panel changes-panel-left" style=${{ width: `${leftPct}%` }}>
        <div class="changes-panel-header">
          <span class="glyph">◆</span>
          <span>${t("changes.chatPanelTitle")}</span>
        </div>
        <div class="changes-panel-body">
          <${ChatPane}
            comments=${comments}
            deleteComment=${deleteComment}
          />
        </div>
      </div>

      <${ResizeHandle} onResize=${handleLeftResize} direction="horizontal" />

      <div class="changes-panel changes-panel-center">
        ${
          reviewMode
            ? html`
              <${TabBar}
                reviewTab=${html`<${ReviewTab} count=${modifiedCount()} active=${true} onClick=${toggleReviewMode} />`}
                fileList=${diffs.map((d) => d.file)}
                onOpenFile=${(f: string) => {
                  handleOpenFile(f);
                  setReviewMode(false);
                }}
                onToggleReview=${toggleReviewMode}
                files=${openFiles}
                activePath=${activeFilePath}
                onSelect=${(path: string) => {
                  setActiveFilePath(path);
                  setReviewMode(false);
                }}
                onClose=${closeFile}
              />
              <div class="review-controls" style=${{ display: "flex", alignItems: "center", gap: "8px", padding: "6px 12px", borderBottom: "1px solid var(--bd)", fontSize: "12px" }}>
                <select value=${diffSource} onChange=${(e: Event) => {
                  const v = (e.target as HTMLSelectElement).value as
                    | "session"
                    | "git"
                    | "checkpoint";
                  setDiffSource(v);
                  if (v !== "checkpoint") setSelectedCheckpointId(null);
                }} style=${{ fontSize: "12px", fontWeight: 500, padding: "1px 4px", borderRadius: "3px", background: "var(--bg-elev)", color: "var(--fg-0)", border: "1px solid var(--bd)", cursor: "pointer", outline: "none" }}>
                  <option value="git">${t("changes.diffSourceGit")}</option>
                  <option value="session">${t("changes.diffSourceSession")}</option>
                  <option value="checkpoint">${t("changes.diffSourceCheckpoint")}</option>
                </select>
                ${
                  diffSource !== "checkpoint" || selectedCheckpointId
                    ? html`
                <span style=${{ color: "var(--fg-3)" }}>${modifiedCount()}</span>
                <div style=${{ marginLeft: "auto", display: "flex", alignItems: "center", gap: "4px" }}>
                  <button class=${`toggle-btn ${diffStyle === "unified" ? "active" : ""}`} onClick=${() => setDiffStyle("unified")} style=${{ fontSize: "11px", padding: "2px 6px" }}>${t("changes.diffStyleUnified")}</button>
                  <button class=${`toggle-btn ${diffStyle === "split" ? "active" : ""}`} onClick=${() => setDiffStyle("split")} style=${{ fontSize: "11px", padding: "2px 6px" }}>${t("changes.diffStyleSplit")}</button>
                  <button class="toggle-btn" onClick=${expandAll} style=${{ fontSize: "11px", padding: "2px 6px" }}>${t("changes.expandAll")}</button>
                  <button class="toggle-btn" onClick=${collapseAll} style=${{ fontSize: "11px", padding: "2px 6px" }}>${t("changes.collapseAll")}</button>
                </div>
                `
                    : null
                }
              </div>
              ${
                diffSource === "checkpoint" && selectedCheckpointId
                  ? html`
                <div style=${{ padding: "4px 12px", fontSize: "11px", color: "var(--fg-3)", borderBottom: "1px solid var(--bd)", cursor: "pointer" }}>
                  <span onClick=${() => setSelectedCheckpointId(null)} style=${{ color: "var(--c-brand)", cursor: "pointer" }}>← ${t("changes.backToList")}</span>
                </div>
              `
                  : null
              }
              ${
                diffSource === "checkpoint" && !selectedCheckpointId
                  ? html`
                <div class="checkpoint-picker" style=${{ flex: "1", overflowY: "auto", padding: "8px 12px" }}>
                  <div style=${{ display: "flex", gap: "6px", marginBottom: "8px" }}>
                    <input
                      value=${createName}
                      onInput=${(e: Event) => setCreateName((e.target as HTMLInputElement).value)}
                      placeholder=${t("changes.createPlaceholder")}
                      style=${{ flex: 1, fontSize: "12px", padding: "4px 8px", background: "var(--bg-input)", border: "1px solid var(--bd)", borderRadius: "3px", color: "var(--fg-0)" }}
                    />
                    <button
                      class="primary"
                      onClick=${async () => {
                        const name = createName.trim();
                        if (!name) return;
                        try {
                          await api("/checkpoint-create", { method: "POST", body: { name } });
                          setCreateName("");
                          const list =
                            await api<
                              Array<{ id: string; name: string; ago: string; fileCount: number }>
                            >("/checkpoints");
                          setCheckpointList(list);
                        } catch {
                          alert(t("changes.createFailed"));
                        }
                      }}
                      disabled=${!createName.trim()}
                      style=${{ padding: "5px 12px" }}
                    >${t("changes.createBtn")}</button>
                  </div>
                  ${
                    checkpointList.length === 0
                      ? html`
                    <div class="empty" style=${{ textAlign: "center", margin: "12px" }}>${t("changes.checkpointEmpty")}</div>
                  `
                      : checkpointList.map(
                          (c) => html`
                    <div
                      key=${c.id}
                      class="checkpoint-item"
                      style=${{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "6px 8px", cursor: "pointer", borderRadius: "4px", borderBottom: "1px solid var(--bd)" }}
                      onMouseEnter=${(e: Event) => {
                        (e.currentTarget as HTMLElement).style.background = "var(--bg-hover)";
                      }}
                      onMouseLeave=${(e: Event) => {
                        (e.currentTarget as HTMLElement).style.background = "transparent";
                      }}
                    >
                      <div
                        onClick=${() => {
                          setSelectedCheckpointId(c.id);
                        }}
                        style=${{ display: "flex", flexDirection: "column", gap: "2px", flex: 1 }}
                      >
                        <span style=${{ fontSize: "13px", fontWeight: 500 }}>${c.name}</span>
                        <span style=${{ fontSize: "11px", color: "var(--fg-3)" }}>${c.id.slice(0, 7)} · ${c.fileCount} file${c.fileCount === 1 ? "" : "s"}</span>
                      </div>
                      <div style=${{ display: "flex", alignItems: "center", gap: "8px" }}>
                        <span style=${{ fontSize: "11px", color: "var(--fg-4)" }}>${c.ago}</span>
                        <button
                          onClick=${async (e: Event) => {
                            e.stopPropagation();
                            if (confirm(t("changes.restoreConfirm").replace("{name}", c.name))) {
                              try {
                                await api("/checkpoint-restore", {
                                  method: "POST",
                                  body: { id: c.id },
                                });
                                setSelectedCheckpointId(null);
                                setDiffSource("git");
                              } catch {
                                alert(t("changes.restoreFailed"));
                              }
                            }
                          }}
                          style=${{ fontSize: "11px", padding: "2px 6px", background: "var(--c-brand)", color: "#fff", border: "none", borderRadius: "3px", cursor: "pointer" }}
                        >${t("changes.restoreBtn")}</button>
                        <button
                          onClick=${async (e: Event) => {
                            e.stopPropagation();
                            if (confirm(t("changes.deleteConfirm").replace("{name}", c.name))) {
                              try {
                                await api("/checkpoint-delete", {
                                  method: "POST",
                                  body: { id: c.id },
                                });
                                setCheckpointList((prev) => prev.filter((x) => x.id !== c.id));
                              } catch {
                                alert(t("changes.deleteFailed"));
                              }
                            }
                          }}
                          style=${{ fontSize: "11px", padding: "2px 6px", color: "var(--fg-3)", border: "1px solid var(--bd)", borderRadius: "3px", cursor: "pointer", background: "transparent" }}
                        >${t("changes.deleteBtn")}</button>
                      </div>
                    </div>
                  `,
                        )
                  }
                </div>
              `
                  : null
              }
              <div class="review-diff-view" style=${{ flex: "1", overflowY: "auto" }}>
                <div class="review-diff-list" style=${{ padding: "0 12px" }} key=${diffStyle} dangerouslySetInnerHTML=${{ __html: reviewHtml }}></div>
              </div>
            `
            : html`
              <${TabBar}
                reviewTab=${html`<${ReviewTab} count=${modifiedCount()} active=${false} onClick=${toggleReviewMode} />`}
                fileList=${diffs.map((d) => d.file)}
                onOpenFile=${handleOpenFile}
                files=${openFiles}
                activePath=${activeFilePath}
                onSelect=${setActiveFilePath}
                onClose=${closeFile}
              />
              <${CodeViewer}
                key=${activeFile?.path ?? "empty"}
                file=${activeFile}
                comments=${activeFileComments}
                draft=${draft && draft.file === activeFilePath ? draft : null}
                onStartComment=${startDraft}
                onEditComment=${editComment}
                onCancelComment=${cancelDraft}
                onCommentChange=${setDraftContent}
                onSubmitComment=${submitDraft}
                onDeleteComment=${deleteComment}
              />
            `
        }
      </div>

      <${ResizeHandle} onResize=${handleRightResize} direction="horizontal" />

      <div class="changes-panel changes-panel-right" style=${{ width: `${rightPct}%` }}>
        <div class="changes-panel-header">
          <span class="glyph">▼</span>
          <span>${t("changes.fileTreeTitle")}</span>
        </div>
        <${FileTreeToggle}
          showOnlyModified=${showOnlyModified}
          modifiedCount=${modifiedCount()}
          onToggle=${toggleModifiedFilter}
        />
        <div class="changes-panel-body">
          ${
            loading
              ? html`<div class="empty" style=${{ margin: "12px", textAlign: "center" }}>${t("changes.loadingFiles")}</div>`
              : html`<${FileTree}
                nodes=${tree}
                expanded=${expanded}
                onToggle=${toggleExpand}
                onSelect=${(node: any) => {
                  setReviewMode(false);
                  openFile(node);
                }}
                modifiedFiles=${modifiedFiles()}
                showOnlyModified=${showOnlyModified}
              />`
          }
        </div>
      </div>
    </div>
  `;
}

// ── ChatStatusBar — inlined (mirrors chat.ts pattern) ──────────

interface ChatStats {
  lastPromptTokens: number;
  contextCapTokens: number;
  cacheHitRatio: number;
  lastTurnCostUsd: number;
  totalCostUsd: number;
  turns: number;
  balance?: Array<{ total_balance: string; currency: string }>;
}

interface ChatStatusBarProps {
  stats: ChatStats | null;
  model: string | null;
}

function fmtCost(usd: number, currency?: string): string {
  if (currency === "CNY" || currency === "¥") {
    return `¥${(usd * 7.2).toFixed(4)}`;
  }
  return `$${usd.toFixed(4)}`;
}

function ChatStatusBar({ stats, model }: ChatStatusBarProps) {
  useLang();
  if (!stats) {
    return html`
      <div class="chat-statusbar">
        <span class="muted">—</span>
      </div>
    `;
  }
  const ctxPct =
    stats.contextCapTokens > 0 ? (stats.lastPromptTokens / stats.contextCapTokens) * 100 : 0;
  const balance = stats.balance && stats.balance.length > 0 ? stats.balance[0] : null;
  return html`
    <div class="chat-statusbar">
      <span class="status-item">
        <span class="status-label">${t("chat.statusModel")}</span>
        <code>${model ?? "—"}</code>
      </span>
      <span class="status-item">
        <span class="status-label">${t("chat.statusCtx")}</span>
        <span class="status-bar-mini">
          <span class="status-bar-mini-fill" style=${`width: ${Math.min(100, ctxPct).toFixed(1)}%;`}></span>
        </span>
        <span class="muted">${stats.lastPromptTokens.toLocaleString()} / ${(stats.contextCapTokens / 1000).toFixed(0)}K</span>
      </span>
      <span class="status-item">
        <span class="status-label">${t("chat.statusCache")}</span>
        <span class=${stats.cacheHitRatio >= 0.9 ? "status-ok" : stats.cacheHitRatio >= 0.6 ? "status-warn" : "status-err"}>
          ${(stats.cacheHitRatio * 100).toFixed(1)}%
        </span>
      </span>
      <span class="status-item">
        <span class="status-label">${t("chat.statusTurn")}</span>
        <code>${fmtCost(stats.lastTurnCostUsd, balance?.currency)}</code>
      </span>
      <span class="status-item">
        <span class="status-label">${t("chat.statusSession")}</span>
        <code>${fmtCost(stats.totalCostUsd, balance?.currency)}</code>
        <span class="muted" style="font-size: 10px;">
          ${t("chat.statusTurns", { count: stats.turns, s: stats.turns === 1 ? "" : "s" })}
        </span>
      </span>
      ${
        balance
          ? html`
          <span class="status-item">
            <span class="status-label">${t("chat.statusBalance")}</span>
            <code>${balance.total_balance} ${balance.currency}</code>
          </span>
        `
          : null
      }
    </div>
  `;
}

// ── CommentCard ────────────────────────────────────────────────

interface CommentCardProps {
  fileName: string;
  lineNumber: number;
  content: string;
  onRemove: () => void;
}

function CommentCard(props: CommentCardProps) {
  return html`
    <div class="comment-card">
      <span class="comment-card-icon">⬥</span>
      <span class="comment-card-file">${props.fileName}:${props.lineNumber}</span>
      <span class="comment-card-content">${props.content}</span>
      <span class="comment-card-remove" onClick=${props.onRemove}>×</span>
    </div>
  `;
}

// ── LineCommentAnchor ──────────────────────────────────────────

interface LineCommentAnchorProps {
  visible: boolean;
  onClick: () => void;
  hasComments: boolean;
  commentCount: number;
}

function LineCommentAnchor(props: LineCommentAnchorProps) {
  return html`
    <div
      class="line-comment-anchor ${props.visible ? "visible" : ""}"
      onClick=${(e: Event) => {
        e.stopPropagation();
        props.onClick();
      }}
    >
      ${
        props.hasComments
          ? html`<span class="comment-count">${props.commentCount}</span>`
          : html`<span class="plus-icon">+</span>`
      }
    </div>
  `;
}

// ── LineCommentBubble ──────────────────────────────────────────

interface LineCommentBubbleProps {
  content: string;
  lineNumber: number;
  onEdit: () => void;
  onDelete: () => void;
}

function LineCommentBubble(props: LineCommentBubbleProps) {
  return html`
    <div class="line-comment-bubble">
      <div class="bubble-content">${props.content}</div>
      <div class="bubble-footer">
        <span class="bubble-line">${t("changes.commentLine")} ${props.lineNumber}</span>
        <div class="bubble-actions">
          <button class="bubble-btn" onClick=${props.onEdit}>${t("changes.commentEdit")}</button>
          <button class="bubble-btn danger" onClick=${props.onDelete}>${t("changes.commentDelete")}</button>
        </div>
      </div>
    </div>
  `;
}

// ── LineCommentEditor ──────────────────────────────────────────

interface LineCommentEditorProps {
  lineNumber: number;
  value: string;
  onInput: (value: string) => void;
  onCancel: () => void;
  onSubmit: () => void;
}

function LineCommentEditor(props: LineCommentEditorProps) {
  const isComposingRef = useRef(false);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        props.onCancel();
      } else if (e.key === "Enter" && e.ctrlKey) {
        e.preventDefault();
        props.onSubmit();
      }
    },
    [props.onCancel, props.onSubmit],
  );

  return html`
    <div class="line-comment-editor">
      <div class="line-comment-label">${t("changes.commentLabel")} ${props.lineNumber}</div>
      <textarea
        class="line-comment-textarea"
        value=${props.value}
        onCompositionStart=${() => {
          isComposingRef.current = true;
        }}
        onCompositionEnd=${(e: CompositionEvent) => {
          isComposingRef.current = false;
          props.onInput((e.target as HTMLTextAreaElement).value);
        }}
        onInput=${(e: Event) => {
          if (!isComposingRef.current) {
            props.onInput((e.target as HTMLTextAreaElement).value);
          }
        }}
        onKeyDown=${handleKeyDown}
        placeholder=${t("changes.commentPlaceholder")}
        rows=${3}
        autofocus=${true}
      />
      <div class="line-comment-actions">
        <button class="btn ghost" onClick=${props.onCancel}>${t("changes.commentCancel")}</button>
        <button class="btn primary" onClick=${props.onSubmit} disabled=${!props.value.trim()}>${t("changes.commentSubmit")}</button>
      </div>
    </div>
  `;
}

// ── FileTree ───────────────────────────────────────────────────

interface FileTreeProps {
  nodes: TreeNode[];
  expanded: Set<string>;
  onToggle: (path: string) => void;
  onSelect: (node: TreeNode) => void;
  indent?: number;
  modifiedFiles?: Set<string>;
  showOnlyModified?: boolean;
}

function filterModifiedNodes(nodes: TreeNode[], modifiedFiles: Set<string>): TreeNode[] {
  return nodes
    .map((node) => {
      if (node.isDir && node.children) {
        const filteredChildren = filterModifiedNodes(node.children, modifiedFiles);
        if (filteredChildren.length === 0) return null;
        return { ...node, children: filteredChildren };
      }
      if (modifiedFiles.has(node.path)) return node;
      return null;
    })
    .filter((n) => n !== null) as TreeNode[];
}

function renderTree(props: FileTreeProps): any[] {
  const {
    nodes,
    expanded,
    onToggle,
    onSelect,
    indent = 0,
    modifiedFiles = new Set(),
    showOnlyModified = false,
  } = props;
  const displayNodes = showOnlyModified ? filterModifiedNodes(nodes, modifiedFiles) : nodes;
  return displayNodes.map((node) => {
    const isExpanded = expanded.has(node.path);
    const indentEls: any[] = [];
    for (let i = 0; i < indent; i++) {
      indentEls.push(html`<span class="indent" key=${`indent-${i}`} />`);
    }
    if (node.isDir) {
      const cls = isExpanded ? "tree-node open" : "tree-node";
      return html`
        <div key=${node.path}>
          <div class=${cls} onClick=${() => onToggle(node.path)}>
            ${indentEls}
            <span class="arrow">${isExpanded ? "▾" : "▸"}</span>
            <span class="icon dir">▼</span>
            <span class="name">${node.name}</span>
          </div>
          ${
            isExpanded && node.children && node.children.length > 0
              ? renderTree({
                  nodes: node.children,
                  expanded,
                  onToggle,
                  onSelect,
                  indent: indent + 1,
                  modifiedFiles,
                  showOnlyModified,
                })
              : null
          }
          ${
            isExpanded && (!node.children || node.children.length === 0)
              ? html`<div class="tree-node" style=${{ paddingLeft: `${(indent + 1) * 14 + 8}px` }}>
                <span class="name muted">${t("changes.treeEmpty")}</span>
              </div>`
              : null
          }
        </div>
      `;
    }
    const { icon, cls } = getFileIcon(node.name);
    const isModified = modifiedFiles.has(node.path);
    return html`
      <div
        key=${node.path}
        class="tree-node"
        onClick=${() => onSelect(node)}
        style=${{ paddingLeft: `${indent * 14 + 8}px` }}
      >
        <span class=${`icon ${cls}`}>${icon}</span>
        <span class="name">${node.name}</span>
        ${isModified ? html`<span class="mod-indicator" />` : null}
      </div>
    `;
  });
}

function FileTree(props: FileTreeProps) {
  return html`
    <div class="tree">
      ${renderTree(props)}
    </div>
  `;
}

// ── FileTreeToggle ─────────────────────────────────────────────

interface FileTreeToggleProps {
  showOnlyModified: boolean;
  modifiedCount: number;
  onToggle: () => void;
}

function FileTreeToggle(props: FileTreeToggleProps) {
  return html`
    <div class="file-tree-toggle">
      <button
        class=${`toggle-btn ${props.showOnlyModified ? "active" : ""}`}
        onClick=${props.onToggle}
      >
        ${props.modifiedCount} ${t("changes.changes")}
      </button>
      <button
        class=${`toggle-btn ${!props.showOnlyModified ? "active" : ""}`}
        onClick=${props.onToggle}
      >
        ${t("changes.allFiles")}
      </button>
    </div>
  `;
}

// ── ReviewTab ──────────────────────────────────────────────────

interface ReviewTabProps {
  count: number;
  active?: boolean;
  onClick?: () => void;
}

function ReviewTab(props: ReviewTabProps) {
  return html`
    <div
      class=${`editor-tab review-tab${props.active ? " active" : ""}`}
      onClick=${props.onClick}
      style=${{ display: "flex", alignItems: "center", gap: "3px", padding: "4px 6px", cursor: props.onClick ? "pointer" : "default" }}
    >
      <span class="review-icon">◑</span>
      <span>${t("changes.review")}</span>
      <span style=${{ color: "var(--fg-3)", fontSize: "10.5px" }}>${props.count}</span>
    </div>
  `;
}

// ── ResizeHandle ───────────────────────────────────────────────

interface ResizeHandleProps {
  onResize: (delta: number) => void;
  direction: "horizontal" | "vertical";
}

function ResizeHandle(props: ResizeHandleProps) {
  const { onResize, direction } = props;
  const dragging = useRef(false);
  const startX = useRef(0);

  const onMouseDown = useCallback(
    (e: MouseEvent) => {
      e.preventDefault();
      dragging.current = true;
      startX.current = direction === "horizontal" ? e.clientX : e.clientY;
      document.body.style.cursor = direction === "horizontal" ? "col-resize" : "row-resize";
      document.body.style.userSelect = "none";
    },
    [direction],
  );

  useEffect(() => {
    const onMouseMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      const current = direction === "horizontal" ? e.clientX : e.clientY;
      const delta = current - startX.current;
      startX.current = current;
      onResize(delta);
    };
    const onMouseUp = () => {
      if (!dragging.current) return;
      dragging.current = false;
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    document.addEventListener("mousemove", onMouseMove);
    document.addEventListener("mouseup", onMouseUp);
    return () => {
      document.removeEventListener("mousemove", onMouseMove);
      document.removeEventListener("mouseup", onMouseUp);
    };
  }, [onResize, direction]);

  const isH = direction === "horizontal";
  return html`
    <div
      onMouseDown=${onMouseDown}
      style=${{
        width: isH ? "4px" : "100%",
        height: isH ? "100%" : "4px",
        cursor: isH ? "col-resize" : "row-resize",
        background: "var(--bd)",
        flexShrink: 0,
        transition: "background 0.15s",
      }}
      onMouseEnter=${(e: Event) => {
        (e.target as HTMLElement).style.background = "var(--c-brand)";
      }}
      onMouseLeave=${(e: Event) => {
        (e.target as HTMLElement).style.background = "var(--bd)";
      }}
    />
  `;
}

// ── TabBar ─────────────────────────────────────────────────────

interface TabBarProps {
  files: OpenFile[];
  activePath: string | null;
  onSelect: (path: string) => void;
  onClose: (path: string) => void;
  reviewTab?: any;
  fileList?: string[];
  onOpenFile?: (file: string) => void;
}

function TabBar(props: TabBarProps) {
  const { files, activePath, onSelect, onClose, reviewTab, fileList, onOpenFile } = props;
  const popupRef = useRef<HTMLDivElement | null>(null);
  const btnRef = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    const btn = btnRef.current;
    if (!btn || !fileList || fileList.length === 0) return;

    const toggle = (e: MouseEvent) => {
      e.stopPropagation();
      if (popupRef.current) {
        popupRef.current.remove();
        popupRef.current = null;
        return;
      }
      const allFiles = fileList;
      const popup = document.createElement("div");
      popupRef.current = popup;
      popup.style.cssText =
        "position:fixed;top:20%;left:50%;transform:translateX(-50%);background:var(--bg-elev-2);border:1px solid var(--bd);border-radius:6px;max-height:400px;display:flex;flex-direction:column;z-index:1000;min-width:380px;max-width:600px;box-shadow:0 8px 24px rgba(0,0,0,.4)";

      const input = document.createElement("input");
      input.placeholder = "搜索文件...";
      input.style.cssText =
        "margin:6px 8px;padding:5px 8px;font-size:12px;background:var(--bg);color:var(--fg-0);border:1px solid var(--bd);border-radius:4px;outline:none;flex-shrink:0";
      input.onclick = (ev) => ev.stopPropagation();
      popup.appendChild(input);

      const listWrap = document.createElement("div");
      listWrap.style.cssText = "overflow-y:auto;flex:1;padding:0 4px 4px";
      popup.appendChild(listWrap);

      function renderList(filter: string) {
        listWrap.innerHTML = "";
        const q = filter.toLowerCase();
        for (const f of allFiles) {
          if (q && !f.toLowerCase().includes(q)) continue;
          const row = document.createElement("div");
          row.textContent = f;
          row.style.cssText =
            "padding:3px 8px;font-size:11px;cursor:pointer;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;font-family:var(--font-mono);border-radius:3px";
          row.onmouseenter = () => (row.style.background = "var(--bg-hover)");
          row.onmouseleave = () => (row.style.background = "transparent");
          row.onclick = (ev) => {
            ev.stopPropagation();
            onOpenFile?.(f);
            popup.remove();
            popupRef.current = null;
          };
          listWrap.appendChild(row);
        }
      }
      renderList("");

      input.oninput = () => renderList(input.value);

      setTimeout(() => input.focus(), 50);

      document.body.appendChild(popup);

      const close = (ev: MouseEvent) => {
        if (popupRef.current && !popup.contains(ev.target as Node) && ev.target !== btn) {
          popup.remove();
          popupRef.current = null;
          document.removeEventListener("mousedown", close, true);
        }
      };
      document.addEventListener("mousedown", close, true);
    };

    btn.addEventListener("click", toggle);
    return () => {
      btn.removeEventListener("click", toggle);
      if (popupRef.current) {
        popupRef.current.remove();
        popupRef.current = null;
      }
    };
  }, [fileList, onOpenFile]);

  return html`
    <div class="editor-tabs">
      ${reviewTab || null}
      ${
        fileList
          ? html`
        <span
          ref=${btnRef}
          style=${{
            fontSize: "14px",
            padding: "4px 3px",
            cursor: "pointer",
            color: "var(--fg-3)",
            userSelect: "none",
            lineHeight: "1",
            fontFamily: "var(--font-mono)",
          }}
          title="Open file"
        >+</span>
      `
          : null
      }
      ${files.map(
        (f) => html`
        <div
          key=${f.path}
          class=${`editor-tab ${f.path === activePath ? "active" : ""}`}
          onClick=${() => onSelect(f.path)}
          title=${f.path}
        >
          <span>${f.name}</span>
          <span
            class="x"
            onClick=${(e: Event) => {
              e.stopPropagation();
              onClose(f.path);
            }}
            title=${t("changes.tabClose")}
          >×</span>
        </div>
      `,
      )}
    </div>
  `;
}

// ── CodeViewer ─────────────────────────────────────────────────

interface CodeViewerProps {
  file: OpenFile | null;
  comments?: LineComment[];
  draft?: LineCommentDraft | null;
  onStartComment?: (file: string, lineNumber: number) => void;
  onCancelComment?: () => void;
  onCommentChange?: (content: string) => void;
  onSubmitComment?: () => void;
  onDeleteComment?: (id: string) => void;
  onEditComment?: (id: string, content: string) => void;
}

function CodeViewer(props: CodeViewerProps) {
  const {
    file,
    comments = [],
    draft,
    onStartComment,
    onEditComment,
    onCancelComment,
    onCommentChange,
    onSubmitComment,
    onDeleteComment,
  } = props;
  const codeRef = useRef<HTMLDivElement>(null);
  const [hoveredLine, setHoveredLine] = useState<number | null>(null);

  useEffect(() => {
    if (!file) return;
    const el = codeRef.current;
    if (!el) return;
    el.innerHTML = "";
    const lines = file.content.split("\n");
    const commentsByLine = new Map<number, LineComment[]>();
    comments.forEach((c) => {
      const existing = commentsByLine.get(c.lineNumber) || [];
      existing.push(c);
      commentsByLine.set(c.lineNumber, existing);
    });

    lines.forEach((line, i) => {
      const lineNumber = i + 1;
      const lineComments = commentsByLine.get(lineNumber) || [];
      const hasComments = lineComments.length > 0;
      const lineDiv = document.createElement("div");
      lineDiv.className = "editor-line";
      lineDiv.dataset.lineNumber = String(lineNumber);
      lineDiv.style.position = "relative";
      lineDiv.addEventListener("mouseenter", () => setHoveredLine(lineNumber));
      lineDiv.addEventListener("mouseleave", () => setHoveredLine(null));

      const gutter = document.createElement("div");
      gutter.className = "lineno";
      gutter.textContent = String(lineNumber);
      gutter.style.position = "relative";
      gutter.style.display = "flex";
      gutter.style.alignItems = "center";
      gutter.style.justifyContent = "center";
      gutter.style.gap = "4px";

      if (onStartComment) {
        const isVisible =
          hoveredLine === lineNumber &&
          (!draft || draft.file !== file.path || draft.lineNumber !== lineNumber);
        const anchorBtn = document.createElement("span");
        anchorBtn.className = `line-comment-anchor ${isVisible ? "visible" : ""}`;
        anchorBtn.style.cssText =
          "width:16px;height:16px;display:flex;align-items:center;justify-content:center;opacity:0;pointer-events:none;cursor:pointer;transition:opacity 0.15s ease;flex-shrink:0;";
        if (isVisible) {
          anchorBtn.style.opacity = "1";
          anchorBtn.style.pointerEvents = "auto";
        }
        if (hasComments) {
          anchorBtn.innerHTML = `<span class="comment-count" style="background:rgba(121,192,255,0.12);border-radius:2px;padding:0 3px;font-size:10px;color:#79c0ff;font-family:monospace;">${lineComments.length}</span>`;
        } else {
          anchorBtn.innerHTML = `<span class="plus-icon" style="font-family:monospace;font-size:14px;color:#6e7681;line-height:1;">+</span>`;
        }
        anchorBtn.addEventListener("mouseenter", () => {
          anchorBtn.style.opacity = "1";
        });
        anchorBtn.addEventListener("click", (e) => {
          e.stopPropagation();
          onStartComment(file.path, lineNumber);
        });
        gutter.appendChild(anchorBtn);
      }

      const content = document.createElement("span");
      content.className = "ln-content";
      content.textContent = line || " ";

      lineDiv.appendChild(gutter);
      lineDiv.appendChild(content);
      el.appendChild(lineDiv);

      if (draft && draft.file === file.path && draft.lineNumber === lineNumber) {
        const editorContainer = document.createElement("div");
        editorContainer.className = "line-comment-editor";
        const labelDiv = document.createElement("div");
        labelDiv.className = "line-comment-label";
        labelDiv.textContent = `${t("changes.commentLabel")} ${lineNumber}`;
        const textarea = document.createElement("textarea");
        textarea.className = "line-comment-textarea";
        textarea.placeholder = t("changes.commentPlaceholder");
        textarea.rows = 2;
        textarea.value = draft.content;
        let isComposing = false;
        textarea.addEventListener("compositionstart", () => {
          isComposing = true;
        });
        textarea.addEventListener("compositionend", (e) => {
          isComposing = false;
          if (onCommentChange) onCommentChange((e.target as HTMLTextAreaElement).value);
        });
        textarea.addEventListener("input", (e) => {
          if (!isComposing && onCommentChange)
            onCommentChange((e.target as HTMLTextAreaElement).value);
        });
        textarea.addEventListener("keydown", (e) => {
          if (e.key === "Escape" && onCancelComment) {
            e.preventDefault();
            onCancelComment();
          } else if (e.key === "Enter" && e.ctrlKey && onSubmitComment) {
            e.preventDefault();
            onSubmitComment();
          }
        });
        const actionsDiv = document.createElement("div");
        actionsDiv.className = "line-comment-actions";
        actionsDiv.style.cssText = "display:flex;gap:4px;justify-content:flex-end;";
        const cancelBtn = document.createElement("button");
        cancelBtn.className = "btn ghost";
        cancelBtn.textContent = t("changes.commentCancel");
        cancelBtn.style.cssText =
          "background:transparent;border:none;color:#6e7681;padding:3px 8px;font-size:11px;cursor:pointer;";
        cancelBtn.addEventListener("click", () => {
          if (onCancelComment) onCancelComment();
        });
        const submitBtn = document.createElement("button");
        submitBtn.className = "btn primary";
        submitBtn.textContent = t("changes.commentSubmit");
        submitBtn.style.cssText =
          "background:#79c0ff;color:#0a0c10;border:none;padding:3px 8px;font-size:11px;cursor:pointer;border-radius:2px;font-weight:600;";
        submitBtn.disabled = !draft.content.trim();
        submitBtn.addEventListener("click", () => {
          if (onSubmitComment) onSubmitComment();
        });
        actionsDiv.appendChild(cancelBtn);
        actionsDiv.appendChild(submitBtn);
        editorContainer.appendChild(labelDiv);
        editorContainer.appendChild(textarea);
        editorContainer.appendChild(actionsDiv);
        el.appendChild(editorContainer);
        setTimeout(() => textarea.focus(), 0);
      }

      if (hasComments) {
        lineComments.forEach((comment) => {
          if (el.querySelector(`.line-comment-bubble[data-id="${comment.id}"]`)) return;
          const isEditing = draft && draft.editingId === comment.id;
          if (isEditing) return;
          const bubbleDiv = document.createElement("div");
          bubbleDiv.className = "line-comment-bubble";
          bubbleDiv.dataset.id = comment.id;
          const contentDiv = document.createElement("div");
          contentDiv.className = "bubble-content";
          contentDiv.textContent = comment.content;
          const footerDiv = document.createElement("div");
          footerDiv.className = "bubble-footer";
          const lineSpan = document.createElement("span");
          lineSpan.className = "bubble-line";
          lineSpan.textContent = `评论第 ${comment.lineNumber} 行`;
          const actionsDiv = document.createElement("div");
          actionsDiv.className = "bubble-actions";
          actionsDiv.style.display = "flex";
          actionsDiv.style.gap = "4px";
          if (onEditComment) {
            const editBtn = document.createElement("button");
            editBtn.className = "bubble-btn";
            editBtn.textContent = "编辑";
            editBtn.style.cssText =
              "background:transparent;border:none;color:#6e7681;padding:3px 8px;font-size:11px;cursor:pointer;border-radius:2px;";
            editBtn.addEventListener("click", (e) => {
              e.stopPropagation();
              onEditComment(comment.id, comment.content);
            });
            actionsDiv.appendChild(editBtn);
          }
          if (onDeleteComment) {
            const deleteBtn = document.createElement("button");
            deleteBtn.className = "bubble-btn danger";
            deleteBtn.textContent = "删除";
            deleteBtn.style.cssText =
              "background:transparent;border:none;color:#6e7681;padding:3px 8px;font-size:11px;cursor:pointer;border-radius:2px;";
            deleteBtn.addEventListener("click", (e) => {
              e.stopPropagation();
              onDeleteComment(comment.id);
            });
            actionsDiv.appendChild(deleteBtn);
          }
          footerDiv.appendChild(lineSpan);
          footerDiv.appendChild(actionsDiv);
          bubbleDiv.appendChild(contentDiv);
          bubbleDiv.appendChild(footerDiv);
          el.appendChild(bubbleDiv);
        });
      }
    });

    if (hljs) {
      const codeEl = codeRef.current;
      if (codeEl) {
        codeEl.querySelectorAll(".ln-content").forEach((span) => {
          const text = span.textContent ?? "";
          try {
            const result = hljs.highlight(text, { language: file.language, ignoreIllegals: true });
            span.innerHTML = result.value;
          } catch {
            span.textContent = text;
          }
        });
      }
    }
  }, [file, comments, draft]);

  useEffect(() => {
    if (!codeRef.current || !file) return;
    const anchors = codeRef.current.querySelectorAll<HTMLElement>(".line-comment-anchor");
    anchors.forEach((anchor) => {
      const lineDiv = anchor.closest(".editor-line") as HTMLElement;
      if (!lineDiv) return;
      const lineNumber = Number.parseInt(lineDiv.dataset.lineNumber || "0", 10);
      const isVisible =
        hoveredLine === lineNumber &&
        (!draft || draft.file !== file.path || draft.lineNumber !== lineNumber);
      anchor.style.opacity = isVisible ? "1" : "0";
      anchor.style.pointerEvents = isVisible ? "auto" : "none";
    });
  }, [hoveredLine, draft, file]);

  if (!file) {
    return html`
      <div class="editor-area" style=${{ display: "flex", alignItems: "center", justifyContent: "center" }}>
        <div class="empty">${t("changes.viewerPlaceholder")}</div>
      </div>
    `;
  }

  return html`
    <div class="editor-area" ref=${codeRef} />
    <div class="editor-status">
      <span class="glyph">◆</span>
      <span class="v">${file.name}</span>
      <span class="grow"></span>
      <span>${file.language}</span>
      <span class="v">${String(file.content.split("\n").length)} lines</span>
    </div>
  `;
}

// ── ChatPane ───────────────────────────────────────────────────

interface StreamingState {
  id: string;
  text: string;
  reasoning: string;
}

interface ActiveToolState {
  id: string;
  toolName?: string;
  args?: string;
}

interface MessagesResponse {
  messages?: ChatMsg[];
  busy?: boolean;
}

interface ChatStats {
  lastPromptTokens: number;
  contextCapTokens: number;
  cacheHitRatio: number;
  lastTurnCostUsd: number;
  totalCostUsd: number;
  turns: number;
  balance?: Array<{ total_balance: string; currency: string }>;
}

interface OverviewResponse {
  model?: string;
  stats?: ChatStats;
}

interface SlashCommand {
  cmd: string;
  summary: string;
  argsHint?: string;
}

interface PopoverItem {
  label: string;
  meta?: string;
  insert: string;
}

type PopoverKind = "slash" | null;

interface ChatPaneProps {
  comments: LineComment[];
  deleteComment: (id: string) => void;
}

function ChatPane(props: ChatPaneProps) {
  useLang();
  const [messages, setMessages] = useState<ChatMsg[]>([]);
  const [streaming, setStreaming] = useState<StreamingState | null>(null);
  const [activeTool, setActiveTool] = useState<ActiveToolState | null>(null);
  const [busy, setBusy] = useState(false);
  const [input, setInput] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [statusLine, setStatusLine] = useState<string | null>(null);
  const [stats, setStats] = useState<ChatStats | null>(null);
  const [model, setModel] = useState<string | null>(null);
  const shouldAutoScroll = useRef(true);
  const feedRef = useRef<HTMLDivElement | null>(null);
  const streamBufRef = useRef<StreamingState | null>(null);
  const streamRafRef = useRef<number | null>(null);
  /** Suppresses scroll listener during programmatic auto-snap so it doesn't re-arm shouldAutoScroll. */
  const autoScrollInFlight = useRef(false);
  const [slashCommands, setSlashCommands] = useState<SlashCommand[]>([]);
  const [popoverKind, setPopoverKind] = useState<PopoverKind>(null);
  const [popoverItems, setPopoverItems] = useState<PopoverItem[]>([]);
  const [popoverSel, setPopoverSel] = useState(0);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const r = await api<{ commands: SlashCommand[] }>("/slash");
        if (!cancelled) setSlashCommands(r.commands);
      } catch {
        /* swallow */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await api<MessagesResponse>("/messages");
        if (!cancelled) {
          setMessages(data.messages ?? []);
          setBusy(Boolean(data.busy));
        }
      } catch {
        if (!cancelled) setMessages([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    const tick = async () => {
      try {
        const data = await api<OverviewResponse>("/overview");
        if (cancelled) return;
        setStats(data.stats ?? null);
        setModel(data.model ?? null);
      } catch {
        /* swallow */
      }
    };
    tick();
    const t = setInterval(tick, 2500);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, []);

  // rAF-coalesce assistant_delta events — same pattern as ChatPanel.
  const flushStreaming = useCallback(() => {
    streamRafRef.current = null;
    if (streamBufRef.current) setStreaming(streamBufRef.current);
  }, []);
  const cancelStreamingRaf = useCallback(() => {
    if (streamRafRef.current !== null) {
      cancelAnimationFrame(streamRafRef.current);
      streamRafRef.current = null;
    }
    streamBufRef.current = null;
  }, []);

  // SSE reconnect drops missed deltas / finals — re-fetch canonical
  // state to avoid wedging the UI on stale messages.
  const refetchCanonicalState = useCallback(async () => {
    try {
      const data = await api<MessagesResponse>("/messages");
      setMessages(data.messages ?? []);
      setBusy(Boolean(data.busy));
      cancelStreamingRaf();
      setStreaming(null);
      setActiveTool(null);
    } catch {
      /* keep current state — next event or next reconnect will retry */
    }
  }, [cancelStreamingRaf]);

  useEffect(() => {
    const es = new EventSource(`/api/events?token=${TOKEN}`);
    let firstOpen = true;
    es.onopen = () => {
      if (firstOpen) {
        firstOpen = false;
        return;
      }
      void refetchCanonicalState();
    };
    es.onmessage = (ev) => {
      let dash: any;
      try {
        dash = JSON.parse(ev.data);
      } catch {
        return;
      }
      if (dash.kind === "ping") return;
      if (dash.kind === "busy-change") {
        setBusy(dash.busy);
        return;
      }
      if (dash.kind === "user") {
        setMessages((prev) => [...prev, { id: dash.id, role: "user" as const, text: dash.text }]);
        return;
      }
      if (dash.kind === "assistant_delta") {
        const cur = streamBufRef.current;
        const baseId = cur?.id === dash.id ? cur : null;
        streamBufRef.current = {
          id: dash.id,
          text: (baseId?.text ?? "") + (dash.contentDelta ?? ""),
          reasoning: (baseId?.reasoning ?? "") + (dash.reasoningDelta ?? ""),
        };
        if (streamRafRef.current === null) {
          streamRafRef.current = requestAnimationFrame(flushStreaming);
        }
        return;
      }
      if (dash.kind === "assistant_final") {
        cancelStreamingRaf();
        setStreaming(null);
        setMessages((prev) => [
          ...prev,
          { id: dash.id, role: "assistant", text: dash.text, reasoning: dash.reasoning },
        ]);
        return;
      }
      if (dash.kind === "tool_start") {
        setActiveTool({ id: dash.id, toolName: dash.toolName, args: dash.args });
        return;
      }
      if (dash.kind === "tool") {
        setActiveTool((cur) => (cur && cur.id === dash.id ? null : cur));
        setMessages((prev) => [
          ...prev,
          {
            id: dash.id,
            role: "tool",
            text: dash.content,
            toolName: dash.toolName,
            toolArgs: dash.args,
          },
        ]);
        return;
      }
      if (dash.kind === "warning" || dash.kind === "error" || dash.kind === "info") {
        if (dash.kind === "error") setActiveTool(null);
        setMessages((prev) => [...prev, { id: dash.id, role: dash.kind, text: dash.text }]);
        return;
      }
      if (dash.kind === "status") {
        setStatusLine(dash.text);
        setTimeout(() => setStatusLine((cur) => (cur === dash.text ? null : cur)), 5000);
        return;
      }
    };
    es.onerror = () => {
      setError(t("chat.eventStreamError"));
      setTimeout(() => setError(null), 3000);
    };
    return () => {
      es.close();
      cancelStreamingRaf();
    };
  }, [refetchCanonicalState, cancelStreamingRaf]);

  useEffect(() => {
    if (!shouldAutoScroll.current) return;
    const el = feedRef.current;
    if (!el) return;
    autoScrollInFlight.current = true;
    el.scrollTop = el.scrollHeight;
    setTimeout(() => {
      autoScrollInFlight.current = false;
    }, 0);
  }, [messages, streaming]);

  useEffect(() => {
    const el = feedRef.current;
    if (!el) return;
    const onScroll = () => {
      if (autoScrollInFlight.current) return;
      const distFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
      shouldAutoScroll.current = distFromBottom < 80;
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, []);

  const updatePopover = useCallback(
    async (text: string) => {
      const slashMatch = /^\/([A-Za-z0-9_-]*)$/.exec(text);
      if (slashMatch) {
        const prefix = slashMatch[1]!.toLowerCase();
        const items: PopoverItem[] = slashCommands
          .filter((c) => c.cmd.startsWith(prefix))
          .slice(0, 12)
          .map((c) => ({
            label: `/${c.cmd}`,
            meta: c.summary,
            insert: `/${c.cmd}${c.argsHint ? " " : ""}`,
          }));
        setPopoverKind("slash");
        setPopoverItems(items);
        setPopoverSel(0);
        return;
      }
      setPopoverKind(null);
    },
    [slashCommands],
  );

  const applyPopover = useCallback(() => {
    const item = popoverItems[popoverSel];
    if (!item) return false;
    setInput(item.insert);
    setPopoverKind(null);
    return true;
  }, [popoverItems, popoverSel, popoverKind, input]);

  const onInput = useCallback(
    (e: Event) => {
      const v = (e.target as HTMLTextAreaElement).value;
      setInput(v);
      updatePopover(v);
    },
    [updatePopover],
  );

  const send = useCallback(async () => {
    const text = input.trim();
    if (busy) return;
    if (!text && props.comments.length === 0) return;
    setError(null);

    let prompt = text;
    if (props.comments.length > 0) {
      const commentRefs = props.comments
        .map((c) => `📝 ${c.file}:${c.lineNumber} ${c.content}`)
        .join("\n");
      prompt = text ? `${text}\n\n${commentRefs}` : commentRefs;
    }

    try {
      const res = await api<{ accepted: boolean; reason?: string }>("/submit", {
        method: "POST",
        body: { prompt },
      });
      if (!res.accepted) {
        setError(res.reason ?? "rejected");
        return;
      }
      setInput("");
      props.comments.forEach((c) => props.deleteComment(c.id));
    } catch (err) {
      setError((err as Error).message);
    }
  }, [input, busy, props.comments]);

  const abort = useCallback(async () => {
    try {
      await api("/abort", { method: "POST" });
    } catch {
      /* swallow */
    }
  }, []);

  const newConversation = useCallback(async () => {
    if (busy) {
      if (!confirm(t("changes.newConfirmBusy"))) return;
    } else if (messages.length > 0 && !confirm(t("changes.newConfirm"))) {
      return;
    }
    try {
      await api("/submit", { method: "POST", body: { prompt: "/new" } });
      setMessages([]);
      setStreaming(null);
      setActiveTool(null);
      showToast(t("changes.newToast"), "info");
      setTimeout(async () => {
        try {
          const r = await api<MessagesResponse>("/messages");
          setMessages(r.messages ?? []);
        } catch {
          /* swallow */
        }
      }, 200);
    } catch (err) {
      setError(t("changes.newFailed", { error: (err as Error).message }));
    }
  }, [busy, messages.length]);

  const clearScrollback = useCallback(async () => {
    try {
      await api("/submit", { method: "POST", body: { prompt: "/clear" } });
      setMessages([]);
      setStreaming(null);
      setActiveTool(null);
      showToast(t("changes.clearToast"), "info");
      setTimeout(async () => {
        try {
          const r = await api<MessagesResponse>("/messages");
          setMessages(r.messages ?? []);
        } catch {
          /* swallow */
        }
      }, 200);
    } catch (err) {
      setError(t("changes.clearFailed", { error: (err as Error).message }));
    }
  }, []);

  const onKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (popoverKind && popoverItems.length > 0) {
        if (e.key === "ArrowDown") {
          e.preventDefault();
          setPopoverSel((i) => (i + 1) % popoverItems.length);
          return;
        }
        if (e.key === "ArrowUp") {
          e.preventDefault();
          setPopoverSel((i) => (i - 1 + popoverItems.length) % popoverItems.length);
          return;
        }
        if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey)) {
          e.preventDefault();
          if (applyPopover() && e.key === "Enter" && popoverKind === "slash") send();
          return;
        }
        if (e.key === "Escape") {
          e.preventDefault();
          setPopoverKind(null);
          return;
        }
      }
      if (e.key === "Escape" && busy) {
        e.preventDefault();
        abort();
        return;
      }
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        send();
      }
    },
    [send, abort, busy, popoverKind, popoverItems, applyPopover],
  );

  const allMessages = streaming
    ? [
        ...messages,
        {
          id: streaming.id,
          role: "assistant" as const,
          text: streaming.text,
          reasoning: streaming.reasoning,
        },
      ]
    : messages;

  return html`
    <div style=${{ display: "flex", flexDirection: "column", height: "100%" }}>
      ${statusLine ? html`<div class="changes-panel-header"><span>${statusLine}</span></div>` : null}
      <div class="chat-feed" style=${{ flex: 1, overflowY: "auto", padding: "8px" }} ref=${feedRef}>
        ${
          allMessages.length === 0 && !streaming
            ? html`<div class="empty" style=${{ margin: "12px", textAlign: "center" }}>${t("changes.chatWelcome")}</div>`
            : null
        }
        ${allMessages.map((msg) => {
          const isStreaming = streaming && msg.id === streaming.id;
          if (msg.role === "tool") {
            return html`
              <div class="chat-msg tool" key=${msg.id}>
                <div class="glyph">▣</div>
                <${ToolCard} msg=${msg} />
              </div>
            `;
          }
          return html`
            <${ChatMessage}
              key=${msg.id}
              msg=${{ id: msg.id, role: msg.role, text: msg.text, reasoning: msg.reasoning, toolName: msg.toolName, toolArgs: msg.toolArgs }}
              streaming=${Boolean(isStreaming)}
            />
          `;
        })}
      </div>
      ${error ? html`<div class="notice err" style=${{ margin: "0 8px 4px" }}>${error}</div>` : null}
      <div style=${{ padding: "8px", borderTop: "1px solid var(--bd)", flexShrink: 0 }}>
        ${
          props.comments.length > 0
            ? html`
          <div class="comment-cards-container" style=${{ display: "flex", flexWrap: "wrap", gap: "6px", marginBottom: "8px" }}>
            ${props.comments.map(
              (comment) => html`
              <${CommentCard}
                key=${comment.id}
                fileName=${comment.file}
                lineNumber=${comment.lineNumber}
                content=${comment.content}
                onRemove=${() => props.deleteComment(comment.id)}
              />
            `,
            )}
          </div>
        `
            : null
        }
        <div style=${{ display: "flex", gap: "8px", alignItems: "flex-end", position: "relative" }}>
          <div style=${{ flex: 1, position: "relative" }}>
            ${
              popoverKind && popoverItems.length > 0
                ? html`
                  <div class="popover" style="position:absolute;bottom:calc(100% + 6px);left:0;width:380px;max-height:280px;overflow-y:auto;z-index:10">
                    <div class="popover-h">${t("chat.slashCommands")}</div>
                    ${popoverItems.map(
                      (it, i) => html`
                        <div
                          class=${`popover-row ${i === popoverSel ? "sel" : ""}`}
                          onMouseDown=${(e: Event) => {
                            e.preventDefault();
                            setPopoverSel(i);
                            applyPopover();
                          }}
                        >
                          <span class="g">/</span>
                          <span class="name">${it.label}</span>
                          ${it.meta ? html`<span class="meta">${it.meta}</span>` : null}
                        </div>
                      `,
                    )}
                  </div>
                `
                : null
            }
            <textarea
              class="input"
              style=${{ width: "100%", resize: "none", minHeight: "36px", fontFamily: "inherit", fontSize: "13px", padding: "8px 10px", lineHeight: "1.4", background: "var(--bg-input)", border: "1px solid var(--bd)", borderRadius: "4px", color: "var(--fg-0)" }}
              placeholder=${props.comments.length > 0 ? "总结评论..." : t("changes.chatPlaceholder")}
              value=${input}
              onInput=${onInput}
              onKeyDown=${onKeyDown}
              onBlur=${() => setTimeout(() => setPopoverKind(null), 150)}
              rows="2"
            />
          </div>
          <div style=${{ display: "flex", flexDirection: "column", gap: "6px", flexShrink: 0 }}>
            <button class="primary" onClick=${send} disabled=${busy || (!input.trim() && props.comments.length === 0)} style=${{ padding: "8px 12px", borderRadius: "4px" }}>${t("changes.chatSend")}</button>
            <div style=${{ display: "flex", gap: "6px" }}>
              <button onClick=${newConversation} title=${t("changes.newTitle")}>${t("changes.newConversation")}</button>
              <button onClick=${clearScrollback} title=${t("changes.clearTitle")}>${t("changes.clearConversation")}</button>
            </div>
          </div>
        </div>
      </div>
      <${ChatStatusBar} stats=${stats} model=${model} />
    </div>
  `;
}
