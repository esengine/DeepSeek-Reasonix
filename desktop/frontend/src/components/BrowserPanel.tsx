import { ArrowLeft, ArrowRight, Bug, Compass, Download, Hand, Plus, RotateCw, TriangleAlert, X, ZoomIn, ZoomOut } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type RefObject } from "react";

import { zoomPercent } from "../lib/browserAddress";
import type { BrowserDownloadView, BrowserTabView } from "../lib/browserHost";
import { useBrowserCopy, type BrowserCopy } from "../lib/browserPanelCopy";
import { selectActiveTab, selectAddress, useBrowserPanelStore } from "../lib/browserPanelStore";
import { desktopHost } from "../lib/desktopHost";
import { useI18n } from "../lib/i18n";
import { useToast } from "../lib/toast";
import { useBrowserSurfaceLayout } from "../lib/useBrowserSurfaceLayout";

const DOWNLOAD_STRIP_LIMIT = 5;

// The dock tab's label bypasses the shared dictionaries because both locale
// chunks sit on their bundle ratchet (check-bundle-budget.mjs).
const DOCK_TAB_LABEL: Record<string, string> = { zh: "浏览器", "zh-TW": "瀏覽器" };

/** Browser entry in the workspace dock's tab bar; mirrors the DockTab markup. */
export function BrowserDockTab({ active, onSelect }: { active: boolean; onSelect: () => void }) {
  const { locale } = useI18n();
  return (
    <button type="button" role="tab" aria-selected={active} className={`workbench-dock__tab${active ? " workbench-dock__tab--active" : ""}`} onClick={onSelect}>
      <Compass size={13} /><span className="workbench-dock__tab-label">{DOCK_TAB_LABEL[locale] ?? "Browser"}</span>
    </button>
  );
}

export function BrowserPanel({ taskId }: { taskId: string | undefined }) {
  const copy = useBrowserCopy();
  const { showToast } = useToast();
  const host = desktopHost().browser;
  const tabs = useBrowserPanelStore((state) => state.shown);
  const activeTab = useBrowserPanelStore(selectActiveTab);
  const address = useBrowserPanelStore(selectAddress);
  const downloads = useBrowserPanelStore((state) => state.downloads);
  const addressRef = useRef<HTMLInputElement>(null);
  const [surface, setSurface] = useState<HTMLDivElement | null>(null);
  const notifyRef = useRef((message: string) => showToast(copy.actionFailed(message), "error"));
  notifyRef.current = (message) => showToast(copy.actionFailed(message), "error");

  useEffect(() => {
    if (!host) return;
    return useBrowserPanelStore.getState().attach(host, (message) => notifyRef.current(message));
  }, [host]);
  useEffect(() => useBrowserPanelStore.getState().setTaskId(taskId), [taskId]);
  useEffect(() => {
    useBrowserPanelStore.getState().setVisible(true);
    return () => useBrowserPanelStore.getState().setVisible(false);
  }, []);
  useBrowserSurfaceLayout(host, surface);

  const focusAddress = useCallback(() => {
    addressRef.current?.focus();
    addressRef.current?.select();
  }, []);
  const onPanelKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if ((event.metaKey || event.ctrlKey) && !event.altKey && event.key.toLowerCase() === "l") {
      event.preventDefault();
      focusAddress();
    }
  };
  const store = useBrowserPanelStore.getState;
  const openFromDraft = async () => {
    if (!(await store().openDraft())) focusAddress();
  };
  const addressBar = <AddressBar copy={copy} value={address} inputRef={addressRef} tabId={activeTab?.id ?? null} />;

  return (
    <section className="browser-panel" aria-label={copy.panel} onKeyDown={onPanelKeyDown}>
      {tabs.length > 0 && (
        <div className="browser-panel__tabs" role="tablist" aria-label={copy.tabs}>
          {tabs.map((tab) => (
            <BrowserTab key={tab.id} tab={tab} active={tab.id === activeTab?.id} copy={copy} />
          ))}
          <button type="button" className="browser-panel__icon-btn browser-panel__new-tab" aria-label={copy.newTab} onClick={() => void openFromDraft()}>
            <Plus size={14} />
          </button>
        </div>
      )}
      {tabs.length > 0 && (
        <div className="browser-panel__toolbar">
          <button type="button" className="browser-panel__icon-btn" aria-label={copy.back} disabled={!activeTab?.canGoBack}
            onClick={() => activeTab && void store().navigate(activeTab.id, { action: "back" })}><ArrowLeft size={14} /></button>
          <button type="button" className="browser-panel__icon-btn" aria-label={copy.forward} disabled={!activeTab?.canGoForward}
            onClick={() => activeTab && void store().navigate(activeTab.id, { action: "forward" })}><ArrowRight size={14} /></button>
          {activeTab?.loading
            ? <button type="button" className="browser-panel__icon-btn" aria-label={copy.stop} onClick={() => void store().navigate(activeTab.id, { action: "stop" })}><X size={14} /></button>
            : <button type="button" className="browser-panel__icon-btn" aria-label={copy.reload} disabled={!activeTab}
              onClick={() => activeTab && void store().navigate(activeTab.id, { action: "reload" })}><RotateCw size={14} /></button>}
          {addressBar}
          {activeTab?.mode === "agent" && (
            <button type="button" className="btn btn--small" onClick={() => void store().takeover(activeTab.id)}>{copy.takeControl}</button>
          )}
          <button type="button" className="browser-panel__icon-btn" aria-label={copy.zoomOut} disabled={!activeTab}
            onClick={() => activeTab && void store().zoom(activeTab.id, -1)}><ZoomOut size={14} /></button>
          <button type="button" className="browser-panel__zoom" aria-label={copy.zoomReset(zoomPercent(activeTab?.zoom ?? 1))} disabled={!activeTab}
            onClick={() => activeTab && void store().zoom(activeTab.id, 0)}>{zoomPercent(activeTab?.zoom ?? 1)}%</button>
          <button type="button" className="browser-panel__icon-btn" aria-label={copy.zoomIn} disabled={!activeTab}
            onClick={() => activeTab && void store().zoom(activeTab.id, 1)}><ZoomIn size={14} /></button>
          <button type="button" className="browser-panel__icon-btn" aria-label={copy.devTools} disabled={!activeTab}
            onClick={() => activeTab && void store().toggleDevTools(activeTab.id)}><Bug size={14} /></button>
        </div>
      )}
      {activeTab?.mode === "human" && (
        <div className="browser-panel__takeover" role="status">
          <Hand size={14} aria-hidden="true" />
          <span>{copy.takeover}</span>
          <button type="button" className="btn btn--small" onClick={() => void store().resume(activeTab.id)}>{copy.resume}</button>
        </div>
      )}
      <div className="browser-panel__content">
        {tabs.length === 0 ? (
          <div className="browser-panel__empty">
            <Compass size={28} aria-hidden="true" />
            <p className="browser-panel__empty-title">{copy.emptyTitle}</p>
            <p className="browser-panel__empty-hint">{copy.emptyHint}</p>
            {addressBar}
          </div>
        ) : activeTab?.error ? (
          <div className="browser-panel__error" role="alert">
            <TriangleAlert size={22} aria-hidden="true" />
            <p className="browser-panel__error-title">{copy.errorTitle}</p>
            <p className="browser-panel__error-detail">{copy.errorDetail(activeTab.error.code, activeTab.error.description)}</p>
            <code className="browser-panel__error-url">{activeTab.url}</code>
            <button type="button" className="btn btn--small" onClick={() => void store().navigate(activeTab.id, { action: "reload" })}>{copy.retry}</button>
          </div>
        ) : (
          <div ref={setSurface} data-browser-surface="" className="browser-panel__surface" />
        )}
      </div>
      {downloads.length > 0 && <DownloadStrip downloads={downloads} copy={copy} />}
    </section>
  );
}

