// The run graph: the shape a turn's fan-out actually had. Its own catalogue for
// the same reason the others have one — en.ts sits at the file-size ceiling, and
// a screen's worth of wording is what a translator reads at a time.
export const EN_GRAPH: Record<string, string> = {
  "待运行": "Not started",
  "排队中": "Queued",
  "复用": "Reused",
  "已取消": "Cancelled",
  "跳过": "Skipped",
  "状态": "State",
  "权限": "Grant",
  "耗时": "Took",
  "等待": "Waiting on",
  "没有空位": "No free slot",
  "同时写的名额满了": "Concurrent writers full",
  "要写的路径被占着": "The path it writes was held",
};
