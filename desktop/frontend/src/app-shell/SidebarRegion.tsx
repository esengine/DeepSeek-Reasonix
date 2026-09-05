import { lazy, Suspense, type ComponentProps, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";
import { AlarmClock, Brain, Command, MessageSquare, PanelLeft, PanelRight, Search, Settings, SquarePen, Trash2 } from "lucide-react";
import { Tooltip } from "../components/Tooltip";
import type { Translator } from "../lib/i18n";
import type { SettingsTab } from "../lib/types";
import logoWordmark from "../assets/logo-wordmark.svg";

const ProjectTree = lazy(() => import("../components/ProjectTree").then((module) => ({ default: module.ProjectTree })));

type SidebarRegionProps = {
  className: string;
  workbench: boolean;
  creation: boolean;
  collapsed: boolean;
  navTooltipDisabled: boolean;
  searchOpen: boolean;
  togglePressed: boolean;
  toggleTitle: string;
  resize: {
    min: number;
    max: number;
    value: number;
    onPointerDown: (event: PointerEvent<HTMLButtonElement>) => void;
    onKeyDown: (event: KeyboardEvent<HTMLButtonElement>) => void;
    onReset: () => void;
  };
  projectTree: ComponentProps<typeof ProjectTree>;
  t: Translator;
  onNewSession: () => void;
  onOpenTrash: () => void;
  onOpenAutomation: () => void;
  onOpenSettings: (tab: SettingsTab) => void;
  onToggleSearch: () => void;
  onToggle: () => void;
};

/** Sidebar presentation shared by classic, workbench and creation layouts. */
export function SidebarRegion(props: SidebarRegionProps) {
  const { t } = props;
  return (
    <>
      <aside className={props.className} aria-label={t("sidebar.navigation")}>
        {props.workbench ? (
          <>
            <div className="sidebar__head" aria-hidden={props.collapsed}>
              <div className="sidebar__brand sidebar__brand--workbench">
                <img src={logoWordmark} alt="Reasonix" className="sidebar__brand-logo sidebar__brand-logo--workbench" draggable={false} />
              </div>
            </div>
            <div className="sidebar__quick-actions">
              <button className="sidebar__quick-action" type="button" onClick={props.onNewSession}>
                <MessageSquare size={18} aria-hidden="true" /><span>{t("topbar.newSession")}</span>
              </button>
            </div>
          </>
        ) : (
          <>
            <div className="sidebar__brand" aria-hidden={props.collapsed}>
              <img src={logoWordmark} alt="Reasonix" className="sidebar__brand-logo" draggable={false} />
            </div>
            <button className="sidebar__new" onClick={props.onNewSession}>
              <SquarePen size={18} /><span>{props.creation ? t("creation.sidebar.newChat") : t("topbar.newSession")}</span>
            </button>
          </>
        )}
        {props.creation && (
          <section className="sidebar-feature-zone" aria-label={t("settings.title")}>
            <div className="sidebar-feature-zone__title">{t("creation.sidebar.features")}</div>
            <div className="sidebar-feature-zone__items">
              <FeatureButton icon={<Command size={14} />} label={t("creation.sidebar.skills")} onClick={() => props.onOpenSettings("skills")} />
              <FeatureButton icon={<Brain size={14} />} label={t("settings.tab.memory")} onClick={() => props.onOpenSettings("memory")} />
              <FeatureButton icon={<MessageSquare size={14} />} label={t("creation.sidebar.messageChannels")} onClick={() => props.onOpenSettings("bots")} />
              <FeatureButton icon={<AlarmClock size={14} />} label={t("sidebar.automation")} onClick={props.onOpenAutomation} />
            </div>
          </section>
        )}
        <section className="sidebar__section sidebar__section--projects">
          <Suspense fallback={null}><ProjectTree {...props.projectTree} /></Suspense>
        </section>
        {props.workbench ? (
          <nav className="sidebar__nav sidebar__nav--footer">
            <div className="sidebar__utility-row" aria-label={t("sidebar.utilityActions")}>
              <UtilityButton label={t("sidebar.trash")} icon={<Trash2 size={16} />} onClick={props.onOpenTrash} />
              <UtilityButton label={t("heartbeat.scheduler")} icon={<AlarmClock size={16} />} onClick={props.onOpenAutomation} />
              <UtilityButton label={t("topbar.settings")} icon={<Settings size={16} />} onClick={() => props.onOpenSettings("general")} />
            </div>
          </nav>
        ) : (
          <nav className="sidebar__nav">
            {props.creation && (
              <Tooltip label={t("projectTree.searchPlaceholder")} fill side="right" disabled={props.navTooltipDisabled}>
                <button className={`sidebar__navitem sidebar__navitem--search${props.searchOpen ? " sidebar__navitem--active" : ""}`} type="button"
                  aria-label={t("projectTree.searchPlaceholder")} aria-pressed={props.searchOpen} onClick={props.onToggleSearch}>
                  <Search size={15} /><span>{t("tabBar.commandSearchCompact")}</span>
                </button>
              </Tooltip>
            )}
            <NavButton label={t("sidebar.trash")} icon={<Trash2 size={15} />} disabledTooltip={props.navTooltipDisabled} onClick={props.onOpenTrash} />
            {!props.creation && <NavButton label={t("heartbeat.scheduler")} icon={<AlarmClock size={15} />} disabledTooltip={props.navTooltipDisabled} onClick={props.onOpenAutomation} />}
            <NavButton label={t("topbar.settings")} icon={<Settings size={15} />} disabledTooltip={props.navTooltipDisabled} onClick={() => props.onOpenSettings("general")} />
          </nav>
        )}
      </aside>
      <button className="sidebar-resizer" type="button" role="separator" aria-orientation="vertical" aria-label={t("sidebar.resize")}
        aria-valuemin={props.resize.min} aria-valuemax={props.resize.max} aria-valuenow={props.resize.value}
        onPointerDown={props.resize.onPointerDown} onKeyDown={props.resize.onKeyDown} onDoubleClick={props.resize.onReset} />
      {props.creation && (
        <button className={`sidebar-collapse-toggle${props.collapsed ? " sidebar-collapse-toggle--collapsed" : ""}${props.togglePressed ? " sidebar-collapse-toggle--pressed" : ""}`}
          type="button" onClick={props.onToggle} aria-label={props.toggleTitle} aria-pressed={!props.collapsed} title={props.toggleTitle}>
          {props.collapsed ? <PanelRight size={14} /> : <PanelLeft size={14} />}
        </button>
      )}
    </>
  );
}

function FeatureButton({ icon, label, onClick }: { icon: ReactNode; label: string; onClick: () => void }) {
  return <button className="sidebar-feature-zone__item" type="button" onClick={onClick}>{icon}<span>{label}</span></button>;
}

function UtilityButton({ icon, label, onClick }: { icon: ReactNode; label: string; onClick: () => void }) {
  return <Tooltip label={label} fill side="top"><button className="sidebar__utility-button" type="button" onClick={onClick}>{icon}<span className="sr-only">{label}</span></button></Tooltip>;
}

function NavButton({ icon, label, disabledTooltip, onClick }: { icon: ReactNode; label: string; disabledTooltip: boolean; onClick: () => void }) {
  return <Tooltip label={label} fill side="right" disabled={disabledTooltip}><button className="sidebar__navitem" onClick={onClick}>{icon}<span>{label}</span></button></Tooltip>;
}
