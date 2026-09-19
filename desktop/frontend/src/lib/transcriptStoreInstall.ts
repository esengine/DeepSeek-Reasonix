import { asArray } from "./array";
import { appendLivePageEntries } from "./transcriptLiveWindow";
import type { PreparedTranscriptInstall, SessionTranscript, TranscriptProjection } from "./transcriptStoreTypes";
import type { HistoryEntry, HistorySlice } from "./types";

/** Prepare a complete Follow replacement while preserving the resident object
 * that owns in-flight content handoffs. Nothing becomes visible until commit. */
export function prepareTranscriptInstall(previous: SessionTranscript | undefined, session: SessionTranscript,
  slice: HistorySlice, windowPageEntries: number,
  replaceRecords: (candidate: SessionTranscript, entries: HistoryEntry[]) => void,
  projectionOf: (candidate: SessionTranscript) => TranscriptProjection,
  install: (candidate: SessionTranscript) => void): PreparedTranscriptInstall {
  session.generation = (previous?.generation ?? 0) + 1;
  session.bindingKey = previous?.bindingKey;
  session.canonicalV2 = true;
  session.latestSequence = slice.revision;
  const entries = asArray<HistoryEntry>(slice.entries);
  replaceRecords(session, entries);
  appendLivePageEntries(session.pages, entries.map(entry => entry.entryId), windowPageEntries);
  if (session.pages.length) {
    session.pages[0].olderCursor = slice.nextCursor ?? "";
    session.pages[session.pages.length - 1].newerCursor = slice.newerCursor ?? "";
  }
  Object.assign(session, {
    nextCursor: slice.nextCursor ?? "", newerCursor: slice.newerCursor ?? "",
    hasOlder: Boolean(slice.hasOlder), hasNewer: Boolean(slice.hasNewer),
    totalTurns: slice.totalTurns ?? 0, startTurn: slice.startTurn ?? 0, endTurn: slice.endTurn ?? 0,
    revision: slice.revision ?? 0, revisionKnown: true, digest: slice.digest ?? "",
  });
  const projection = projectionOf(session);
  let committed = false;
  return { projection, commit: () => {
    if (committed) return;
    committed = true;
    if (!previous) return install(session);
    const retained = {
      pendingContent: previous.pendingContent, generationSettlement: previous.generationSettlement,
      olderInFlight: previous.olderInFlight, newerInFlight: previous.newerInFlight,
      reclaimedOlder: previous.reclaimedOlder, reclaimedNewer: previous.reclaimedNewer,
    };
    Object.assign(previous, session, retained);
    install(previous);
  } };
}
