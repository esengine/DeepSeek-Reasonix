// Why a held queue entry stopped, keyed by the kernel's block code. A code with
// no entry shows the kernel's own diagnostic.
export const BLOCK_WHY: Record<string, string> = {
  steer_unapplied: "这条插话在本轮结束前没来得及送达，没有发给模型",
  turn_snapshot_failed: "本轮已结束，但对话记录没能保存",
  turn_ack_failed: "本轮已结束，但队列没能确认这一条",
  owner_inactive: "运行这一条的会话已不在了",
  manifest_salvaged: "队列记录损坏，这一条是从残留文件里找回的",
};
