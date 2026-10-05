import { useEffect, useRef } from "react";
import { t } from "../i18n";
import type { Finding } from "./usefind";

export function Find({ find }: { find: Finding }) {
  return <div className="findhost">{find.open && <Bar find={find} />}</div>;
}

function Bar({ find }: { find: Finding }) {
  const { query, total, index, capped, focus, ask: onQuery, step: onStep, close: onClose } = find;
  const box = useRef<HTMLInputElement>(null);
  useEffect(() => {
    box.current?.focus();
    box.current?.select();
  }, [focus]);

  const asked = query.trim().length > 0;
  const count = total > 0 ? `${index + 1} / ${total}${capped ? "+" : ""}` : asked ? t("无结果") : "";

  return (
    <div className="tfind" role="search">
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
        placeholder={t("在这段对话里查找")}
        aria-label={t("在这段对话里查找")}
        onChange={(ev) => onQuery(ev.target.value)}
        onKeyDown={(ev) => {
          if (ev.nativeEvent.isComposing) return;
          if (ev.key === "Enter" || ev.key === "ArrowDown" || ev.key === "ArrowUp") {
            ev.preventDefault();
            onStep(ev.key === "ArrowUp" || (ev.key === "Enter" && ev.shiftKey) ? -1 : 1);
          } else if (ev.key === "Escape") {
            ev.preventDefault();
            ev.stopPropagation();
            onClose();
          }
        }}
      />
      <span className="n" data-empty={asked && total === 0 ? "" : undefined} aria-live="polite">
        {count}
      </span>
      <button
        className="st"
        data-action="transcript.find"
        data-value="prev"
        disabled={total === 0}
        aria-label={t("上一处")}
        title={t("上一处")}
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
        aria-label={t("下一处")}
        title={t("下一处")}
        onClick={() => onStep(1)}
      >
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d="M4.4 6.4 8 10l3.6-3.6" />
        </svg>
      </button>
      <button className="clr" data-action="transcript.find" data-value="close" aria-label={t("关闭查找")} onClick={onClose}>
        ×
      </button>
    </div>
  );
}
