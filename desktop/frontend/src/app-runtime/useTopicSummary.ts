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
  useEffect(() => {
    const target = input.target;
    const topicId = target?.topicId?.trim();
    if (!topicId) {
      input.onTurns(undefined);
      return;
    }
    let current = true;
    void input.getSummary({
      scope: target?.scope === "global" ? "global" : "project",
      workspaceRoot: target?.scope === "global" ? "" : target?.workspaceRoot ?? "",
      topicId,
    }).then((summary) => {
      if (current) input.onTurns(summary.turns);
    }).catch(() => {
      if (current) input.onTurns(undefined);
    });
    return () => { current = false; };
  }, [input.getSummary, input.onTurns, input.revision, input.target]);
}
