// Adapted from DeepSeek Harness c291e7961a, ui-chat/TurnProcessNodeView.tsx (MIT).
import { memo } from "react";
import { ChevronDown } from "lucide-react";
import type { ChatNode } from "../../lib/chatViewSource";
import { useT } from "../../lib/i18n";
import css from "./TurnProcessNodeView.styles";

export const TurnProcessNodeView = memo(function TurnProcessNodeView({ node, onToggle }: {
  node: Extract<ChatNode, { kind: "process" }>; onToggle: () => void;
}) {
  const t = useT();
  if (!node.foldable) return null;
  const labels: string[] = [];
  if (node.toolCallCount) labels.push(t("chat.process.tools", { count: node.toolCallCount }));
  if (node.messageCount) labels.push(t("chat.process.messages", { count: node.messageCount }));
  if (node.subagentCount) labels.push(t("chat.process.subagents", { count: node.subagentCount }));
  if (node.failureCount) labels.push(t("chat.process.failures", { count: node.failureCount }));
  return <button type="button" className={`${css.root} chat-process`} data-open={!node.collapsed || undefined}
    data-turn-process={node.turnKey} aria-expanded={!node.collapsed}
    onClick={event => { event.currentTarget.focus({ preventScroll: true }); onToggle(); }}>
    <span className={css.label}>{labels.length ? labels.join(" · ") : t("chat.process")}</span>
    <ChevronDown className={css.chevron} />
  </button>;
});