function AddressBar({ copy, value, inputRef, tabId }: { copy: BrowserCopy; value: string; inputRef: RefObject<HTMLInputElement | null>; tabId: string | null }) {
  const store = useBrowserPanelStore.getState;
  return (
    <input
      ref={inputRef}
      className="browser-panel__address"
      type="text"
      aria-label={copy.address}
      placeholder={copy.addressPlaceholder}
      spellCheck={false}
      autoComplete="off"
      value={value}
      onChange={(event) => store().setDraft(tabId, event.target.value)}
      onFocus={(event) => event.currentTarget.select()}
      onKeyDown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault();
          void store().submitAddress();
        } else if (event.key === "Escape") {
          store().clearDraft(tabId);
          event.currentTarget.blur();
        }
      }}
    />
  );
}

function BrowserTab({ tab, active, copy }: { tab: BrowserTabView; active: boolean; copy: BrowserCopy }) {
  const title = tab.title || tab.url || copy.untitled;
  const store = useBrowserPanelStore.getState;
  return (
    <div className={`browser-tab${active ? " browser-tab--active" : ""}`}>
      <button type="button" role="tab" aria-selected={active} className="browser-tab__select" title={tab.url} onClick={() => store().activate(tab.id)}>
        {tab.loading && <span className="browser-tab__spinner" role="img" aria-label={copy.loading} />}
        <span className="browser-tab__title">{title}</span>
        {tab.temporary && <span className="browser-tab__badge">{copy.temporary}</span>}
      </button>
      <button type="button" className="browser-tab__close" aria-label={copy.closeTab(title)} onClick={() => void store().close(tab.id)}>
        <X size={12} />
      </button>
    </div>
  );
}

function downloadProgress(download: BrowserDownloadView, copy: BrowserCopy): string {
  if (download.state !== "progressing") return copy.downloadState[download.state];
  if (download.total > 0) return `${Math.min(100, Math.round((download.received / download.total) * 100))}%`;
  return copy.downloadState.progressing;
}

function DownloadStrip({ downloads, copy }: { downloads: BrowserDownloadView[]; copy: BrowserCopy }) {
  return (
    <div className="browser-panel__downloads" aria-label={copy.downloads}>
      <div className="browser-panel__downloads-head">
        <Download size={12} aria-hidden="true" />
        <span>{copy.downloads}</span>
        <button type="button" className="browser-panel__icon-btn" aria-label={copy.clearDownloads} onClick={() => useBrowserPanelStore.getState().clearDownloads()}>
          <X size={12} />
        </button>
      </div>
      <ul className="browser-panel__download-list">
        {downloads.slice(0, DOWNLOAD_STRIP_LIMIT).map((download) => (
          <li key={download.id} className={`browser-download browser-download--${download.state}`} title={download.path}>
            <span className="browser-download__name">{download.filename}</span>
            <span className="browser-download__state">{downloadProgress(download, copy)}</span>
            {download.state === "progressing" && (
              <progress className="browser-download__bar" max={download.total > 0 ? download.total : undefined} value={download.total > 0 ? download.received : undefined} aria-label={download.filename} />
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
