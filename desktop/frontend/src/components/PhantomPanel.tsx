import { useCallback, useEffect, useState } from "react";
import { ChevronRight, Circle, CircleDot, AlertCircle, Clock, Archive } from "lucide-react";
import { app, onPhantomUpdate } from "../lib/bridge";
import { useT } from "../lib/i18n";

// ── A2 设计修订五：虚空 UI（Phantom Panel） ──
// 嵌入主窗口的 Session 状态投影面板。
// 虚线边框风格，按名称排序，点击跳转到对应标签页。
// 所有更新通过 Wails 事件流推送，零 token。

interface PhantomConclusion {
  summary: string;
  status: string;
  confidence: number;
  timestamp: string;
  sourceTurn: number;
}

interface CommBadge {
  pendingCount: number;
  totalCount: number;
  sentCount: number;
  recvCount: number;
  lastCommType: string;
  lastCommTime: string;
  unread: boolean;
}

interface JumpTarget {
  tabId: string;
  topicId: string;
  scrollPos: number;
}

interface PhantomEntry {
  sessionId: string;
  name: string;
  workspaceRoot: string;
  status: string; // active|idle|failed|waiting|archived
  conclusion: PhantomConclusion | null;
  commBadge: CommBadge;
  isolationLevel: string; // sandbox|zoned|observed|merged
  lastUpdate: string;
  jumpTarget: JumpTarget;
  turnCount: number;
}

interface PhantomPanelView {
  entries: PhantomEntry[];
  activeCount: number;
  totalCount: number;
}

interface PhantomUpdate {
  sessionId: string;
  type: string; // status|conclusion|comm|isolation|added|removed
  entry?: PhantomEntry;
}

// 状态图标和颜色
function StatusIcon({ status }: { status: string }) {
  switch (status) {
    case "active":
      return <CircleDot className="phantom-status-icon phantom-status-active" size={12} />;
    case "idle":
      return <Circle className="phantom-status-icon phantom-status-idle" size={12} />;
    case "failed":
      return <AlertCircle className="phantom-status-icon phantom-status-failed" size={12} />;
    case "waiting":
      return <Clock className="phantom-status-icon phantom-status-waiting" size={12} />;
    case "archived":
      return <Archive className="phantom-status-icon phantom-status-archived" size={12} />;
    default:
      return <Circle className="phantom-status-icon phantom-status-unknown" size={12} />;
  }
}

// 隔离级别颜色
function isolationColor(level: string): string {
  switch (level) {
    case "sandbox":
      return "var(--phantom-isolation-sandbox, #ef4444)";
    case "zoned":
      return "var(--phantom-isolation-zoned, #f97316)";
    case "observed":
      return "var(--phantom-isolation-observed, #eab308)";
    case "merged":
      return "var(--phantom-isolation-merged, #22c55e)";
    default:
      return "var(--phantom-isolation-default, #6b7280)";
  }
}

// 结论状态文本
function conclusionStatusText(status: string): string {
  switch (status) {
    case "已就绪":
      return "已就绪";
    case "等待中":
      return "等待中";
    case "编译错误":
      return "编译错误";
    case "执行失败":
      return "执行失败";
    case "纯文本回复":
      return "纯文本回复";
    default:
      return status;
  }
}

