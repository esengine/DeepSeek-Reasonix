import type { ReactNode } from "react";
import { PanelLeft } from "lucide-react";
import { Tooltip } from "../components/Tooltip";
import { WorktreeBadge } from "../components/WorktreeBadge";

export type TopicbarView = {
  automationReturn: boolean; automationReturnLabel: string;
  chromeHidden: boolean;
  sidebar: { title: string; blocked: boolean; pressed: boolean; collapsed: boolean };
  title: {
    text: string; hover: string; renameLabel: string; editing: boolean;
    draft: string; editSize?: number; canRename: boolean; workspaceLabel?: string;
  };
  subtitle: {
    visible: boolean; title: string; worktreeTabId?: string;
    mergeLabel: string; mergeTooltip: string; sourcePlatform?: string; sourceLabel?: string;
  };
};
type Commands = {
  openAutomation(): void;
  toggleSidebar(): void;
  setTitleDraft(value: string): void;
  commitRename(): void | Promise<void>;
  cancelRename(): void;
  startRename(): void;
  openWorktree(tabId: string): void;
};

/** Presentation only: consume display values separately from stable commands. */
export function TopicbarRegion({ view, commands, children }: {
  view: TopicbarView; commands: Commands; children: ReactNode;
}) {
  const { sidebar, title, subtitle } = view;
  return <header className="topicbar">
    {view.automationReturn && <button className="btn btn--small" type="button" onClick={event => {
      event.currentTarget.focus({ preventScroll: true });
      commands.openAutomation();
    }}>{view.automationReturnLabel}</button>}
    {view.chromeHidden && <Tooltip label={sidebar.title}>
      <button className={[
        "topicbar__chrome-btn", sidebar.blocked ? "topicbar__chrome-btn--blocked" : "",
        sidebar.pressed ? "topicbar__chrome-btn--pressed" : "",
      ].filter(Boolean).join(" ")} type="button"
        onClick={sidebar.blocked ? undefined : commands.toggleSidebar}
        aria-label={sidebar.title} aria-pressed={!sidebar.collapsed} aria-disabled={sidebar.blocked}>
        <PanelLeft size={15} />
      </button>
    </Tooltip>}
    <div className="topicbar__identity">
      <div className="topicbar__title-row">
        {title.editing ? <div className="topicbar__title-edit">
          <input autoFocus className="topicbar__title-input" aria-label={title.renameLabel}
            size={title.editSize} value={title.draft}
            onChange={event => commands.setTitleDraft(event.target.value)}
            onFocus={event => event.currentTarget.select()}
            onKeyDown={event => {
              if (event.key === "Enter") { event.preventDefault(); void commands.commitRename(); }
              if (event.key === "Escape") { event.preventDefault(); commands.cancelRename(); }
            }} onBlur={() => void commands.commitRename()} />
        </div> : title.canRename ? <h1 title={title.hover}>
          <button className="topicbar__title-button" type="button" onClick={commands.startRename} aria-label={title.renameLabel}>
            {title.text}
          </button>
        </h1> : <h1 title={title.hover}>{title.text}</h1>}
        {title.workspaceLabel && <span className="topicbar__workspace-label" title={title.workspaceLabel}>{title.workspaceLabel}</span>}
      </div>
      {subtitle.visible && <div className="topicbar__subtitle" title={subtitle.title}>
        {subtitle.worktreeTabId && <WorktreeBadge size={11} />}
        {subtitle.worktreeTabId && <button type="button" className="topicbar__worktree-btn"
          onClick={() => commands.openWorktree(subtitle.worktreeTabId!)} title={subtitle.mergeTooltip}>
          <span>{subtitle.mergeLabel}</span>
        </button>}
        {subtitle.sourcePlatform && <span className={`topicbar__source-chip topicbar__source-chip--${subtitle.sourcePlatform}`}>{subtitle.sourceLabel}</span>}
      </div>}
    </div>
    <div className="topicbar__spacer" />
    {children}
  </header>;
}
