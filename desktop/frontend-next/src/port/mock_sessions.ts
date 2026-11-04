import type { SessionStatus } from "./session";
import type { Completion, CompletionItem } from "./port";

const PAST = [
  { id: "20261018-020806-feature", title: "把密钥轮换做成一键操作", at: "2026-10-18 02:08" },
  { id: "20261017-101010-flaky", title: "修一个偶发红的分支菜单测试", at: "2026-10-17 10:10" },
  { id: "20261016-183000-search", title: "跨对话搜索要能搜对话内容", at: "2026-10-16 18:30" },
];

/** What the kernel offers for a #word: past conversations whose title holds it,
 *  newest first, each inserting its @session reference. */
export function pastSessionItems(query: string): CompletionItem[] {
  const needle = query.trim().toLowerCase();
  return PAST.filter((s) => !needle || s.title.toLowerCase().includes(needle)).map((s) => ({
    label: s.title,
    insert: "@session:" + s.id,
    hint: s.at,
    kind: "session",
  }));
}

/** The #word menu under the caret, or null when the caret is not in one. */
export function sessionCompletion(line: string, cursor: number): Completion | null {
  const before = line.slice(0, cursor);
  const word = /(?:^|\s)#(\S*)$/.exec(before);
  if (!word) return null;
  const rest = line.slice(cursor).search(/\s/);
  return { kind: "session", from: before.length - word[1].length - 1, to: rest < 0 ? line.length : cursor + rest, query: word[1], items: pastSessionItems(word[1]) };
}

// ?jobs puts two running and two finished background jobs on the bench.
export function benchJobs(): Pick<SessionStatus, "jobs" | "endedJobs"> {
  const now = Date.now();
  return {
    jobs: [
      { id: "bash-1", kind: "bash", label: "npm run test", description: "跑桌面端测试", detail: "npm run test 2>&1 | tail -40", status: "running", startedAt: now - 741_000, tail: "compiling module 192 … ok" },
      { id: "bash-2", kind: "bash", label: "go test", description: "跑第 1 组内核测试", detail: "go test ./internal/... -run Kernel1", status: "running", startedAt: now - 741_000, tail: "ok  reasonix/internal/agent  41.2s" },
    ],
    endedJobs: [
      { id: "bash-3", kind: "bash", label: "go test", description: "跑第 2 组内核测试", detail: "go test ./internal/... -run Kernel2", status: "failed", startedAt: now - 300_000, endedAt: now - 58_000, exitCode: 1, tail: "FAIL TestCompactKeepsPins (0.31s)" },
      { id: "bash-4", kind: "bash", label: "npm run build", description: "构建前端", detail: "npm run build", status: "done", startedAt: now - 260_000, endedAt: now - 192_000, exitCode: 0 },
    ],
  };
}
