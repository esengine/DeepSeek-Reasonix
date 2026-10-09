import { useEffect, useState } from "react";
import { t } from "../i18n";
import { host as hostBridge } from "../port/host";
import type { HubPort } from "../port/hub";
import type { AccountState, AgentPort, SessionStatus, WorkspaceInfo } from "../port/port";
import { DeviceBar } from "./DeviceBar";
import { ariaChord, chord } from "./keys";
import { PhonePop } from "./PhonePop";
import { WindowControls, zoomOnTitleBar } from "./WindowControls";

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
  onSettings: (section?: string) => void;
  onBrowser: () => void;
  browser: boolean;
  account: AccountState | null;
  rail: boolean;
  theme: string;
  onRail: () => void;
  onTheme: () => void;
  onFind: () => void;
  // The window's port, for what belongs to the window rather than a pane.
  hub?: HubPort;
  onError?: (e: unknown) => void;
}

export function Chrome({ port, status, title, steer, onSettings, onBrowser, browser, account, host, rail, theme, onRail, onTheme, onFind, hub, onError }: Props) {
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
  // Only for the "隔离" tag: the folder list and the switch itself moved to the
  // sidebar, where adding one and opening one are the same gesture.
  const [ws, setWs] = useState<WorkspaceInfo | null>(null);
  useEffect(() => {
    if (!port) {
      setWs(null);
      return;
    }
    port.workspaces().then(setWs).catch(() => setWs(null));
  }, [port, root]);

  return (
    <>
    {hub && <DeviceBar hub={hub} />}
    <div className="chrome" onDoubleClick={zoomOnTitleBar}>
      <button className="thbtn studio-menu" data-action="chrome.rail" onClick={onRail} aria-pressed={rail} aria-label={rail ? t("收起工作区栏") : t("展开工作区栏")}>
        <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M3 4h10M3 8h10M3 12h10" /></svg>
      </button>

      <div className="crumb">
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
        <span className="isolab" hidden={!ws?.isolated}>
          {t("隔离")}
        </span>
        {/* Same slot as the isolated tag: both answer "this workspace is not an
            ordinary folder on this machine", so they read as one language. */}
        <span className="isolab hostlab" hidden={!host}>
          {host}
        </span>
        <span className="crumbsep">/</span>
        <b title={status?.sessionPath}>{sessionName(title, status?.sessionPath)}</b>
      </div>

      <span className="badge" hidden={steer === 0}>
        {t("插话待送达")} <b>{steer}</b>
      </span>

      <div className="r">
        {hub && onError && <PhonePop hub={hub} />}
        {port && (
          <button
            className="thbtn find-action"
            data-action="transcript.find"
            data-value="open"
            onClick={onFind}
            aria-label={t("在这段对话里查找")}
            aria-keyshortcuts={ariaChord("f")}
            title={`${t("在这段对话里查找")}  ${chord("F")}`}
          >
            <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M7.2 3.1a4.1 4.1 0 1 0 0 8.2 4.1 4.1 0 0 0 0-8.2M10.3 10.3 13 13" /></svg>
          </button>
        )}
        <button
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
        </button>
        <button className="top-action" data-action="settings.section" data-value="tools" onClick={() => onSettings("tools")} aria-label={t("沙盒")} title={t("沙盒与运行边界")}>
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m8 2.5 5 2.7v5.6l-5 2.7-5-2.7V5.2Z" /><path d="m3 5.2 5 2.7 5-2.7M8 7.9v5.6" /></svg><span>{t("沙盒")}</span>
        </button>
        <button className="thbtn browser-action" data-action="browser.open" onClick={onBrowser} aria-pressed={browser} aria-label="Browser" title={browser ? t("关闭内置 Browser") : t("打开内置 Browser")}>
          <svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="5.4" /><path d="M2.8 8h10.4M8 2.6c1.5 1.5 2.3 3.3 2.3 5.4S9.5 11.9 8 13.4C6.5 11.9 5.7 10.1 5.7 8S6.5 4.1 8 2.6Z" /></svg>
        </button>
        <span className="account-presence" title={account?.signedIn ? account.user?.label : t("未登录")} />
        <WindowControls />
      </div>
    </div>
    </>
  );
}
