import type { MemoryEntry } from "./port";

// The memory shelf the window opens onto with no kernel behind it. Data only:
// how a save or a restore moves it is the port's, and lives beside the calls
// that do it.
export const MEMORIES: MemoryEntry[] = [] = [
    {
      name: "no-coauthored-by", title: "提交信息不带 Co-Authored-By",
      description: "也不要 Generated with 之类的署名脚注", activation: "pinned",
      scope: "project", type: "feedback", updatedAt: "2026-06-11",
      body: "提交信息里不要出现 Co-Authored-By，PR 描述里也不要生成署名脚注。",
      path: "~/.reasonix/projects/reasonix/memory/no-coauthored-by.md",
    },
    {
      name: "reply-language", title: "回复用中文",
      description: "跟着用户每条消息的语言走", activation: "pinned",
      scope: "global", type: "user", updatedAt: "2026-05-02",
      body: "回复语言跟随用户当前这条消息的语言。",
    },
    {
      name: "v2-rewrite", title: "v2 是从零重写的 Go 内核",
      description: "没有 web，桌面端重做；main-v2 是默认分支", activation: "relevant",
      scope: "project", type: "project", updatedAt: "2026-05-30",
      body: "v2 = 从零重写的 Go 内核。不带 web；桌面端重做。main-v2 是默认分支。",
      usedLastTurn: true, why: "问题里提到了 main-v2 和分支",
    },
    {
      name: "old-build-flag", title: "构建用 -tags legacy",
      description: "迁移前的构建方式", activation: "relevant",
      scope: "project", type: "reference", updatedAt: "2025-11-02", expired: true,
      body: "构建时加 -tags legacy。",
    },
  ]

// A fortnight with the shape a real one has: a couple of heavy days, a quiet
// stretch, and an early span the recorder priced before cost was persisted —
// those days carry tokens and no cost, which the panel must not render as free.
export const USAGE_TOKENS = [0, 34_429_281, 106_429_066, 45_257_473, 0, 178_805, 795_413,
  5_910_837, 27_075_946, 8_447_084, 31_405_730, 1_672_731, 4_319_099];
export const USAGE_PRICED: (string | null)[] = [null, null, null, null, null, "0.028", "0.204", "1.107",
  "1.778", "0.440", "5.749", "0.330", "0.853"];
