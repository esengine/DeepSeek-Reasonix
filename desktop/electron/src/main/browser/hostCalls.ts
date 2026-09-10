import type { BrowserNavigateTarget } from "../../shared/ipc.js";
import type { HostCallTable } from "../hostCalls.js";
import { bool, num, str, strList, type Params } from "../params.js";
import type { ActionExecutor, ActResult } from "./actions.js";
import type { DocumentRegistry } from "./documents.js";
import type { DownloadTracker, HostDownload } from "./downloads.js";
import { takenOver } from "./errors.js";
import type { GrantRegistry } from "./grants.js";
import { captureScreenshot, type ScreenshotDeps, type ScreenshotResult } from "./screenshot.js";
import { takeSnapshot, type SnapshotResult } from "./snapshot.js";
import type { BrowserSurfaceManager, BrowserTab } from "./surfaceManager.js";

export interface HostBrowserTab {
  id: string;
  url: string;
  title: string;
  loading: boolean;
  temporary: boolean;
}

export interface BrowserHostDeps {
  surfaces: BrowserSurfaceManager;
  grants: GrantRegistry;
  documents: DocumentRegistry;
  actions: ActionExecutor;
  downloads: DownloadTracker;
  snapshot?(tab: BrowserTab, selector: string): Promise<SnapshotResult>;
  screenshot?(tab: BrowserTab, request: { ref: string; fullPage: boolean; directory: string }): Promise<ScreenshotResult>;
  screenshotDeps?: ScreenshotDeps;
}

export function hostTab(surfaces: BrowserSurfaceManager, tab: BrowserTab): HostBrowserTab {
  const view = surfaces.view(tab);
  return { id: view.id, url: view.url, title: view.title, loading: view.loading, temporary: view.temporary };
}

// Every call after grant re-verifies the grant and that the browser tab
// belongs to the grant's task; reads and writes both refuse a tab the user
// is operating (-32011).
export function buildBrowserHostCalls(deps: BrowserHostDeps): HostCallTable {
  const { surfaces, grants, documents, downloads } = deps;
  const boundTab = (params: Params): BrowserTab => {
    const tab = surfaces.get(str(params, "tabId"));
    grants.verifyTab(str(params, "grantId"), tab?.taskId);
    return tab as BrowserTab;
  };
  const agentTab = (params: Params): BrowserTab => {
    const tab = boundTab(params);
    if (tab.mode !== "agent") throw takenOver(`tab ${tab.id} is in human mode`);
    return tab;
  };
  const snapshot = deps.snapshot ?? ((tab, selector) => takeSnapshot(tab.view.page, tab.id, tab.epoch, selector, documents));
  const screenshot = deps.screenshot ?? ((tab, request) => {
    const token = documents.currentToken(tab.id);
    const binding = token ? documents.lookup(token) ?? null : null;
    return captureScreenshot(tab.view.page, binding, tab.view.page.getZoomFactor() || 1, request, deps.screenshotDeps);
  });

  return {
    "host/browser.grant": (params) => {
      grants.install({ grantId: str(params, "grantId"), taskId: str(params, "tabId"), sessionId: str(params, "sessionId") });
      return {};
    },
    "host/browser.revoke": (params) => {
      const grant = grants.revoke(str(params, "grantId"));
      if (grant) for (const tab of surfaces.tabsForTask(grant.taskId)) documents.invalidateTab(tab.id);
      return {};
    },
    "host/browser.tabs.list": (params) => {
      const grant = grants.verify(str(params, "grantId"));
      return { tabs: surfaces.tabsForTask(grant.taskId).map((tab) => hostTab(surfaces, tab)) };
    },
    "host/browser.tabs.open": async (params) => {
      const grant = grants.verify(str(params, "grantId"));
      const tab = await surfaces.open(str(params, "url"), { taskId: grant.taskId, temporary: bool(params, "temporary") });
      return hostTab(surfaces, tab);
    },
    "host/browser.tabs.navigate": async (params) => {
      const tab = agentTab(params);
      const action = str(params, "action");
      const target: BrowserNavigateTarget = action === "back" || action === "forward" || action === "reload" ? { action } : { url: str(params, "url") };
      await surfaces.navigate(tab.id, target);
      return hostTab(surfaces, tab);
    },
    "host/browser.tabs.close": (params) => {
      const tab = boundTab(params);
      documents.invalidateTab(tab.id);
      downloads.forgetTab(tab.id);
      surfaces.close(tab.id);
      return {};
    },
    "host/browser.snapshot": (params) => snapshot(agentTab(params), str(params, "selector")),
    "host/browser.act": (params): Promise<ActResult> => {
      const tab = boundTab(params);
      const grantId = str(params, "grantId");
      const directory = str(params, "directory");
      if (directory !== "") downloads.setTaskDirectory(tab.taskId, directory);
      return deps.actions.act(
        tab,
        {
          operationId: str(params, "operationId"),
          tabId: tab.id,
          documentToken: str(params, "documentToken"),
          action: str(params, "action"),
          ref: str(params, "ref"),
          text: str(params, "text"),
          keys: str(params, "keys"),
          options: strList(params, "options"),
          files: strList(params, "files"),
          submit: bool(params, "submit"),
          deltaX: num(params, "deltaX"),
          deltaY: num(params, "deltaY"),
        },
        () => grants.verifyTab(grantId, surfaces.get(tab.id)?.taskId),
      );
    },
    "host/browser.screenshot": (params) => {
      const tab = agentTab(params);
      const directory = str(params, "directory");
      if (directory !== "") downloads.setTaskDirectory(tab.taskId, directory);
      return screenshot(tab, { ref: str(params, "ref"), fullPage: bool(params, "fullPage"), directory });
    },
    "host/browser.downloads": async (params): Promise<{ downloads: HostDownload[] }> => {
      const tab = boundTab(params);
      return { downloads: await downloads.wait(tab.id, num(params, "waitForMs")) };
    },
  };
}
