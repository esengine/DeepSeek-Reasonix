import { useEffect, useMemo, useState } from "react";

import { useCommittedCommand } from "../lib/useCommittedCommand";
import { desktopBridge } from "./desktopBridgeAdapter";
import type { TabMeta } from "../lib/types";

type TopicSummary = Readonly<{ turns?: number }>;

/**
 * Owns the topic summary chain: the target memo keyed by topic identity, the
 * GetTopicSummary bridge command, the single-flight fetch (an identity or
 * revision change cancels the superseded request) and the resulting
 * activeTopicTurns state. Presentation reads only the returned turns.
 */
export function useTopicSummary(input: {
  activeTab: TabMeta | undefined;
  revision: number;
}): { activeTopicTurns: number | undefined } {
  const { activeTab, revision } = input;
  const [activeTopicTurns, setActiveTopicTurns] = useState<number | undefined>(undefined);

  const scope = activeTab?.scope;
  const workspaceRoot = activeTab?.workspaceRoot;
  const topicId = activeTab?.topicId;
  const target = useMemo(() => (topicId === undefined ? null : { scope, workspaceRoot, topicId }),
    [scope, workspaceRoot, topicId]);

  const getSummary = useCommittedCommand((request: { scope: "global" | "project"; workspaceRoot: string; topicId: string }) => desktopBridge.getTopicSummary(request));
  const commitTurns = useCommittedCommand((turns: number | undefined) => setActiveTopicTurns(turns));

  useEffect(() => {
    const currentTarget = target;
    const topicId = currentTarget?.topicId?.trim();
    if (!topicId) {
      commitTurns(undefined);
      return;
    }
    let current = true;
    void getSummary({
      scope: currentTarget?.scope === "global" ? "global" : "project",
      workspaceRoot: currentTarget?.scope === "global" ? "" : currentTarget?.workspaceRoot ?? "",
      topicId,
    }).then((summary: TopicSummary) => {
      if (current) commitTurns(summary.turns);
    }).catch(() => {
      if (current) commitTurns(undefined);
    });
    return () => { current = false; };
  }, [getSummary, commitTurns, revision, target]);

  return { activeTopicTurns };
}
