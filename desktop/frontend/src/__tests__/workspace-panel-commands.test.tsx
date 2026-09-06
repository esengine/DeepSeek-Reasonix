import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useWorkspacePanelCommands } from "../app-runtime/useWorkspacePanelCommands";
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
const setTreeWidth = (width: number) => { restoredWidth = width; };
function Probe({ workspace, creation, visible }: { workspace: string; creation: boolean; visible: boolean }) {
  commands = useWorkspacePanelCommands({ workspaceRoot: workspace, creation, visible, closeOverlays, clearLiveWidth,
    availableWidth: 800, clampTreeWidth: (width) => width, setTreeWidth });
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
  await act(async () => root.unmount());
  const before = { closes, widthClears, layout: useLayoutStore.getState() };
  first.openRightDockMode("changed"); first.toggleWorkspaceMaximized(); first.closeWorkspacePanel();
  assert.deepEqual({ closes, widthClears, layout: useLayoutStore.getState() }, before, "disposed entries cannot change layout or project preferences");
  console.log("workspace commands: scoped restoration, Creation, preview/maximize, remote requests and synchronous disposal passed");
} finally { dom.window.close(); }
