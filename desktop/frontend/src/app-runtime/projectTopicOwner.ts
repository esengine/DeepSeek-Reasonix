import { CommandCancelled } from "../lib/commandOutcome";
import type { RemoteSessionView } from "../lib/remoteTypes";
import type { SessionOperationAuthority } from "./useResourceOperations";

export type TopicRenameTarget =
  | { kind: "local"; topicId: string }
  | { kind: "remote"; hostId: string; workspace: string; sessionPath: string };
export type ProjectTopicPorts = {
  renameLocal: (id: string, title: string) => Promise<void>;
  listRemote: (host: string, workspace: string) => Promise<RemoteSessionView[]>;
  renameRemote: (host: string, workspace: string, name: string, title: string) => Promise<void>;
  markChanged: (update: (value: number) => number) => void;
  refreshTabs: (apply?: () => boolean, options?: { afterMutation?: boolean }) => Promise<readonly { id: string }[]>;
  syncActive: (rebuild: boolean) => Promise<unknown>;
};
export type ProjectRefreshInput = { activeTabId?: string; ports: ProjectTopicPorts };

export async function refreshProjectTopics(input: ProjectRefreshInput, authority: SessionOperationAuthority) {
  authority.checkpoint();
  input.ports.markChanged(value => value + 1);
  const tabs = await input.ports.refreshTabs(() => {
    try { authority.checkpoint(); return true; } catch { return false; }
  }, { afterMutation: true });
  authority.checkpoint();
  if (authority.ownsUI() && input.activeTabId && !tabs.some(tab => tab.id === input.activeTabId)) await input.ports.syncActive(false);
}

export async function renameProjectTopic(input: ProjectRefreshInput & { target: TopicRenameTarget; title: string }, authority: SessionOperationAuthority) {
  const { target, title, ports } = input;
  authority.checkpoint();
  if (target.kind === "local") await ports.renameLocal(target.topicId, title);
  else {
    const sessions = await ports.listRemote(target.hostId, target.workspace);
    authority.checkpoint();
    // `current` is a navigation snapshot, not the identity of the rename target.
    const source = sessions.find(session => target.sessionPath
      ? session.path === target.sessionPath
      : !session.path && !session.name);
    if (!source) throw new CommandCancelled("superseded");
    await ports.renameRemote(target.hostId, target.workspace, source.name, title);
  }
  authority.checkpoint();
  await refreshProjectTopics(input, authority);
}
