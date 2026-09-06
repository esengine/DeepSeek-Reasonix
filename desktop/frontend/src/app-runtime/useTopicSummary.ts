import { useEffect } from "react";

type TopicSummaryTarget = Readonly<{
  scope?: string | null;
  workspaceRoot?: string | null;
  topicId?: string | null;
}>;

type TopicSummary = Readonly<{ turns?: number }>;

export function useTopicSummary(input: {
  target: TopicSummaryTarget | null | undefined;
  revision: number;
  getSummary: (request: { scope: "global" | "project"; workspaceRoot: string; topicId: string }) => Promise<TopicSummary>;
  onTurns: (turns: number | undefined) => void;
}) {
  const { getSummary, onTurns, revision, target } = input;
  useEffect(() => {
    const currentTarget = target;
    const topicId = currentTarget?.topicId?.trim();
    if (!topicId) {
      onTurns(undefined);
      return;
    }
    let current = true;
    void getSummary({
      scope: currentTarget?.scope === "global" ? "global" : "project",
      workspaceRoot: currentTarget?.scope === "global" ? "" : currentTarget?.workspaceRoot ?? "",
      topicId,
    }).then((summary) => {
      if (current) onTurns(summary.turns);
    }).catch(() => {
      if (current) onTurns(undefined);
    });
    return () => { current = false; };
  }, [getSummary, onTurns, revision, target]);
}
