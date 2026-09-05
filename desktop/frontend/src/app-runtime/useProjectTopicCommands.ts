import { useLayoutEffect, useRef, useState } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { useResourceOperations, type SessionResource } from "./useResourceOperations";
import { refreshProjectTopics, renameProjectTopic, type ProjectTopicPorts, type TopicRenameTarget } from "./projectTopicOwner";

type Input = {
  visible: SessionResource;
  topic?: { id: string; title: string; target: TopicRenameTarget };
  ports: ProjectTopicPorts;
  navigation: {
    openBlank: (scope: string, workspace: string) => Promise<void>;
    enqueue: (request: { kind: "isolated-worktree"; workspaceRoot: string }) => Promise<void>;
    switchFolder: (path?: string) => Promise<unknown>;
  };
  reportError: (error: unknown) => void;
};
type RenameDraft = { id: string; target: TopicRenameTarget; title: string };

/** Project commands retain only committed ports and one explicitly targeted draft. */
export function useProjectTopicCommands(input: Input) {
  const operations = useResourceOperations({ visible: { tabId: input.visible.tabId || "application", sessionKey: input.visible.sessionKey } });
  const [draft, setDraft] = useState<RenameDraft | null>(null);
  const handled = useRef(false);
  const activeIdentity = input.topic ? JSON.stringify([input.topic.id, input.topic.target]) : "";
  const draftIdentity = draft ? JSON.stringify([draft.id, draft.target]) : "";
  useLayoutEffect(() => {
    if (draftIdentity && draftIdentity !== activeIdentity) { handled.current = true; setDraft(null); }
  }, [activeIdentity, draftIdentity]);
  const report = useCommittedCommand(input.reportError);
  const rename = useCommittedCommand(async (target: TopicRenameTarget, title: string) => {
    if (!title.trim()) return;
    const outcome = await operations({ kind: "workspace", workspaceKey: JSON.stringify(target) }, "topic-rename",
      { target, title: title.trim(), activeTabId: input.visible.tabId, ports: input.ports }, renameProjectTopic);
    if (outcome.status === "failed") report(outcome.error);
  });
  const renameTopic = useCommittedCommand((topicId: string, title: string) => topicId ? rename({ kind: "local", topicId }, title) : Promise.resolve());
  const refreshProjectsAndTabs = useCommittedCommand(async () => {
    const outcome = await operations({ kind: "application" }, "project-refresh", { activeTabId: input.visible.tabId, ports: input.ports }, refreshProjectTopics);
    if (outcome.status === "failed") report(outcome.error);
  });
  const startActiveTopicRename = useCommittedCommand(() => {
    if (!input.topic) return;
    handled.current = false;
    setDraft({ ...input.topic });
  });
  const cancelActiveTopicRename = useCommittedCommand(() => { handled.current = true; setDraft(null); });
  const commitActiveTopicRename = useCommittedCommand(async () => {
    if (!draft || handled.current) return;
    handled.current = true;
    setDraft(null);
    await rename(draft.target, draft.title);
  });
  const setTopicTitleDraft = useCommittedCommand((title: string) => setDraft(current => current ? { ...current, title } : current));
  const onCreateTopic = useCommittedCommand((scope: string, workspace: string) => input.navigation.openBlank(scope, scope === "project" ? workspace : ""));
  const onCreateIsolatedWorktree = useCommittedCommand((workspaceRoot: string) => input.navigation.enqueue({ kind: "isolated-worktree", workspaceRoot }));
  const onAddProject = useCommittedCommand(async (path?: string) => { await input.navigation.switchFolder(path); });
  return {
    topicTitleDraft: draft?.title ?? "", topicbarEditing: Boolean(draft && draftIdentity === activeIdentity),
    setTopicTitleDraft, startActiveTopicRename, cancelActiveTopicRename, commitActiveTopicRename,
    renameTopic, refreshProjectsAndTabs, onCreateTopic, onCreateIsolatedWorktree, onAddProject,
  };
}
