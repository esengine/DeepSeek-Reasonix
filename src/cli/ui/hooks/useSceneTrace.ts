import { useStdout } from "ink";
import { useEffect } from "react";
import { box, frame, text } from "../scene/build.js";
import { emitSceneFrame, isSceneTraceEnabled } from "../scene/trace.js";
import type { TextRun } from "../scene/types.js";

export type SceneTraceSummary = {
  cardCount: number;
  busy: boolean;
  activity?: string;
};

export function useSceneTrace(summary: SceneTraceSummary): void {
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const rows = stdout?.rows ?? 24;
  useEffect(() => {
    if (!isSceneTraceEnabled()) return;
    const runs: TextRun[] = [
      { text: "reasonix · " },
      { text: `${summary.cardCount} cards`, style: { dim: true } },
      { text: summary.busy ? " · busy" : " · idle" },
    ];
    if (summary.activity) {
      runs.push({ text: ` · ${summary.activity}`, style: { dim: true } });
    }
    emitSceneFrame(frame(cols, rows, box([text(runs)], { paddingX: 1 })));
  }, [cols, rows, summary.cardCount, summary.busy, summary.activity]);
}
