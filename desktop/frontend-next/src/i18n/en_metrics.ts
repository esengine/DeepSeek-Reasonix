// The metrics rail: what a turn is costing, what filled the window, and what it
// touched outside the transcript. Split out for the same reason en_settings and
// en_kernel were — one screen's worth of wording, read together.
export const EN_METRICS: Record<string, string> = {
  "{n} 个运行中": "{n} running",
  "前缀缓存": "Prefix cache",
  "工作树改动": "Worktree changes",
  "没有在跑的后台任务": "No background jobs running",
  "已放行": "auto-approved",
  "待审": "for review",
  "前缀变了": "prefix changed",
  "前缀未变": "prefix held",
  "正文变了": "body changed",
  "正文未变": "body held",
  "沿用的消息条数": "Messages carried over",
  "前缀哈希": "Prefix hash",
  "按兜底价估算": "fallback price",
  "已结算": "settled",
  "本回合": "this turn ",
  "原币种": "as quoted",
  "两种结算币种不予合并 —— 合成单一总额需要虚构一个汇率。": "Two settlement currencies do not add up — one total would mean inventing a rate.",
  "工具 schema": "Tool schema",
  "远程主机": "Remote hosts",
  "还没有请求": "no requests yet",
  "该来源未声明窗口大小，因此无法显示已用比例，也不会自动压缩。中转服务转发的是第三方模型，其容量只有你知道。":
    "Nobody has said how large this source's window is, so there is no share to draw — and no automatic compaction either. A relay forwards somebody else's model, so only you know what it holds.",
  "该窗口值的来源无法确定 —— 点击可改为该模型的实际上限":
    "Whoever entered this window may not have meant this model — click to set what it actually holds",
  "只改当前这个模型，同一个来源下的其它模型不动。填模型文档写的上下文上限，不是最大输出。会重建运行时，任务跑着的时候改不了。":
    "Applies to this model alone; the others on this source are left as they are. The context ceiling from the model's docs, not its max output. This rebuilds the runtime, so it cannot be set while a task is running.",
  "填写模型文档中的上下文上限，而非最大输出长度。将重建运行时，任务运行期间无法修改。":
    "The context ceiling from the model's docs, not its max output. This rebuilds the runtime, so it cannot be set while a task is running.",
  "{n} 会话": "{n} sessions",
  "每回合用量": "Tokens per round",
  "峰值 {peak} · 均 {avg}": "peak {peak} · avg {avg}",
  "正在读这个文件的改动…": "Reading this file's changes…",
  "这个文件现在和上一次提交一样": "This file now matches the last commit",
  "改动太长，只显示了前面一段": "Too long to show in full — this is the start of it",
  "改了 {n} 次": "edited {n} times",
  "改过": "Modified",
};
