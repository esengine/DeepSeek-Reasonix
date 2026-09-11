import { useMemo } from "react";
import type { TranscriptRow } from "./transcriptRows";
import type { LiveStream } from "./useController";
import { transcriptLiveSelectableRows, transcriptSelectableRows } from "./transcriptSelectionText";

export function useTranscriptSelectableRows(rows: readonly TranscriptRow[], live?: LiveStream) {
  const stable = useMemo(() => transcriptSelectableRows(rows), [rows]);
  const byKey = useMemo(() => new Map(stable.map((row) => [row.rowKey, row])), [stable]);
  const overrides = useMemo(() => transcriptLiveSelectableRows(byKey, live), [byKey, live]);
  return [stable, overrides] as const;
}
