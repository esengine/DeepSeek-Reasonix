import "./SubagentDetails.css";

import { Markdown } from "./Markdown";
import { ReasoningSummary } from "./ReasoningSummary";
import { useT } from "../lib/i18n";
import type { SubagentProgress } from "../lib/useController";

type SubagentPreviewProps = {
  progress: SubagentProgress;
  showReasoning: boolean;
  reasoningOpen: boolean;
  onReasoningToggle: () => void;
  onReasoningOpen: () => void;
};

export function SubagentPreview({
  progress,
  showReasoning,
  reasoningOpen,
  onReasoningToggle,
  onReasoningOpen,
}: SubagentPreviewProps) {
  const t = useT();
  return (
    <div className="tool__subagent-preview">
      {progress.reasoning && showReasoning && (
        <div className="tool__subagent-preview-section">
          <button
            type="button"
            className="tool__subagent-preview-label tool__subagent-preview-label--toggle"
            onClick={onReasoningToggle}
            aria-expanded={reasoningOpen}
          >
            {t("subagent.preview.reasoning")}
          </button>
          {reasoningOpen ? (
            <div className="tool__subagent-preview-text tool__subagent-preview-text--markdown">
              <Markdown text={progress.reasoning} streaming={progress.phase === "reasoning"} />
            </div>
          ) : (
            <ReasoningSummary
              text={progress.reasoning}
              streaming={progress.phase === "reasoning"}
              onOpen={onReasoningOpen}
            />
          )}
        </div>
      )}
      {progress.text && (
        <div className="tool__subagent-preview-section">
          <div className="tool__subagent-preview-label">{t("subagent.preview.text")}</div>
          <pre className="tool__subagent-preview-text">{progress.text}</pre>
        </div>
      )}
      {progress.notice && (
        <div className="tool__subagent-preview-section">
          <div className="tool__subagent-preview-label">{t("subagent.preview.notice")}</div>
          <pre className="tool__subagent-preview-text">{progress.notice}</pre>
        </div>
      )}
      {progress.truncated && <div className="tool__note">{t("subagent.preview.truncated")}</div>}
    </div>
  );
}
