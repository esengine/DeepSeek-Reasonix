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

export interface ForkBindings {
  ForkForTab(tabID: string, turn: number): Promise<TabMeta>;
  ForkWorktreeForTab(tabID: string, turn: number): Promise<ForkWorktreeResultView>;
}

interface MockForkBindings extends ForkBindings {
  Fork(turn: number): Promise<TabMeta>;
}

export function makeMockForkBindings(
  getTabs: () => TabMeta[],
  setTabs: (tabs: TabMeta[]) => void,
  defaultTitle: string,
): MockForkBindings {
  const fork = async (_turn: number): Promise<TabMeta> => {
    const tabs = getTabs();
    const active = tabs.find((tab) => tab.active) ?? tabs[0];
    const stamp = Date.now();
    const tab: TabMeta = {
      ...active,
      id: `tab_fork_${stamp}`,
      topicId: `topic_fork_${stamp}`,
      topicTitle: `${active.topicTitle || defaultTitle} · fork`,
      active: true,
      running: false,
    };
    setTabs([...tabs.map((item) => ({ ...item, active: false })), tab]);
    return { ...tab };
  };
  const forkForTab = async (tabID: string, turn: number): Promise<TabMeta> => {
    setTabs(getTabs().map((tab) => ({ ...tab, active: tab.id === tabID })));
    return fork(turn);
  };
  return {
    Fork: fork,
    ForkForTab: forkForTab,
    async ForkWorktreeForTab(tabID, turn) {
      return mockForkWorktree(await forkForTab(tabID, turn));
    },
  };
}

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

export async function settleForkConversationForTab(
  bindings: ForkBindings,
  sourceTabId: string,
  turn: number,
  isolated: boolean,
  notice: Notice,
  adopt: (tab: TabMeta) => Promise<unknown>,
  sync: () => Promise<unknown>,
): Promise<{ ok: boolean; tabId?: string; tab?: TabMeta }> {
  const tab = await forkConversationForTab(bindings, sourceTabId, turn, isolated, notice);
  if (!tab?.id) {
    await sync();
    return { ok: false };
  }
  await adopt(tab);
  return { ok: true, tabId: tab.id, tab };
}
