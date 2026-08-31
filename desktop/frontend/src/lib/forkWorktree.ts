import type { AppBindings } from "./bridge";
import { t } from "./i18n";
import type { TabMeta } from "./types";

export interface ForkWorktreeResultView {
  tab: TabMeta;
  isolated: boolean;
  fallbackToShared?: boolean;
  sourceDirty?: boolean;
  branch?: string;
}

export function mockForkWorktree(tab: TabMeta): ForkWorktreeResultView {
  return { tab: { ...tab, workspaceRoot: `${tab.workspaceRoot}-worktree` }, isolated: true, branch: "reasonix/delivery-mock" };
}

type ForkBindings = Pick<AppBindings, "ForkForTab" | "ForkWorktreeForTab">;
type Notice = (tabId: string, level: "info" | "warn", text: string) => void;

export async function forkConversationForTab(bindings: ForkBindings, sourceTabId: string, turn: number, isolated: boolean, notice: Notice): Promise<TabMeta | undefined> {
  if (!isolated) return bindings.ForkForTab(sourceTabId, turn);
  const result = await bindings.ForkWorktreeForTab(sourceTabId, turn);
  if (result.sourceDirty) {
    notice(sourceTabId, "warn", t("rewind.forkWorktreeDirtySource"));
    return undefined;
  }
  if (result.fallbackToShared && result.tab?.id) {
    notice(result.tab.id, "info", t("rewind.forkWorktreeFallbackNotice"));
  }
  return result.tab;
}
