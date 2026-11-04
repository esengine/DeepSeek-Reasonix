// The run graph: the shape a turn's fan-out actually had. Its own catalogue for
// the same reason the others have one — en.ts sits at the file-size ceiling, and
// a screen's worth of wording is what a translator reads at a time.
export const EN_GRAPH: Record<string, string> = {
  "已取消": "Cancelled",
  "跳过": "Skipped",
  "状态": "State",
  "权限": "Grant",
  "等待": "Waiting on",
};
