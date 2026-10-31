import { useState } from "react";
import { t } from "../../i18n";
import type { MessageOrigin } from "../../port/team";
import { useAgentName } from "../../state/team";
import { StudioIcon } from "../StudioIcon";

// The body of the block the host framed a delivery in: what is between the
// opening and closing tag, with the escaped tags put back.
function framedBody(text: string): string {
  const open = text.indexOf(">\n");
  const tag = text.match(/^<([a-z-]+)[\s>]/)?.[1];
  const close = tag ? text.indexOf(`\n</${tag}>`) : -1;
  if (open < 0 || close < open) return text.trim();
  return text.slice(open + 2, close).replace(/<\\/g, "<").trim();
}

// A turn the host ran for another agent or a group, which the transcript would
// otherwise show as an answer to nothing.
export function OriginLine({ origin, text }: { origin: MessageOrigin; text: string }) {
  const [open, setOpen] = useState(false);
  const name = useAgentName(origin.agent) ?? t("另一个 Agent");
  const label =
    origin.kind === "user_message" ? t("终端消息（来源未验证）")
      : origin.kind === "schedule" ? t("定时任务")
      : origin.kind === "group_turn" ? t("群聊轮到这个 Agent 发言")
        : origin.kind === "reply" ? t("{name} 的回复", { name }) : t("来自 {name} 的消息", { name });
  const body = framedBody(text);
  return (
    <div className="team-origin" data-kind={origin.kind}>
      <button type="button" className="team-origin-head" data-action="team.origin-toggle" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        <StudioIcon name={origin.kind === "group_turn" ? "chat" : origin.kind === "schedule" ? "clock" : "agent"} aria-hidden="true" />
        <span>{label}</span>
        <StudioIcon name={open ? "down" : "chevron"} aria-hidden="true" />
      </button>
      {open && body && <p className="team-origin-body">{body}</p>}
    </div>
  );
}
