// The metrics rail: what a turn is costing, what filled the window, and what it
// touched outside the transcript. Split out for the same reason en_settings and
// en_kernel were — one screen's worth of wording, read together.
export const EN_METRICS: Record<string, string> = {
  "前缀缓存": "Prefix cache",
  "工作树改动": "Worktree changes",
  "已放行": "auto-approved",
  "待审": "for review",
  "前缀变了": "prefix changed",
  "正文变了": "body changed",
  "前缀哈希": "Prefix hash",
  "工具 schema": "Tool schema",
  "该来源未声明窗口大小，因此无法显示已用比例，也不会自动压缩。中转服务转发的是第三方模型，其容量只有你知道。":
    "Nobody has said how large this source's window is, so there is no share to draw — and no automatic compaction either. A relay forwards somebody else's model, so only you know what it holds.",
  "该窗口值的来源无法确定 —— 点击可改为该模型的实际上限":
    "Whoever entered this window may not have meant this model — click to set what it actually holds",
  "只改当前这个模型，同一个来源下的其它模型不动。填模型文档写的上下文上限，不是最大输出。":
    "Applies to this model alone; the others on this source are left as they are. The context ceiling from the model's docs, not its max output.",
  "填写模型文档中的上下文上限，而非最大输出长度。":
    "The context ceiling from the model's docs, not its max output.",
  "{n} 会话": "{n} sessions",
  "改了 {n} 次": "edited {n} times",
  "改过": "Modified",
};
