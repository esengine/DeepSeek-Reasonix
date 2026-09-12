import type { ForkBindings, ForkWorktreeResultView } from "./forkWorktree";
import type { TabMeta } from "./types";

export function mockForkWorktree(tab: TabMeta): ForkWorktreeResultView {
  return { tab: { ...tab, workspaceRoot: `${tab.workspaceRoot}-worktree` }, isolated: true, branch: "reasonix/delivery-mock" };
}

export function increaseMockForkTitle(title: string): string {
  const base = title.trim();
  const ascii = /^(.*) \(([0-9]+)\)$/.exec(base);
  if (ascii) return `${ascii[1]} (${BigInt(ascii[2]) + 1n})`;
  const fullwidth = /^(.*)（([0-9]+)）$/.exec(base);
  if (fullwidth) return `${fullwidth[1]}（${BigInt(fullwidth[2]) + 1n}）`;
  return `${base} (1)`;
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
      topicTitle: increaseMockForkTitle(active.topicTitle || defaultTitle),
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