// 单个虚空 UI 条目
function PhantomEntryRow({
  entry,
  onJump,
}: {
  entry: PhantomEntry;
  onJump: (sessionId: string) => void;
}) {
  const handleClick = useCallback(() => {
    onJump(entry.sessionId);
  }, [entry.sessionId, onJump]);

  return (
    <div
      className="phantom-entry"
      onClick={handleClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          handleClick();
        }
      }}
    >
      {/* 左侧：隔离级别颜色条 */}
      <div
        className="phantom-isolation-bar"
        style={{ backgroundColor: isolationColor(entry.isolationLevel) }}
      />

      {/* 状态图标 */}
      <StatusIcon status={entry.status} />

      {/* 名称和结论 */}
      <div className="phantom-entry-content">
        <div className="phantom-entry-header">
          <span className="phantom-entry-name">{entry.name}</span>
          {entry.commBadge.totalCount > 0 && (
            <span
              className={`phantom-comm-badge ${entry.commBadge.unread ? "phantom-comm-unread" : ""}`}
              title={`交流: ${entry.commBadge.totalCount} (发出${entry.commBadge.sentCount}, 收到${entry.commBadge.recvCount})`}
            >
              交流: {entry.commBadge.totalCount}
              {entry.commBadge.unread && <span className="phantom-comm-arrow"> ↗</span>}
            </span>
          )}
        </div>
        {entry.conclusion && (
          <div className="phantom-entry-conclusion">
            <span className="phantom-conclusion-status">
              {conclusionStatusText(entry.conclusion.status)}
            </span>
            {entry.conclusion.summary && (
              <span className="phantom-conclusion-summary"> — {entry.conclusion.summary}</span>
            )}
          </div>
        )}
      </div>

      {/* 跳转箭头 */}
      <ChevronRight className="phantom-jump-arrow" size={14} />
    </div>
  );
}

export function PhantomPanel() {
  const t = useT();
  const [view, setView] = useState<PhantomPanelView>({ entries: [], activeCount: 0, totalCount: 0 });
  const [collapsed, setCollapsed] = useState(false);

  // 初始加载
  useEffect(() => {
    const load = async () => {
      try {
        const result = await app().GetPhantomEntries();
        if (result) {
          setView(result as PhantomPanelView);
        }
      } catch {
        // 忽略错误（可能在 mock 模式下不可用）
      }
    };
    load();
  }, []);

  // 订阅实时更新
  useEffect(() => {
    const unsub = onPhantomUpdate((update: PhantomUpdate) => {
      setView((prev) => {
        const entries = [...prev.entries];
        const idx = entries.findIndex((e) => e.sessionId === update.sessionId);

        switch (update.type) {
          case "added":
            if (update.entry && idx === -1) {
              entries.push(update.entry);
            }
            break;
          case "removed":
            if (idx !== -1) {
              entries.splice(idx, 1);
            }
            break;
          case "status":
          case "conclusion":
          case "comm":
          case "isolation":
            if (update.entry && idx !== -1) {
              entries[idx] = update.entry;
            } else if (update.entry && idx === -1) {
              entries.push(update.entry);
            }
            break;
        }

        // 按 name 排序
        entries.sort((a, b) => a.name.localeCompare(b.name));

        const activeCount = entries.filter((e) => e.status === "active").length;
        return { entries, activeCount, totalCount: entries.length };
      });
    });
    return unsub;
  }, []);

  // 跳转到指定标签页
  const handleJump = useCallback(async (sessionId: string) => {
    try {
      await app().JumpToPhantomEntry(sessionId);
    } catch {
      // 忽略错误
    }
  }, []);

  if (view.totalCount === 0) {
    return null; // 没有条目时不显示面板
  }

  return (
    <div className={`phantom-panel ${collapsed ? "phantom-panel-collapsed" : ""}`}>
      {/* 标题栏 */}
      <div
        className="phantom-panel-header"
        onClick={() => setCollapsed(!collapsed)}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            setCollapsed(!collapsed);
          }
        }}
      >
        <span className="phantom-panel-title">
          {t("phantom.title", "虚空 UI")}
        </span>
        <span className="phantom-panel-count">
          {view.activeCount > 0 && (
            <span className="phantom-active-count">{view.activeCount} 活跃</span>
          )}
          <span className="phantom-total-count">{view.totalCount}</span>
        </span>
      </div>

      {/* 条目列表 */}
      {!collapsed && (
        <div className="phantom-entries">
          {view.entries.map((entry) => (
            <PhantomEntryRow
              key={entry.sessionId}
              entry={entry}
              onJump={handleJump}
            />
          ))}
        </div>
      )}
    </div>
  );
}
