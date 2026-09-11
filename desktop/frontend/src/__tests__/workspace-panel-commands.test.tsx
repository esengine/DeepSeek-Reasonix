import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useWorkspacePanelCommands } from "../app-runtime/useWorkspacePanelCommands";
import { useSessionNavigationCommands, type SessionNavigationCommandsInput } from "../app-runtime/useSessionNavigationCommands";
import { loadWorkspacePanelOpen, saveWorkspacePanelOpen, useLayoutStore } from "../store/layout";
import { useRemoteStore } from "../store/remote";
import type { RemoteHostView } from "../lib/types";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage,
  IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
let commands!: ReturnType<typeof useWorkspacePanelCommands>;
let closes = 0; let widthClears = 0;
const closeOverlays = () => { closes++; };
const clearLiveWidth = () => { widthClears++; };
let restoredWidth = 0;
const globalRoot = "/fixture/global-workspace";
let navigation!: ReturnType<typeof useSessionNavigationCommands>;
let navigationRequest: unknown;
const setTreeWidth = (width: number) => { restoredWidth = width; };
function Probe({ workspace, creation, visible }: { workspace: string; creation: boolean; visible: boolean }) {
  commands = useWorkspacePanelCommands({ workspaceRoot: workspace, creation, visible, closeOverlays, clearLiveWidth,
    availableWidth: 800, clampTreeWidth: (width) => width, setTreeWidth, gridOpen: visible, t: (key: string) => key } as never);
  navigation = useSessionNavigationCommands({
    activeTab: { id: "fixture", scope: workspace === globalRoot ? "global" : "project", workspaceRoot: workspace },
    closeTransientOverlays: closeOverlays, clearImDetail: () => {}, prepareBlankWorkspace: commands.prepareBlankWorkspace,
    navigation: { enqueueNavigation: async request => { navigationRequest = request; } },
  } as SessionNavigationCommandsInput);
  return null;
}
const paint = (workspace: string, creation = false, visible = false) => act(async () => root.render(<Probe workspace={workspace} creation={creation} visible={visible} />));
try {
  saveWorkspacePanelOpen(false, "A"); saveWorkspacePanelOpen(true, "B");
  await paint("A");
  const first = commands;
  assert.equal(useLayoutStore.getState().workspacePanelOpen, false);
  await act(async () => commands.openRightDockMode("changed"));
  assert.equal(loadWorkspacePanelOpen("A"), true);
  await paint("A", false, true);
  await act(async () => { commands.toggleWorkspaceMaximized(); commands.handleWorkspacePreviewModeChange(true); });
  assert.equal(useLayoutStore.getState().workspacePanelMaximized, true);
  await act(async () => commands.openRightDockMode("context"));
  assert.equal(useLayoutStore.getState().workspacePanelMaximized, false);
  assert.equal(useLayoutStore.getState().workspacePreviewActive, false);
  await act(async () => commands.toggleWorkspacePanel());
  assert.equal(loadWorkspacePanelOpen("A"), false);
  assert.equal(widthClears, 1);
  await paint("B");
  assert.equal(useLayoutStore.getState().workspacePanelOpen, true, "different project restores its own preference");
  await paint("A", true);
  assert.equal(useLayoutStore.getState().workspacePanelOpen, false);
  assert.equal(useLayoutStore.getState().rightDockMode, "files", "Creation cannot leave a hidden overview selected");
  assert.equal(commands.closeWorkspacePanel, first.closeWorkspacePanel);
  assert.equal(commands.openRightDockMode, first.openRightDockMode);
  await act(async () => commands.toggleWorkspacePanel());
  assert.equal(useLayoutStore.getState().rightDockMode, "files");
  const hosts = [{ id: "offline" }, { id: "online" }] as RemoteHostView[];
  await act(async () => {
    useRemoteStore.getState().setHosts(hosts);
    useRemoteStore.getState().applyStatus({ hostId: "online", state: "connected" });
    commands.openRemoteDock();
  });
  assert.equal(useRemoteStore.getState().explorerHostId, "online");
  assert.equal(useRemoteStore.getState().explorerOpen, false, "request is consumed by the same dock owner");
  assert.equal(useLayoutStore.getState().rightDockMode, "remote");
  await act(async () => { commands.restoreWorkspaceDockWidths(640, 0); });
  assert.equal(restoredWidth, 640, "dock width restore clamps through the owner and writes the layout store port");
  await act(async () => useRemoteStore.getState().setHosts([]));
  assert.equal(useLayoutStore.getState().rightDockMode, "files");
  await paint("A");
  await act(async () => commands.openRightDockMode("changed"));
  saveWorkspacePanelOpen(true, "B");
  await act(async () => {
    commands.toggleWorkspaceMaximized();
    commands.prepareBlankWorkspace("B");
  });
  assert.equal(useLayoutStore.getState().workspacePanelOpen, false, "new-session intent collapses the dock immediately");
  assert.equal(useLayoutStore.getState().workspacePanelMaximized, false);
  assert.equal(loadWorkspacePanelOpen("A"), true, "another project's preference is untouched");
  await paint("B");
  assert.equal(useLayoutStore.getState().workspacePanelOpen, false, "destination restoration cannot reopen the blank-session dock");
  await act(async () => commands.openRightDockMode("files"));
  await paint("B");
  assert.equal(useLayoutStore.getState().workspacePanelOpen, true, "manual open stays open on subsequent renders");
  await paint("A");
  assert.equal(useLayoutStore.getState().workspacePanelOpen, true, "ordinary project navigation retains restoration behavior");
  saveWorkspacePanelOpen(true, "");
  saveWorkspacePanelOpen(true, globalRoot);
  await act(async () => navigation.openBlankSession("global", globalRoot));
  assert.deepEqual(navigationRequest, { kind: "blank", scope: "global", workspaceRoot: "" }, "global bridge requests retain the empty root contract");
  assert.equal(loadWorkspacePanelOpen(""), true, "global creation does not overwrite the legacy fallback for other projects");
  await paint(globalRoot);
  assert.equal(useLayoutStore.getState().workspacePanelOpen, false, "global destination restoration cannot reopen the new-session dock");
  await act(async () => commands.openRightDockMode("files"));
  await paint("A");
  await paint(globalRoot);
  assert.equal(useLayoutStore.getState().workspacePanelOpen, true, "manual global preference still restores on ordinary navigation");
  await act(async () => navigation.handleNewTab());
  assert.equal(loadWorkspacePanelOpen(globalRoot), false, "new-session toolbar uses the active global directory");
  assert.deepEqual(navigationRequest, { kind: "blank", scope: "global", workspaceRoot: "" });
  await paint("A");
  assert.equal(useLayoutStore.getState().workspacePanelOpen, true, "global creation preserves the source project's preference");
  await act(async () => root.unmount());
  const before = { closes, widthClears, layout: useLayoutStore.getState() };
  first.openRightDockMode("changed"); first.toggleWorkspaceMaximized(); first.closeWorkspacePanel();
  assert.deepEqual({ closes, widthClears, layout: useLayoutStore.getState() }, before, "disposed entries cannot change layout or project preferences");
  console.log("workspace commands: scoped restoration, Creation, preview/maximize, remote requests and synchronous disposal passed");
} finally { dom.window.close(); }
