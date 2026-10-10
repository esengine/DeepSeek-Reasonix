// Why a fold installed nothing, keyed by the kernel's code. A code with no entry
// shows only that nothing folded, never a guess at the cause.
export const FOLD_WHY: Record<string, string> = {
  summary_failed: "生成摘要的请求失败了",
  summary_timeout: "生成摘要的请求停滞了：连续 6 分钟没有任何输出",
  summary_ceiling: "生成摘要的请求运行满 30 分钟仍未完成，已停止",
  summary_truncated: "摘要在输出上限处被截断，没有采用",
  summary_not_digest: "摘要模型没有按要求的标题给出摘要，没有采用",
  summary_input_too_large: "待折叠的内容缩减后仍超过单次摘要请求的容量",
  hook_refused: "扩展拒绝了这次折叠",
  persist_failed: "折叠结果没能保存",
  context_changed: "摘要生成期间对话发生了变化，已放弃这次折叠",
  digest_lost_every_change: "摘要没有记下这段内容里的任何一处改动，已放弃这次折叠",
  candidate_not_smaller: "折叠后的上下文不比原来小，没有采用",
  candidate_above_ceiling: "折叠后仍超过检查点上限（受保护的内容太多），没有采用",
  candidate_above_trigger: "折叠后仍不低于压缩阈值，没有采用",
  result_above_trigger: "折叠后的上下文仍不低于压缩阈值，已暂停自动重试",
  candidate_above_physical_ceiling: "折叠后仍超过窗口的物理上限，没有采用",
  savings_below_minimum: "固定前缀已占满检查点上限，这次折叠省下的空间太少，没有采用",
  fixed_prefix_above_trigger: "无法折叠的固定部分本身已超过压缩阈值",
  fold_empty_after_hooks: "扩展钩子把待折叠的内容清空了",
  cancelled: "压缩被取消了",
  unclassified: "压缩失败，原因未归类；详情见日志",
  busy: "有一轮对话或会话切换正在进行，请稍后再试",
  input_unchanged: "上下文自上次整理后没有变化",
  no_new_closed_prefix: "自上个检查点以来没有已结束的内容",
  fold_below_economics: "新增内容太少，不值得再生成一次摘要",
  active_turn_boundary: "正在进行的这一轮需要先结束",
  no_foldable_region: "没有可折叠的内容",
};

// The reason a /compact notice carries as its detail; an empty detail is the
// kernel's decline with no class.
export const NO_CODE_WHY = "没有值得折叠的内容";
