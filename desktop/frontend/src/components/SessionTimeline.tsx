import { useMemo } from "react";
import { GitBranch } from "lucide-react";
import { compactQuestionText, questionAnchorId } from "../lib/transcriptGrouping";
import type { Item } from "../lib/useController";

type TimelineEntry =
  | { kind: "turn"; key: string; turn: number; checkpoint: boolean; text: string }
  | { kind: "compact"; key: string; trigger: string; messages: number };

export function SessionTimeline({ items }: { items: Item[] }) {
  const { entries, turnCount } = useMemo(() => {
    const entries: TimelineEntry[] = [];
    let turn = 0;
    for (const item of items) {
      if (item.kind === "user") {
        turn++;
        entries.push({ kind: "turn", key: item.id, turn, checkpoint: Boolean(item.checkpointTurn), text: item.text });
      } else if (item.kind === "compaction" && !item.pending) {
        entries.push({ kind: "compact", key: item.id, trigger: item.trigger, messages: item.messages });
      }
    }
    return { entries, turnCount: turn };
  }, [items]);

  if (turnCount < 2) return null;

  const scrollToTurn = (key: string) => {
    document.getElementById(questionAnchorId(key))?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  return (
    <div className="session-timeline" role="navigation" aria-label="Session timeline">
      <div className="session-timeline__label">
        <GitBranch size={12} />
        <span>Map</span>
      </div>
      <div className="session-timeline__rail">
        {entries.map((entry) =>
          entry.kind === "turn" ? (
            <button
              key={entry.key}
              type="button"
              className={`session-timeline__node${entry.turn === turnCount ? " session-timeline__node--current" : ""}${entry.checkpoint ? " session-timeline__node--checkpoint" : ""}`}
              title={compactQuestionText(entry.text)}
              aria-label={`Turn ${entry.turn}`}
              onClick={() => scrollToTurn(entry.key)}
            >
              {entry.turn}
            </button>
          ) : (
            <div
              key={entry.key}
              className="session-timeline__compact"
              title={`${entry.trigger} · ${entry.messages} messages compacted`}
            />
          ),
        )}
      </div>
    </div>
  );
}
