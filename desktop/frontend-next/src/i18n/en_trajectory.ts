// The trajectory pane: what ran, when, and how much of that the record covers.
// Its own catalogue for the reason en_graph has one — en.ts sits at the
// file-size ceiling, and a screen's worth of wording is read together.
export const EN_TRAJECTORY: Record<string, string> = {
  "common.runAnalysis": "Run analysis",
  "runAnalysis.thisTurnFromRecordedEvents": "This turn · from recorded events",
  "runAnalysis.records": "{n} records",
  "runAnalysis.exportRunRecord": "Export run record",
  "runAnalysis.exported": "Exported",
  "runAnalysis.runSummary": "Run summary",
  "runAnalysis.totalTime": "Total time",
  "runAnalysis.modelP95": "Model P95",
  "runAnalysis.rounds": "{n} rounds",
  "runAnalysis.toolTime": "Tool time",
  "runAnalysis.parallelSaving": "Parallel saving",
  "runAnalysis.activityTimeMinusWallTime": "activity time minus wall time",
  "runAnalysis.needsAttention": "needs attention",
  "runAnalysis.noAnomaly": "no anomaly",
  "runAnalysis.firstActivity": "First activity",
  "runAnalysis.fromTurnStart": "from turn start",
  "runAnalysis.timeDistribution": "Time distribution",
  "runAnalysis.cumulativeActivity": "{time} cumulative activity",
  "runAnalysis.shareOfActivityTime": "Share of activity time",
  "common.system": "System",
  "runAnalysis.signalRail": "Signal rail",
  "runAnalysis.sharesTheTimelineBelow": "Shares the timeline below",
  "runAnalysis.timelineZoom": "Timeline zoom",
  "runAnalysis.resetZoom": "Reset zoom",
  "runAnalysis.recordsWithNoDurationThat":
    "Records with no duration that are not tool calls. Their position is their time, so they cannot be dragged. Zoom and scroll horizontally; signals too close together share one marker with a count. Select one to inspect its record.",
  "runAnalysis.signals": "{n} signals",
  "runAnalysis.activityRounds": "Activity rounds",
  "runAnalysis.selectARowToInspect": "Select a row to inspect it",
  "runAnalysis.modelRound": "Model round",
  "common.type": "Type",
  "runAnalysis.thereIsNoRunTo": "There is no run to analyse yet",
  "runAnalysis.afterYouSendATask":
    "After you send a task, real events will show model, tool, retry and parallel time here.",
  "runAnalysis.timeline": "Timeline",
  // What the rows cover. The three the host distinguishes plus the one the page
  // is in until it has been told: absence is not one of them.
};
