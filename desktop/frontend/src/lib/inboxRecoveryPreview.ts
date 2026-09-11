const RECOVERED_INSTRUCTION_COUNT = 30;
const RECOVERED_INSTRUCTION_BYTES = 1024 * 1024;

const PREVIEWS = [
  "分析知识库原文并整理关键结论、引用位置与待确认事项。",
  "检查仓库实现，补充回归测试并汇报验证结果。",
  "对比现有设计与需求，提出最小改动方案后执行。",
] as const;

let recoveryPaused = true;

export function setInboxRecoveryPreviewPaused(paused: boolean): void {
  recoveryPaused = paused;
}

export function inboxRecoveryPreviewSnapshot(paused = recoveryPaused) {
  const items = Array.from({ length: RECOVERED_INSTRUCTION_COUNT }, (_, index) => ({
    id: `recovered-${index + 1}`,
    intent: "followup",
    state: "uncertain",
    preview: `${index + 1}. ${PREVIEWS[index % PREVIEWS.length]}`,
    byteSize: RECOVERED_INSTRUCTION_BYTES,
    source: "desktop",
    position: index + 1,
  }));
  return {
    revision: 1,
    paused,
    recovered: true,
    recoveredCount: RECOVERED_INSTRUCTION_COUNT,
    items,
    itemsCount: items.length,
    bytes: items.length * RECOVERED_INSTRUCTION_BYTES,
    maxItems: 64,
    maxBytes: 64 * 1024 * 1024,
  };
}
