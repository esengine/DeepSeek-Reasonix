import type { ForkBindings, ForkWorktreeResultView } from "./forkWorktree";
import type { TabMeta } from "./types";

export function mockForkWorktree(tab: TabMeta): ForkWorktreeResultView {
  return { tab: { ...tab, workspaceRoot: `${tab.workspaceRoot}-worktree` }, isolated: true, branch: "reasonix/delivery-mock" };
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
