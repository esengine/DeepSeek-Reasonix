// The run graph: the shape a turn's fan-out actually had. Its own catalogue for
// the same reason the others have one — en.ts sits at the file-size ceiling, and
// a screen's worth of wording is what a translator reads at a time.
export const EN_GRAPH: Record<string, string> = {
  "graph.graph": "Graph",
  "gnode.notStarted": "Not started",
  "gnode.queued": "Queued",
  "gnode.reused": "Reused",
  "common.cancelled": "Cancelled",
  "common.skipped": "Skipped",
  "memoryDetail.state": "State",
  "prefsnav.grant": "Grant",
  "graph.writes": "Writes",
  "runAnalysis.took": "Took",
  "prefsnav.waitingOn": "Waiting on",
  "graph.transcript": "Transcript",
  "graph.error": "Error",
  "gnode.noFreeSlot": "No free slot",
  "gnode.concurrentWritersFull": "Concurrent writers full",
  "gnode.thePathItWritesWas": "The path it writes was held",
};
