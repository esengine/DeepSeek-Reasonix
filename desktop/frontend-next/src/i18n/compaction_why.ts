// Why a fold installed nothing, keyed by the kernel's code. A code with no entry
// shows only that nothing folded, never a guess at the cause.
export const FOLD_WHY: Record<string, string> = {
  summary_failed: "生成摘要的请求失败了",
  summary_timeout: "生成摘要的请求超时了",
  summary_truncated: "摘要在输出上限处被截断，没有采用",
  summary_input_too_large: "待折叠的内容缩减后仍超过单次摘要请求的容量",
  hook_refused: "扩展拒绝了这次折叠",
  persist_failed: "折叠结果没能保存",
  context_changed: "摘要生成期间对话发生了变化，已放弃这次折叠",
  digest_lost_every_change: "摘要没有记下这段内容里的任何一处改动，已放弃这次折叠",
  candidate_not_smaller: "折叠后的上下文不比原来小，没有采用",
  candidate_above_ceiling: "折叠后仍超过检查点上限（受保护的内容太多），没有采用",
  candidate_above_trigger: "折叠后仍不低于压缩阈值，没有采用",
  candidate_above_physical_ceiling: "折叠后仍超过窗口的物理上限，没有采用",
  savings_below_minimum: "固定前缀已占满检查点上限，这次折叠省下的空间太少，没有采用",
  fixed_prefix_above_trigger: "无法折叠的固定部分本身已超过压缩阈值",
  fold_empty_after_hooks: "扩展钩子把待折叠的内容清空了",
};
