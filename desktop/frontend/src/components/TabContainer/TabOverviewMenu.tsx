// TabOverviewMenu is the dock strip's leading dropdown: every open tab with
// when it was opened, every recently closed tab with when it was closed (click
// to reopen), a filter over both lists, and the close-current/others/all
// actions. Relative times refresh while the menu is open so a tab that was
// just closed does not keep reading "just now".
import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, Search } from "lucide-react";
import { useT } from "../../lib/i18n";
import { useActivityBarStore, type TabItem } from "../../store/activityBar";
import { DOCK_ENTRY_ICONS } from "../dockEntryIcons";

function relativeTime(timestamp: number, now: number, t: ReturnType<typeof useT>): string {
  const minutes = Math.floor(Math.max(0, now - timestamp) / 60000);
  if (minutes < 1) return t("rightDock.time.justNow");
  if (minutes < 60) return t("rightDock.time.minutesAgo", { count: String(minutes) });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("rightDock.time.hoursAgo", { count: String(hours) });
  return t("rightDock.time.daysAgo", { count: String(Math.floor(hours / 24)) });
}

function matches(tab: TabItem, query: string): boolean {
  if (!query) return true;
  return tab.label.toLowerCase().includes(query);
}

export function TabOverviewMenu() {
  const t = useT();
  const tabs = useActivityBarStore((state) => state.tabs);
  const activeTabId = useActivityBarStore((state) => state.activeTabId);
  const recentlyClosed = useActivityBarStore((state) => state.recentlyClosed);
  const activateTab = useActivityBarStore((state) => state.activateTab);
  const closeTab = useActivityBarStore((state) => state.closeTab);
  const reopenTab = useActivityBarStore((state) => state.reopenTab);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [now, setNow] = useState(() => Date.now());
  const rootRef = useRef<HTMLDivElement | null>(null);
  const searchRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (!open) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 60000);
    return () => window.clearInterval(timer);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (rootRef.current && event.target instanceof Node && !rootRef.current.contains(event.target)) setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    window.addEventListener("pointerdown", onPointerDown, true);
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown, true);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  const needle = query.trim().toLowerCase();
  const visibleTabs = useMemo(() => tabs.filter((tab) => matches(tab, needle)), [needle, tabs]);
  const visibleClosed = useMemo(
    () => recentlyClosed.filter((record) => matches(record.tab, needle)),
    [needle, recentlyClosed],
  );

  const toggle = () => {
    const next = !open;
    setOpen(next);
    setQuery("");
    if (next) window.setTimeout(() => searchRef.current?.focus(), 0);
  };

  const row = (tab: TabItem, active: boolean, trailing: string, onSelect: () => void) => {
    const Icon = DOCK_ENTRY_ICONS[tab.type];
    return (
      <button
        key={tab.id}
        type="button"
        role="menuitem"
        className={`tab-overview__row${active ? " tab-overview__row--active" : ""}`}
        onClick={onSelect}
      >
        <Icon size={14} className="tab-overview__row-icon" />
        <span className="tab-overview__row-label">{tab.label}</span>
        <span className="tab-overview__row-time">{trailing}</span>
      </button>
    );
  };

  return (
    <div className="tab-overview" ref={rootRef}>
      <button
        type="button"
        className={`workbench-dock__tab-overview${open ? " workbench-dock__tab-overview--open" : ""}`}
        aria-label={t("rightDock.tabOverview")}
        aria-expanded={open}
        onClick={toggle}
      >
        <ChevronDown size={14} />
      </button>
      {open && (
        <div className="tab-overview__menu" role="menu" aria-label={t("rightDock.tabOverview")}>
          <div className="tab-overview__search">
            <Search size={13} />
            <input
              ref={searchRef}
              type="text"
              value={query}
              placeholder={t("rightDock.searchTabs")}
              onChange={(event) => setQuery(event.target.value)}
            />
          </div>
          {visibleTabs.length === 0 && visibleClosed.length === 0 ? (
            <div className="tab-overview__note">{t("rightDock.noTabsFound")}</div>
          ) : null}
          {visibleTabs.length > 0 ? (
            <>
              <div className="tab-overview__section">{t("rightDock.openTabs")}</div>
              {visibleTabs.map((tab) => row(tab, tab.id === activeTabId,
                tab.openedAt ? t("rightDock.openedRelativeTime", { time: relativeTime(tab.openedAt, now, t) }) : "",
                () => { activateTab(tab.id); setOpen(false); }))}
            </>
          ) : null}
          {visibleClosed.length > 0 ? (
            <>
              <div className="tab-overview__section">{t("rightDock.recentlyClosedTabs")}</div>
              {visibleClosed.map((record) => row(record.tab, false,
                t("rightDock.closedRelativeTime", { time: relativeTime(record.closedAt, now, t) }),
                () => { reopenTab(record.tab.id); setOpen(false); }))}
            </>
          ) : null}
          <div className="tab-overview__actions">
            <button type="button" role="menuitem" disabled={!activeTabId}
              onClick={() => { if (activeTabId) closeTab(activeTabId); setOpen(false); }}>
              {t("rightDock.closeCurrentTab")}
            </button>
            <button type="button" role="menuitem" disabled={tabs.length <= 1}
              onClick={() => {
                tabs.filter((tab) => tab.id !== activeTabId).forEach((tab) => closeTab(tab.id));
                setOpen(false);
              }}>
              {t("rightDock.closeOtherTabs")}
            </button>
            <button type="button" role="menuitem" disabled={tabs.length === 0}
              onClick={() => { tabs.forEach((tab) => closeTab(tab.id)); setOpen(false); }}>
              {t("rightDock.closeAllTabs")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
