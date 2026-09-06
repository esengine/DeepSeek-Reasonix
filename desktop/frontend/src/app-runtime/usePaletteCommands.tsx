import { useMemo, type RefObject } from "react";
import { AlarmClock, Activity, BarChart3, Brain, Cpu, Palette, Puzzle, RotateCw, Server, Settings as SettingsIcon, SquarePen, TerminalSquare, Trash2 } from "lucide-react";
import { app } from "../lib/bridge";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { useGlobalShortcut } from "../lib/keyboardShortcuts";
import { clearThemePack } from "../lib/themePack";
import { paletteSessionDisplayTitle, paletteSessionHint, paletteSessionKeywords, sessionActivityTime } from "../lib/session";
import { useOverlayStore } from "../store/overlays";
import { useAppNavigationStore } from "../store/appNavigation";
import { useRemoteStore } from "../store/remote";
import type { RemoteHostView, SessionMeta } from "../lib/types";
import type { Translator } from "../lib/i18n";
import type { PaletteItem } from "../components/CommandPalette";

export type PaletteCommandsInput = {
  managementActive: boolean;
  activeTabId: string | undefined;
  activeTabIdRef: RefObject<string | undefined>;
  remoteSurfaceActive: boolean;
  t: Translator;
  notice(message: string, kind?: "info" | "warn" | "error"): void;
  showToast(message: string, level: "info" | "warn" | "error", options?: { durationMs?: number }): void;
  ports: {
    handleNewTab(): void;
    listSessions(): Promise<SessionMeta[]>;
    openTrash(): void;
    onResumeSession(session: SessionMeta): Promise<void>;
    openRemoteWorkspaceFromStatus(host: RemoteHostView): void;
    connectAndOpenRemoteWorkspace(host: RemoteHostView): void;
    toggleTerminalPanel(): void;
    setTasksOpen(open: false | "session" | "all"): void;
    handleTabClose(id: string): void;
    toggleSidebar(): void;
    returnToWorkspace(): void;
  };
};

/**
 * Owns the command palette: its open action (snapshotting sessions and
 * extension actions), its items and the global command shortcuts that open it
 * or the new-session/settings/tab-close/shortcuts/sidebar surfaces. Session,
 * extension, remote-host and navigation targets come from their stores.
 */
