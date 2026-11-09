import { StudioIcon } from "./StudioIcon";
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
import { workspaceDirectoryDescription } from "./workspaceDirectory";
import { WindowControls, zoomOnTitleBar } from "./WindowControls";
import type { PaneView } from "../state/paneview";
import { useReachable, useViewer } from "../state/viewer";

const base = (p: string) => p.replace(/[/\\]+$/, "").split(/[/\\]/).pop() || p;
// The filename is a timestamp and a model ref — true, and useless to read. The
// title from /sessions is always present once the turn is on disk: a generated
// one when it is ready, the first message truncated until then.
const sessionName = (title?: string, p?: string) =>
  title?.trim() || (p ? base(p).replace(/\.jsonl$/, "") : t("common.new_session"));

interface Props {
  // Set when the focused pane is driven from another machine, naming it.
  host?: string;
  // Null while the window has no pane: the chrome still draws, and the few
  // controls that need a session simply do nothing until one is focused.
  port: AgentPort | null;
  status: SessionStatus | null;
  workspace: WorkspaceInfo | null;
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
}

export function Chrome({ port, status, workspace: ws, title, steer, account, host, theme, onTheme, onFind, view, rows, surfaces, onView, hub, onError }: Props) {
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

  const phone = useFolded("phone");
  const gathered = useFolded("bar") || phone;
  const device = useViewer();
  const reachable = useReachable();
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
            aria-label={t("app.open_project_folder")}
            title={t("app.open_project_folder")}
            disabled={!canOpenProject}
            onClick={openProject}
          >
            <StudioIcon name="folder" />
          </button>
          <span className="crumb-proj" title={root}>
            {project}
          </span>
          {ws?.isolated && ws.current === root && <span className="isolab" title={`${workspaceDirectoryDescription(true)}\n${root}`}>
            {t("common.isolated_worktree")}
          </span>}
          <span className="isolab hostlab" hidden={!host}>
            {host}
          </span>
          <span className="crumbsep">/</span>
        </span>
        <b title={status?.sessionPath}>{sessionName(title, status?.sessionPath)}</b>
        {phone && device && (
          <span className="crumb-sub" data-state={!reachable ? "down" : status?.running ? "run" : "ok"}>
            <i aria-hidden="true" />
            {!reachable
              ? t("remote.machine_disconnected", { machine: device.machine })
              : status?.running
                ? t("remote.running_on_machine", { machine: device.machine })
                : `${device.machine} · ${project}`}
          </span>
        )}
      </div>

      <span className="badge" hidden={steer === 0}>
        {t("app.queued")} <b>{steer}</b>
      </span>

      <div className="r" ref={group}>
        {gathered && (
          <button ref={trigger} className="thbtn r-more" data-action="chrome.more" aria-haspopup="true" aria-controls="chrome-fold" aria-expanded={more} aria-label={t("common.more")} onClick={() => setMore((v) => !v)}>
            <StudioIcon name="more" />
          </button>
        )}
        <div className="r-fold" id="chrome-fold" data-open={more ? "" : undefined}>
        {port && (
          <TooltipButton
            className="thbtn find-action"
            data-action="transcript.find"
            data-value="open"
            onClick={onFind}
            aria-label={t("common.find_in_this_conversation")}
            aria-keyshortcuts={ariaChord("f")}
            title={t("common.find_in_this_conversation")} shortcut={chord("F")}
          >
            <StudioIcon name="search" />
          </TooltipButton>
        )}
        {hub && onError && <PhonePop hub={hub} />}
        <TooltipButton
          className="thbtn theme-toggle"
          data-action="appearance.theme"
          onClick={onTheme}
          aria-label={theme === "dark" ? t("app.switch_to_light_theme") : t("app.switch_to_dark_theme")}
          title={theme === "dark" ? t("app.switch_to_light_theme") : t("app.switch_to_dark_theme")}
        >
          {theme === "dark" ? (
            <StudioIcon name="sun" />
          ) : (
            <StudioIcon name="moon" />
          )}
        </TooltipButton>
        {port && <ViewSwitch view={view} rows={rows} surfaces={surfaces} onView={onView} />}
        </div>
        <span className="account-presence" title={account?.signedIn ? account.user?.label : t("common.not_signed_in")} />
        <WindowControls />
      </div>
    </div>
    </>
  );
}
