import { useEffect, useRef } from "react";
import { t } from "../i18n";
import type { FindScope } from "../state/find";
import type { Finding } from "./usefind";

const SCOPES: [FindScope, string][] = [
  ["all", "chat.find_scope_all"],
  ["user", "chat.find_scope_user"],
  ["reply", "chat.find_scope_reply"],
  ["tool", "chat.find_scope_tool"],
];

export function Find({
  find,
  pending = false,
  failed = false,
}: {
  find: Finding;
  pending?: boolean;
  failed?: boolean;
}) {
  return (
    <div className="findhost">
      {find.open && <Bar find={find} pending={pending} failed={failed} />}
    </div>
  );
}

function Bar({
  find,
  pending,
  failed,
}: {
  find: Finding;
  pending: boolean;
  failed: boolean;
}) {
  const {
    query,
    total,
    index,
    capped,
    focus,
    regex,
    caseSensitive,
    scope,
    setMode,
    ask: onQuery,
    step: onStep,
    close: onClose,
  } = find;
  const box = useRef<HTMLInputElement>(null);
  useEffect(() => {
    box.current?.focus();
    box.current?.select();
  }, [focus]);

  const asked = query.trim().length > 0;
  const count =
    pending && asked
      ? failed
        ? t("chat.some_history_has_not_been")
        : t("chat.searching_earlier_messages")
      : total > 0
        ? `${index + 1} / ${total}${capped ? "+" : ""}`
        : asked
          ? t("chat.no_matches")
          : "";

  const missed = asked && total === 0 && !pending;
  const advice =
    scope !== "all"
      ? "chat.find_try_all_scope"
      : caseSensitive
        ? "chat.find_try_without_case"
        : regex
          ? "chat.find_try_without_regex"
          : "";
  const hint = missed
    ? [t("chat.find_searched_everything"), advice && t(advice)]
        .filter(Boolean)
        .join(" · ")
    : "";

  return (
    <div className="tfindbox">
      <div
        className="tfind"
        role="search"
        data-missed={missed ? "" : undefined}
      >
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d="M7.2 3.1a4.1 4.1 0 1 0 0 8.2 4.1 4.1 0 0 0 0-8.2M10.3 10.3 13 13" />
        </svg>
        <input
          ref={box}
          type="search"
          data-window-keys=""
          aria-keyshortcuts="Enter Shift+Enter ArrowDown ArrowUp Escape"
          data-action-change="transcript.find"
          data-action-keydown="transcript.find"
          data-value="query"
          value={query}
          placeholder={t("common.find_in_this_conversation")}
          aria-label={t("common.find_in_this_conversation")}
          onChange={(ev) => onQuery(ev.target.value)}
          onKeyDown={(ev) => {
            if (ev.nativeEvent.isComposing) return;
            if (
              ev.key === "Enter" ||
              ev.key === "ArrowDown" ||
              ev.key === "ArrowUp"
            ) {
              ev.preventDefault();
              onStep(
                ev.key === "ArrowUp" || (ev.key === "Enter" && ev.shiftKey)
                  ? -1
                  : 1,
              );
            } else if (ev.key === "Escape") {
              ev.preventDefault();
              ev.stopPropagation();
              onClose();
            }
          }}
        />
        <span
          className="n"
          data-empty={missed ? "" : undefined}
          aria-live="polite"
        >
          {count}
        </span>
        <button
          className="st"
          data-action="transcript.find"
          data-value="prev"
          disabled={total === 0}
          aria-label={t("chat.previous_match")}
          title={t("chat.previous_match")}
          onClick={() => onStep(-1)}
        >
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <path d="M4.4 9.6 8 6l3.6 3.6" />
          </svg>
        </button>
        <button
          className="st"
          data-action="transcript.find"
          data-value="next"
          disabled={total === 0}
          aria-label={t("chat.next_match")}
          title={t("chat.next_match")}
          onClick={() => onStep(1)}
        >
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <path d="M4.4 6.4 8 10l3.6-3.6" />
          </svg>
        </button>
        <span className="sep" aria-hidden="true" />
        <button
          className="mode-toggle"
          data-action="transcript.find"
          data-value="case"
          aria-pressed={caseSensitive}
          aria-label={t("common.match_case")}
          title={t("common.match_case")}
          onClick={() => setMode({ caseSensitive: !caseSensitive })}
        >
          Aa
        </button>
        <button
          className="mode-toggle"
          data-action="transcript.find"
          data-value="regex"
          aria-pressed={regex}
          aria-label={t("chat.read_the_query_as_a")}
          title={t("chat.read_the_query_as_a")}
          onClick={() => setMode({ regex: !regex })}
        >
          .*
        </button>
        <button
          className="st"
          data-action="transcript.find"
          data-value="close"
          aria-label={t("chat.close_find")}
          title={t("chat.close_find")}
          onClick={onClose}
        >
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <path d="M4.5 4.5 11.5 11.5M11.5 4.5 4.5 11.5" />
          </svg>
        </button>
      </div>
      {missed ? (
        <div className="tfind-hint">{hint}</div>
      ) : (
        <div
          className="tfind-scope"
          role="group"
          aria-label={t("chat.find_scope_label")}
        >
          {SCOPES.map(([id, label]) => (
            <button
              key={id}
              type="button"
              data-action="transcript.find"
              data-value={`scope:${id}`}
              aria-pressed={scope === id}
              onClick={() => setMode({ scope: id })}
            >
              {t(label)}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
