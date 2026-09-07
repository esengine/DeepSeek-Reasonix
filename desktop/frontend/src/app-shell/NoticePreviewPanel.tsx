import { NoticeCard } from "../components/Transcript";
import { t } from "../lib/i18n";
import { localizedNoticeText, type Item } from "../lib/useController";
import { browserMockScenarioParam } from "../lib/mockScenarios";

export function noticePreviewMockEnabled(): boolean {
  const value = browserMockScenarioParam();
  return value === "notice" || value === "notices" || value === "notice-preview";
}

function noticePreviewItems(): Item[] {
  const notice = (index: number, level: "info" | "warn", text: string, detail: string, code?: string): Item => ({
    kind: "notice",
    id: `notice-preview-${index}`,
    level,
    text: localizedNoticeText(text, code),
    detail,
  });
  return [
    {
      kind: "notice",
      id: "notice-preview-delivery",
      level: "info",
      variant: "delivery",
      title: t("notice.deliveryIncompleteTitle"),
      text: t("notice.deliveryIncompleteBody"),
      detail: "final-answer readiness failed 3 times: missing verification, review_report, and complete_step receipts",
      action: "continue_delivery",
    },
    notice(1, "info", "No visible answer was produced; asking the assistant to respond again.", "empty final answer blocked: qwen3.7-plus returned no visible answer text (finish=stop, reasoning=2314 chars); retrying", "empty_final"),
    notice(2, "info", "The assistant answered before taking action; asking it to use the required tools.", "executor handoff: assistant produced a proposal before running required repository commands; nudged to execute", "executor_handoff"),
    notice(3, "info", "Tool round limit reached; asking the assistant to summarize progress.", "tool budget reached after 128 tool calls; requesting a progress summary before continuing", "tool_budget"),
    notice(4, "info", "The assistant is stuck retrying a blocked action; asking it to change approach.", "loop guard: repeated command failure matched the same stderr signature across 3 attempts", "loop_guard"),
    notice(5, "info", "Context is getting large; preserving cache until cleanup is needed.", "context window 82% full; deferred cleanup to preserve reusable prompt cache"),
    notice(6, "info", "Context cleanup skipped for now.", "cleanup skipped: recent turn included unresolved user approval state"),
    notice(7, "info", "Automatic context cleanup paused because the context window is too small.", "configured compact threshold exceeds current model context window; auto cleanup paused for this model"),
    notice(8, "info", "Context was compacted without a generated summary.", "compaction completed after upstream summary generation returned empty content; retained transcript checkpoint"),
    notice(9, "info", "Goal is not ready to complete yet; continuing the remaining work.", "goal completion check found pending validation: desktop/frontend typecheck"),
    notice(13, "info", "Goal still has unfinished task state; continuing the remaining work.", "active goal has open task state: implement preview, verify browser, report result"),
    notice(16, "warn", "background export failed: needs attention", "background export failed: session archive upload returned 503 after 3 retries"),
    notice(17, "warn", "Job artifact migration failed.", "artifact migration failed for job job_123: checksum mismatch while moving output.zip"),
    notice(18, "warn", "Background job teardown timed out.", "job job_123 did not stop within 10s; process is still marked running by the supervisor"),
    notice(19, "warn", "Some plan-mode tool settings were ignored.", "plan-mode tool settings ignored: unsupported tool allowlist entry \"browser.screenshot\""),
    notice(20, "warn", "Some plan-mode command settings were ignored.", "plan-mode command settings ignored: invalid read-only prefix \"npm && test\""),
    notice(21, "warn", "Config migration did not complete.", "config migration failed at providers.defaultModel: unknown provider reference \"old/deepseek\""),
    notice(22, "warn", "Selected model is missing its API key.", "selected model deepseek/deepseek-v4-pro requires DEEPSEEK_API_KEY, but no key is configured"),
    notice(23, "warn", "An MCP server failed to start.", "mcp server \"github\" failed to start: command not found: mcp-server-github"),
    notice(24, "warn", "Some MCP servers failed to start; run /mcp for details.", "mcp startup failures: github(command not found), linear(authentication expired)"),
    notice(25, "warn", "Guardian was disabled because its model was not found.", "guardian model \"glm-5-guard\" is not present in the configured provider catalog"),
    notice(26, "warn", "Guardian was disabled because it could not start.", "guardian startup failed: provider returned 401 unauthorized"),
  ];
}

export function NoticePreviewPanel() {
  return (
    <div
      style={{
        flex: "1 1 auto",
        minHeight: 0,
        overflow: "auto",
        padding: "44px 24px 128px",
      }}
    >
      <div style={{ maxWidth: 920, margin: "0 auto" }}>
        {noticePreviewItems().map((item) => {
          if (item.kind !== "notice") return null;
          return (
            <NoticeCard
              key={item.id}
              item={item}
              onAction={item.action ? () => undefined : undefined}
              onAccept={item.action === "continue_delivery" ? () => undefined : undefined}
            />
          );
        })}
      </div>
    </div>
  );
}
