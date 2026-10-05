// The trajectory pane: what ran, when, and how much of that the record covers.
// Its own catalogue for the reason en_graph has one — en.ts sits at the
// file-size ceiling, and a screen's worth of wording is read together.
export const EN_TRAJECTORY: Record<string, string> = {
  "运行分析": "Run analysis",
  "本轮 · 基于真实轨迹": "This turn · from recorded events",
  "{n} 条记录": "{n} records",
  "导出轨迹": "Export trajectory",
  "已导出": "Exported",
  "运行摘要": "Run summary",
  "总时长": "Total time",
  "模型 P95": "Model P95",
  "{n} 个回合": "{n} rounds",
  "工具耗时": "Tool time",
  "并行节省": "Parallel saving",
  "累计活动减墙钟": "activity time minus wall time",
  "需要关注": "needs attention",
  "无异常": "no anomaly",
  "首项活动": "First activity",
  "相对本轮开始": "from turn start",
  "时间分布": "Time distribution",
  "累计活动 {time}": "{time} cumulative activity",
  "各类活动耗时占比": "Share of activity time",
  "系统": "System",
  "信号轨": "Signal rail",
  "与下方共用时间轴": "Shares the timeline below",
  "活动回合": "Activity rounds",
  "点击一行查看记录": "Select a row to inspect it",
  "模型回合": "Model round",
  "类型": "Type",
  "还没有可分析的运行": "There is no run to analyse yet",
  "发送任务后，这里会按真实轨迹显示模型、工具、重试与并行耗时。":
    "After you send a task, real events will show model, tool, retry and parallel time here.",
  "时间轴": "Timeline",
  "已保存至 {path}": "Saved to {path}",
  "已下载 {name}": "Downloaded {name}",
  // What the rows cover. The three the host distinguishes plus the one the page
  // is in until it has been told: absence is not one of them.
  "记录已达容量上限而停止 —— 最后一行之后的内容未被保留":
    "The record stopped at its size limit — whatever happened after the last row was not kept",
  "本次运行未记录轨迹 —— 以下仅为本次连接观察到的实时事件":
    "Nothing recorded this run — what follows is only the live events this connection saw",
  "完整记录 · 重新进入会话将原样重建": "The whole record · reopening the session rebuilds it as it is",
  "尚未读取该轨迹的覆盖范围": "How much this trajectory covers has not been read yet",
};
