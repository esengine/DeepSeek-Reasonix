import type { MemoryCatalog, MemoryEntry } from "./port";

export function mockMemoryCatalog(memories: MemoryEntry[]): MemoryCatalog {
  return {
    memories: memories.map((m) => ({ ...m })),
    recallQuery: "缓存命中率为什么会掉",
    indexPath: "~/.reasonix/projects/reasonix/memory/MEMORY.md",
  };
}

// The memory shelf the window opens onto with no kernel behind it. Data only:
// how a save or a restore moves it is the port's, and lives beside the calls
// that do it.
export const MEMORIES: MemoryEntry[] = [
    {
      name: "no-coauthored-by", title: "提交信息不带 Co-Authored-By",
      description: "也不要 Generated with 之类的署名脚注", activation: "pinned",
      scope: "project", type: "feedback", updatedAt: "2026-06-11",
      galaxy: "发布流程", importance: 3, state: "clear", clearForDays: 64, uses: 9, lastUsedAt: "2026-10-31",
      body: "提交信息里不要出现 Co-Authored-By，PR 描述里也不要生成署名脚注。",
      path: "~/.reasonix/projects/reasonix/memory/no-coauthored-by.md",
    },
    {
      name: "reply-language", title: "回复用中文",
      description: "跟着用户每条消息的语言走", activation: "pinned",
      scope: "global", type: "user", updatedAt: "2026-05-02",
      galaxy: "用户偏好", importance: 3, state: "clear", noDecay: true, uses: 21, lastUsedAt: "2026-11-01",
      body: "回复语言跟随用户当前这条消息的语言。",
    },
    {
      name: "v2-rewrite", title: "v2 是从零重写的 Go 内核",
      description: "没有 web，桌面端重做；main-v2 是默认分支", activation: "relevant",
      scope: "project", type: "project", updatedAt: "2026-05-30",
      galaxy: "架构", importance: 2, state: "fuzzy", uses: 3, lastUsedAt: "2026-09-12",
      body: "v2 = 从零重写的 Go 内核。不带 web；桌面端重做。main-v2 是默认分支。",
      usedLastTurn: true, why: "问题里提到了 main-v2 和分支",
    },
    {
      name: "old-build-flag", title: "构建用 -tags legacy",
      description: "迁移前的构建方式", activation: "relevant",
      scope: "project", type: "reference", updatedAt: "2025-11-02", expired: true,
      importance: 1, state: "faded",
      body: "构建时加 -tags legacy。",
    },
    {
      name: "merge-queue-first", title: "发版前先确认合并队列", description: "队列里有严重修复就先合进同一版",
      activation: "relevant", scope: "project", type: "constraint", createdAt: "2026-08-20", updatedAt: "2026-08-20",
      galaxy: "发布流程", importance: 3, state: "clear", noDecay: true, uses: 6, lastUsedAt: "2026-10-28",
      body: "发版前先扫一遍待合并的修复；有严重的先合进同一版再发，免得补发。",
    },
    {
      name: "one-release-a-day", title: "每天最多发一个版本", description: "热修只在完全不可用时例外",
      activation: "relevant", scope: "project", type: "project", createdAt: "2026-11-02", updatedAt: "2026-11-02",
      galaxy: "发布流程", importance: 2, state: "clear", clearForDays: 16, new: true,
      body: "北京日历日最多发一个版本；热修只在“完全不可用”时例外。",
    },
    {
      name: "tests-on-windows", title: "测试都在 Windows 机器上跑", description: "Mac 只改代码",
      activation: "relevant", scope: "global", type: "feedback", createdAt: "2026-11-01", updatedAt: "2026-11-01",
      galaxy: "工作流", importance: 2, state: "clear", clearForDays: 15, new: true,
      body: "所有 Go/前端测试和 lint 用 wintest.sh 在 Windows 机器上跑。",
    },
  ]

// A fortnight with the shape a real one has: a couple of heavy days, a quiet
// stretch, and an early span the recorder priced before cost was persisted —
// those days carry tokens and no cost, which the panel must not render as free.
export const USAGE_TOKENS = [0, 34_429_281, 106_429_066, 45_257_473, 0, 178_805, 795_413,
  5_910_837, 27_075_946, 8_447_084, 31_405_730, 1_672_731, 4_319_099];
export const USAGE_PRICED: (string | null)[] = [null, null, null, null, null, "0.028", "0.204", "1.107",
  "1.778", "0.440", "5.749", "0.330", "0.853"];
