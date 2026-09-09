import { app } from "../lib/bridge";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { clearThemePack } from "../lib/themePack";
import { applyTheme, getTheme, getThemeStyle, isThemeStyle } from "../lib/theme";
import { decisionSurfaceMockFromInput } from "../lib/decisionSurfaceMock";
import { activeTabMirror } from "./activeTabMirror";
import type { SettingsTab } from "../lib/types";
import type { StructuredInvocationSubmit } from "../lib/invocationDisplay";
import type { Translator } from "../lib/i18n";

type MockWorkView = {
  running: true;
  pendingPrompt: false;
  cancellable: true;
  jobs: { id: string; kind: string; label: string; status: string; startedAt: number }[];
};

export type ComposerRouterInput = {
  activeTabId: string | undefined;
  goalDraftActive: boolean;
  t: Translator;
  notice(message: string, kind?: "info" | "warn" | "error"): void;
  showToast(message: string, level: "info" | "warn" | "error", options?: { durationMs?: number }): void;
  ports: {
    runShellForTab(tabId: string, cmd: string): Promise<void>;
    switchModel(name: string, tabId: string): Promise<unknown>;
    newSession(): Promise<unknown>;
    setSettingsTarget(tab: SettingsTab): void;
    setClearContextPending(pending: boolean): void;
    clearWorkspaceConflict(): void;
    setWorkspaceConflict(value: { state: "local"; ownerTabId: string; ownerTitle: string; ownerWork: MockWorkView; canReveal: true; canCreateWorktree: true } | null): void;
    setPendingClose(value: { tabId: string; work: MockWorkView; stopping: boolean } | null): void;
    submitComposerTurn(tabId: string, display: string, submit?: string, structured?: StructuredInvocationSubmit): Promise<void>;
    steerForTab(tabId: string, text: string): Promise<void>;
    isRemoteTab(tabId: string): boolean;
  };
};

function isThemeMode(value: string): value is "auto" | "light" | "dark" {
  return value === "auto" || value === "light" || value === "dark";
}

/**
 * Routes a composer submit to its desktop-native action: shell commands,
 * model/memory/clear/new commands, the browser decision-surface mock seeds,
 * Goal activation or ordinary submission, theme commands and remote steer.
 * Only the routes that need a desktop-native UI action are reserved here.
 */
export function useComposerRouter(input: ComposerRouterInput) {
  const { activeTabId, goalDraftActive, t, notice, showToast, ports } = input;

  const handleSend = useCommittedCommand(async (displayText: string, submitText = displayText, requestedTabId = activeTabId, structured?: StructuredInvocationSubmit) => {
    const sourceTabId = requestedTabId || activeTabId;
    if (!sourceTabId) throw new Error(t("composer.workspaceStarting"));
    const trimmed = displayText.trim();
    // "!<cmd>" runs a shell command directly, bypassing the model.
    if (trimmed.startsWith("!")) {
      const cmd = trimmed.slice(1).trim();
      if (!cmd) {
        notice("usage: !<command>  (e.g. !ls -la)");
        return;
      }
      await ports.runShellForTab(sourceTabId, cmd);
      return;
    }
    const model = /^\/model\s+(\S+)$/.exec(trimmed);
    if (model) {
      await ports.switchModel(model[1], sourceTabId);
      return;
    }
    if (trimmed === "/memory") {
      if (activeTabMirror().current !== sourceTabId) return;
      ports.setSettingsTarget("memory");
      return;
    }
    if (trimmed === "/clear") {
      if (activeTabMirror().current !== sourceTabId) return;
      ports.setClearContextPending(true);
      return;
    }
    if (trimmed === "/new") {
      if (activeTabMirror().current !== sourceTabId) return;
      await ports.newSession();
      return;
    }
    const decisionMock = typeof window !== "undefined" && !window.runtime
      ? decisionSurfaceMockFromInput(trimmed)
      : null;
    if (decisionMock === "workspace_conflict" || decisionMock === "mode_jobs" || decisionMock === "close_active" || decisionMock === "clear_context") {
      if (activeTabMirror().current !== sourceTabId) return;
      ports.clearWorkspaceConflict();
      ports.setPendingClose(null);
      ports.setClearContextPending(false);
      const mockWork: MockWorkView = {
        running: true,
        pendingPrompt: false,
        cancellable: true,
        jobs: [
          { id: "mock-decision-build", kind: "bash", label: "pnpm build", status: "running", startedAt: Date.now() - 42_000 },
          { id: "mock-decision-test", kind: "bash", label: "go test ./...", status: "running", startedAt: Date.now() - 18_000 },
        ],
      };
      if (decisionMock === "workspace_conflict") {
        ports.setWorkspaceConflict({
          state: "local",
          ownerTabId: "mock-workspace-writer",
          ownerTitle: t("mock.topicDevStandard"),
          ownerWork: mockWork,
          canReveal: true,
          canCreateWorktree: true,
        });
      } else if (decisionMock === "close_active") {
        ports.setPendingClose({ tabId: sourceTabId, work: mockWork, stopping: false });
      } else {
        ports.setClearContextPending(true);
      }
      return;
    }
    if (goalDraftActive) {
      await ports.submitComposerTurn(sourceTabId, displayText, submitText, structured);
      return;
    }
    const theme = /^\/theme(?:\s+(\S+))?$/.exec(trimmed);
    if (theme) {
      const arg = theme[1]?.toLowerCase();
      if (!arg) {
        const cur = getTheme();
        notice(t("settings.themeCurrent", { theme: cur, style: getThemeStyle(cur) }));
        return;
      }
      if (arg === "reset" || arg === "default" || arg === "clear") {
        try {
          await app.ResetThemePack();
          clearThemePack();
          notice(t("settings.themeReset"));
        } catch (err) {
          showToast(err instanceof Error ? err.message : String(err), "error");
        }
        return;
      }
      if (isThemeMode(arg)) {
        const next = arg;
        const style = getThemeStyle(next);
        try {
          await app.SetDesktopAppearance(next, style);
          applyTheme(next, style);
          notice(t("settings.themeChanged", { theme: next, style }));
        } catch (err) {
          showToast(err instanceof Error ? err.message : String(err), "error");
        }
        return;
      }
      if (isThemeStyle(arg)) {
        const cur = getTheme();
        try {
          await app.SetDesktopAppearance(cur, arg);
          applyTheme(cur, arg);
          notice(t("settings.themeChanged", { theme: cur, style: arg }));
        } catch (err) {
          showToast(err instanceof Error ? err.message : String(err), "error");
        }
        return;
      }
      notice(t("settings.themeUnknown", { name: arg }), "warn");
      return;
    }
    await ports.submitComposerTurn(sourceTabId, displayText, submitText, structured);
  });

  const handleSteer = useCommittedCommand(async (text: string, requestedTabId = activeTabId) => {
    const sourceTabId = requestedTabId || activeTabId;
    if (!sourceTabId) throw new Error(t("composer.workspaceStarting"));
    if (ports.isRemoteTab(sourceTabId)) {
      await app.SteerRemoteTab(sourceTabId, text.trim());
      return;
    }
    await ports.steerForTab(sourceTabId, text.trim());
  });

  return { handleSend, handleSteer };
}
