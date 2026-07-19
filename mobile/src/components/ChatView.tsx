import { ArrowLeft, ArrowUp, ShieldAlert } from "lucide-react";
import { useEffect, useRef } from "react";
import type { SessionDescriptor } from "../protocol/types";
import { t, type Locale } from "../i18n/messages";
import { IconButton } from "./IconButton";
import { TopBar } from "./TopBar";
import { useKeyboardInset } from "../lib/useKeyboardInset";

export interface ChatLine {
  id: string;
  kind: string;
  text: string;
  role?: "user" | "assistant" | "system" | "tool";
}

function lineRole(line: ChatLine): ChatLine["role"] {
  if (line.role) return line.role;
  if (line.kind === "user") return "user";
  if (
    line.kind === "tool_dispatch" ||
    line.kind === "tool_result" ||
    line.kind === "tool_progress" ||
    line.kind === "notice" ||
    line.kind === "approval_request"
  ) {
    return "tool";
  }
  if (line.kind === "turn_started" || line.kind === "turn_done") return "system";
  return "assistant";
}

export function ChatView({
  session,
  lines,
  draft,
  sending,
  locale,
  showBack,
  pendingApproval,
  onBack,
  onDraftChange,
  onSend,
  onOpenApproval,
}: {
  session: SessionDescriptor;
  lines: ChatLine[];
  draft: string;
  sending: boolean;
  locale: Locale;
  showBack: boolean;
  pendingApproval: boolean;
  onBack: () => void;
  onDraftChange: (v: string) => void;
  onSend: () => void;
  onOpenApproval?: () => void;
}) {
  const streamRef = useRef<HTMLDivElement>(null);
  const keyboardInset = useKeyboardInset();
  const subtitle =
    session.runtime === "local"
      ? t(locale, "sessions.runtimeLocal")
      : t(locale, "sessions.runtimeRemote");

  useEffect(() => {
    const el = streamRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [lines, keyboardInset, pendingApproval]);

  return (
    <div
      className="chat-shell anim-page"
      style={{ ["--rx-keyboard-inset" as string]: `${keyboardInset}px` }}
    >
      <TopBar
        title={session.title || session.id}
        subtitle={
          sending
            ? t(locale, "sessions.streaming")
            : pendingApproval
              ? t(locale, "sessions.statusPending")
              : subtitle
        }
        leading={
          showBack ? (
            <IconButton label={t(locale, "common.back")} onClick={onBack} neutral>
              <ArrowLeft size={22} strokeWidth={2} />
            </IconButton>
          ) : (
            <span style={{ width: 44 }} />
          )
        }
        trailing={
          sending ? (
            <span className="stream-pulse" aria-label={t(locale, "sessions.streaming")} />
          ) : (
            <span style={{ width: 44 }} />
          )
        }
      />

      {pendingApproval ? (
        <button type="button" className="approval-banner anim-slide-down" onClick={onOpenApproval}>
          <ShieldAlert size={16} aria-hidden />
          <span>{t(locale, "approval.banner")}</span>
          <span className="approval-banner-action">{t(locale, "approval.review")}</span>
        </button>
      ) : null}

      <div className="chat-stream" ref={streamRef} aria-live="polite">
        {lines.length === 0 ? (
          <div className="chat-empty anim-enter">
            <p>{t(locale, "sessions.chatEmpty")}</p>
            <p className="chat-empty-hint">{t(locale, "sessions.chatEmptyHint")}</p>
          </div>
        ) : (
          lines.map((line, i) => {
            const role = lineRole(line);
            const delay = Math.min(i, 8) * 20;
            if (role === "tool") {
              return (
                <div
                  key={line.id}
                  className="tool-timeline anim-msg"
                  data-kind={line.kind}
                  style={{ animationDelay: `${delay}ms` }}
                >
                  {line.text}
                </div>
              );
            }
            return (
              <div
                key={line.id}
                className="bubble anim-msg"
                data-role={role}
                style={{ animationDelay: `${delay}ms` }}
              >
                {line.text}
              </div>
            );
          })
        )}
        {sending && !pendingApproval ? (
          <div className="typing-dots" aria-hidden>
            <span />
            <span />
            <span />
          </div>
        ) : null}
      </div>
      <div className="composer-dock">
        <div className="composer-row">
          <textarea
            className="composer-field"
            value={draft}
            rows={1}
            onChange={(e) => onDraftChange(e.target.value)}
            placeholder={t(locale, "composer.placeholder")}
            aria-label={t(locale, "composer.placeholder")}
            disabled={pendingApproval}
            onKeyDown={(e) => {
              // IME composition Enter commits the candidate, not the message.
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault();
                onSend();
              }
            }}
          />
          <button
            type="button"
            className="composer-send"
            onClick={onSend}
            disabled={sending || pendingApproval || !draft.trim()}
            aria-label={t(locale, "composer.send")}
          >
            <ArrowUp size={20} strokeWidth={2.5} />
          </button>
        </div>
      </div>
    </div>
  );
}
