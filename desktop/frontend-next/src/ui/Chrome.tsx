import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { host as hostBridge } from "../port/host";
import type { HubPort } from "../port/hub";
import type { AccountState, AgentPort, SessionStatus, WorkspaceInfo } from "../port/port";
import { DeviceBar } from "./DeviceBar";
import { useDismiss } from "./dismiss";
import { useFolded } from "./usefold";
import { ariaChord, chord } from "./keys";
import { PhonePop } from "./PhonePop";
import { ViewSwitch } from "./ViewSwitch";
import { TooltipButton } from "./TooltipButton";
import { WindowControls, zoomOnTitleBar } from "./WindowControls";
import type { PaneView } from "../state/paneview";

const base = (p: string) => p.replace(/[/\\]+$/, "").split(/[/\\]/).pop() || p;
// The filename is a timestamp and a model ref — true, and useless to read. The
// title from /sessions is always present once the turn is on disk: a generated
// one when it is ready, the first message truncated until then.
const sessionName = (title?: string, p?: string) =>
  title?.trim() || (p ? base(p).replace(/\.jsonl$/, "") : t("新会话"));

interface Props {
  // Set when the focused pane is driven from another machine, naming it.
  host?: string;
  // Null while the window has no pane: the chrome still draws, and the few
  // controls that need a session simply do nothing until one is focused.
  port: AgentPort | null;
  status: SessionStatus | null;
  title?: string;
  steer: number;
  account: AccountState | null;
  theme: string;
  onTheme: () => void;
  onFind: () => void;
  // The focused pane's view, and what the switch needs to draw itself.
  view: PaneView;
  rows: number;
  surfaces: number;
  onView: (to: PaneView) => void;
  // The window's port, for what belongs to the window rather than a pane.
  hub?: HubPort;
  onError?: (e: unknown) => void;
  // Hands out the room after the session name where the focused pane draws its header views.
  onSlot?: (el: HTMLElement | null) => void;
}

export function Chrome({ port, status, title, steer, account, host, theme, onTheme, onFind, view, rows, surfaces, onView, hub, onError, onSlot }: Props) {
  const root = status?.workspaceRoot || status?.cwd || "";
  const project = root ? base(root) : "—";
  // The folder mark opens the project itself. Only a shell that can enter a
  // folder offers it, and only once a pane names where that folder is; a
  // pane driven from another machine names a path this shell cannot enter,
  // so the mark stays disabled there. The kernel is still the one that
  // answers where the folder lives.
  const canOpenProject = !!hub && root !== "" && !host && hostBridge().opensFolders();
  const openProject = () => {
    if (hub && root) void hub.openWorkspace(root).catch((e) => onError?.(e));
  };
  const [ws, setWs] = useState<WorkspaceInfo | null>(null);
  useEffect(() => {
    let current = true;
    setWs(null);
    if (port) port.workspaces().then((value) => { if (current) setWs(value); }).catch(() => { if (current) setWs(null); });
    return () => { current = false; };
  }, [port, root]);

  const gathered = useFolded("bar");
  const [more, setMore] = useState(false);
  const group = useRef<HTMLDivElement>(null);
  const shut = useCallback(() => setMore(false), []);
  useDismiss(more && gathered, group, shut);
  const trigger = useRef<HTMLButtonElement>(null);
  const wasOpen = useRef(false);
  useEffect(() => {
    // Escape leaves focus on a control that has just been hidden; give it back to the button that opened it.
    const at = document.activeElement;
    const lost = !at || at === document.body || !!document.getElementById("chrome-fold")?.contains(at);
    if (wasOpen.current && !more && lost) trigger.current?.focus();
    wasOpen.current = more;
  }, [more]);

  return (
    <>
    {hub && <DeviceBar hub={hub} />}
    <div className="chrome" onDoubleClick={zoomOnTitleBar}>

      <div className="crumb">
        <span className="crumb-context">
          <button
            type="button"
            className="crumb-folder"
            data-action="chrome.open-project"
            aria-label={t("打开项目文件夹")}
            title={t("打开项目文件夹")}
            disabled={!canOpenProject}
            onClick={openProject}
          >
            <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true"><path d="M2.5 4.5h4l1.2 1.4h5.8v6.6h-11z" /></svg>
          </button>
          <span className="crumb-proj" title={root}>
            {project}
          </span>
          <span className="isolab" hidden={!ws || ws.current !== root} title={root}>
            {ws?.isolated ? t("隔离目录") : t("原目录")}
          </span>
          <span className="isolab hostlab" hidden={!host}>
            {host}
          </span>
          <span className="crumbsep">/</span>
        </span>
        <b title={status?.sessionPath}>{sessionName(title, status?.sessionPath)}</b>
        <span className="crumb-slot" ref={onSlot} />
      </div>

      <span className="badge" hidden={steer === 0}>
        {t("插话待送达")} <b>{steer}</b>
      </span>

      <div className="r" ref={group}>
        {gathered && (
          <button ref={trigger} className="thbtn r-more" data-action="chrome.more" aria-haspopup="true" aria-controls="chrome-fold" aria-expanded={more} aria-label={t("更多")} onClick={() => setMore((v) => !v)}>
            <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M3.5 8h.01M8 8h.01M12.5 8h.01" /></svg>
          </button>
        )}
        <div className="r-fold" id="chrome-fold" data-open={more ? "" : undefined}>
        {hub && onError && <PhonePop hub={hub} />}
        {port && (
          <TooltipButton
            className="thbtn find-action"
            data-action="transcript.find"
            data-value="open"
            onClick={onFind}
            aria-label={t("在这段对话里查找")}
            aria-keyshortcuts={ariaChord("f")}
            title={t("在这段对话里查找")} shortcut={chord("F")}
          >
            <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M7.2 3.1a4.1 4.1 0 1 0 0 8.2 4.1 4.1 0 0 0 0-8.2M10.3 10.3 13 13" /></svg>
          </TooltipButton>
        )}
        <TooltipButton
          className="thbtn theme-toggle"
          data-action="appearance.theme"
          onClick={onTheme}
          aria-label={theme === "dark" ? t("切换浅色主题") : t("切换深色主题")}
          title={theme === "dark" ? t("切换浅色主题") : t("切换深色主题")}
        >
          {theme === "dark" ? (
            <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M12.9 9.7A5.3 5.3 0 0 1 6.3 3.1a5.3 5.3 0 1 0 6.6 6.6Z" /></svg>
          ) : (
            <svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="2.7" /><path d="M8 1.7v1.4M8 12.9v1.4M1.7 8h1.4M12.9 8h1.4M3.5 3.5l1 1M11.5 11.5l1 1M3.5 12.5l1-1M11.5 4.5l1-1" /></svg>
          )}
        </TooltipButton>
        {port && <ViewSwitch view={view} rows={rows} surfaces={surfaces} onView={onView} />}
        </div>
        <span className="account-presence" title={account?.signedIn ? account.user?.label : t("未登录")} />
        <WindowControls />
      </div>
    </div>
    </>
  );
}