export function usePaletteCommands(input: PaletteCommandsInput) {
  const { managementActive, activeTabId, activeTabIdRef, remoteSurfaceActive, t, notice, showToast, ports } = input;
  const setPaletteOpen = useOverlayStore((state) => state.setPaletteOpen);
  const paletteSessions = useOverlayStore((state) => state.paletteSessions);
  const setPaletteSessions = useOverlayStore((state) => state.setPaletteSessions);
  const paletteExtensionActions = useOverlayStore((state) => state.paletteExtensionActions);
  const setPaletteExtensionActions = useOverlayStore((state) => state.setPaletteExtensionActions);
  const setShortcutsOpen = useOverlayStore((state) => state.setShortcutsOpen);
  const setTransientOverlayDismissSignal = useOverlayStore((state) => state.setTransientOverlayDismissSignal);
  const remoteHosts = useRemoteStore((state) => state.hosts);
  const remoteStatuses = useRemoteStore((state) => state.statuses);

  const closeTransientOverlays = useCommittedCommand(() => {
    setTransientOverlayDismissSignal((signal) => signal + 1);
  });

  const openPalette = useCommittedCommand(async () => {
    closeTransientOverlays();
    setPaletteOpen(true);
    setPaletteSessions(await ports.listSessions().catch(() => []));
    setPaletteExtensionActions(await app.ExtensionActions(activeTabIdRef.current ?? "").catch(() => []));
  });

  useGlobalShortcut("commandPalette.open", () => {
    setPaletteOpen((current) => {
      if (!current) void openPalette();
      return !current; // toggle the state so the palette actually opens/closes
    });
  }, [openPalette]);
  useGlobalShortcut("app.newSession", () => void ports.handleNewTab(), [ports.handleNewTab]);
  useGlobalShortcut("settings.open", () => {
    closeTransientOverlays();
    useAppNavigationStore.getState().setSettingsTarget(useAppNavigationStore.getState().lastSettingsTarget);
  }, [closeTransientOverlays]);
  useGlobalShortcut("tab.close", () => {
    if (managementActive) ports.returnToWorkspace();
    else if (activeTabId) void ports.handleTabClose(activeTabId);
  }, [activeTabId, managementActive, ports.handleTabClose, ports.returnToWorkspace], managementActive || Boolean(activeTabId));
  useGlobalShortcut("shortcuts.show", () => setShortcutsOpen(true));
  useGlobalShortcut("sidebar.toggle", ports.toggleSidebar, [ports.toggleSidebar], !managementActive);

  const paletteItems = useMemo<PaletteItem[]>(() => {
    const navigation = useAppNavigationStore.getState();
    const cmds: PaletteItem[] = [
      { id: "cmd-new", group: t("palette.group.commands"), title: t("palette.cmd.newSession"), icon: <SquarePen size={15} />, compact: true, keywords: ["new", "新建"], run: () => void ports.handleNewTab() },
      { id: "cmd-automation", group: t("palette.group.commands"), title: t("sidebar.automation"), icon: <AlarmClock size={15} />, compact: true, keywords: ["automation", "自动化"], run: () => navigation.openPage({ kind: "automation" }) },
      { id: "cmd-trash", group: t("palette.group.commands"), title: t("palette.cmd.trash"), icon: <Trash2 size={15} />, compact: true, keywords: ["trash", "回收站"], run: () => void ports.openTrash() },
      { id: "cmd-settings", group: t("palette.group.commands"), title: t("palette.cmd.settings"), icon: <SettingsIcon size={15} />, compact: true, keywords: ["settings", "设置"], run: () => navigation.setSettingsTarget(navigation.lastSettingsTarget) },
      { id: "cmd-appearance", group: t("palette.group.commands"), title: t("palette.cmd.appearance"), icon: <Palette size={15} />, compact: true, keywords: ["theme", "appearance", "外观", "主题"], run: () => navigation.setSettingsTarget("appearance") },
      {
        id: "cmd-theme-reset",
        group: t("palette.group.commands"),
        title: t("settings.themeLibrary.reset"),
        icon: <Palette size={15} />,
        compact: true,
        keywords: ["theme", "reset", "default", "恢复默认", "主题"],
        run: () => {
          void app.ResetThemePack()
            .then(() => {
              clearThemePack();
              notice(t("settings.themeReset"));
            })
            .catch((err) => showToast(err instanceof Error ? err.message : String(err), "error"));
        },
      },
      { id: "cmd-memory", group: t("palette.group.commands"), title: t("palette.cmd.memory"), icon: <Brain size={15} />, compact: true, keywords: ["memory", "记忆"], run: () => navigation.setSettingsTarget("memory") },
      { id: "cmd-models", group: t("palette.group.commands"), title: t("palette.cmd.models"), icon: <Cpu size={15} />, compact: true, keywords: ["model", "模型"], run: () => navigation.setSettingsTarget("models") },
      {
        id: "cmd-usage-stats",
        group: t("palette.group.commands"),
        title: t("palette.cmd.usageStats"),
        icon: <BarChart3 size={15} />,
        compact: true,
        keywords: ["usage", "stats", "statistics", "用量", "统计"],
        run: () => {
          navigation.setSettingsFocus((current) => ({
            target: "model-stats",
            requestId: (current?.requestId ?? 0) + 1,
          }));
          navigation.setSettingsTarget("models");
        },
      },
      { id: "cmd-task-center", group: t("palette.group.commands"), title: t("palette.cmd.taskCenter"), icon: <Activity size={15} />, compact: true, keywords: ["task", "tasks", "center", "任务", "任务中心"], run: () => ports.setTasksOpen("all") },
      { id: "cmd-terminal", group: t("palette.group.commands"), title: t("rightDock.terminal"), icon: <TerminalSquare size={15} />, compact: true, keywords: ["terminal", "shell", "终端"], run: () => ports.toggleTerminalPanel() },
      {
        id: "cmd-reload-runtime",
        group: t("palette.group.commands"),
        title: t("palette.cmd.reloadRuntime"),
        icon: <RotateCw size={15} />,
        compact: true,
        keywords: ["reload", "runtime", "重载", "运行时"],
        run: () => {
          const tabID = activeTabId;
          if (!tabID) return;
          // Success/queued feedback arrives as a tab notice; only hard failures need a toast.
          void app.ReloadRuntime(tabID).catch((err) => showToast(err instanceof Error ? err.message : String(err), "error"));
        },
      },
    ];
    const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
    const dayLabel = (ms: number) => {
      const days = Math.round((startOfDay(new Date()) - startOfDay(new Date(ms))) / 86_400_000);
      if (days <= 0) return t("history.today");
      if (days === 1) return t("history.yesterday");
      return new Date(ms).toLocaleDateString();
    };
    const sessionItems: PaletteItem[] = paletteSessions.slice(0, 12).map((s) => ({
      id: `sess-${s.path}`,
      group: t("palette.group.sessions"),
      title: paletteSessionDisplayTitle(s, t("history.emptySession")),
      hint: paletteSessionHint(s),
      keywords: paletteSessionKeywords(s),
      meta: dayLabel(sessionActivityTime(s)),
      badge: t(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns }),
      run: () => void ports.onResumeSession(s),
    }));
    const remoteItems: PaletteItem[] = remoteHosts.map((host) => {
      const status = remoteStatuses[host.id];
      const connected = status?.state === "connected" || status?.state === "degraded";
      const target = `${host.user ? `${host.user}@` : ""}${host.host}${host.port && host.port !== 22 ? `:${host.port}` : ""}`;
      return {
        id: `remote-${host.id}`,
        group: t("palette.group.remote"),
        title: connected
          ? t("palette.remote.open", { host: host.label })
          : t("palette.remote.connect", { host: host.label }),
        hint: host.defaultWorkspace || target,
        icon: <Server size={15} />,
        keywords: ["ssh", "remote", "远程", "连接", host.label, host.host],
        run: () => {
          if (connected) ports.openRemoteWorkspaceFromStatus(host);
          else ports.connectAndOpenRemoteWorkspace(host);
        },
      };
    });
    const extensionItems: PaletteItem[] = paletteExtensionActions.map((action) => ({
      id: `ext-${action.slash}`,
      group: t("palette.group.extensions"),
      title: action.description || action.slash,
      hint: action.slash,
      icon: <Puzzle size={15} />,
      keywords: ["extension", "扩展", action.plugin, action.action, action.slash],
      run: () => {
        const tabID = activeTabId;
        if (!tabID) return;
        // The extension's result message is user-facing feedback; only hard
        // failures need an error toast.
        void app.InvokeExtensionAction(tabID, action.slash, {})
          .then((message) => {
            if (message) showToast(message, "info");
          })
          .catch((err) => showToast(err instanceof Error ? err.message : String(err), "error"));
      },
    }));
    return [...(remoteSurfaceActive ? cmds.filter((item) => item.id !== "cmd-terminal" && item.id !== "cmd-reload-runtime") : cmds), ...extensionItems, ...remoteItems, ...sessionItems];
  }, [t, paletteSessions, paletteExtensionActions, remoteHosts, remoteStatuses, activeTabId, remoteSurfaceActive, ports, showToast, notice]);

  return { openPalette, paletteItems };
}
