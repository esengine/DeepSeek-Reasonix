import { useState } from "react";
import { t, useLang } from "../i18n";

export type QuestionNavItem = {
  messageIndex: number;
  ordinal: number;
  turn: number;
  text: string;
};

type QuestionNavProps = {
  items: QuestionNavItem[];
  activeMessageIndex: number | null;
  onPick: (messageIndex: number) => void;
};

export function QuestionNav({ items, activeMessageIndex, onPick }: QuestionNavProps) {
  useLang();
  const [hovered, setHovered] = useState<number | null>(null);

  if (items.length === 0) return null;

  const activeItem = items.find((item) => item.messageIndex === activeMessageIndex) ?? items[0];
  const previewItem =
    items.find((item) => item.messageIndex === hovered) ?? activeItem ?? items[0];

  return (
    <nav className="question-nav" aria-label={t("questionNav.label")}>
      <div className="question-nav-rail">
        {items.map((item) => {
          const isActive = item.messageIndex === activeMessageIndex;
          const isHovered = item.messageIndex === hovered;
          const label = t("questionNav.jumpTo", { n: item.ordinal });
          return (
            <button
              key={item.messageIndex}
              type="button"
              className="question-nav-mark"
              data-active={isActive || undefined}
              data-hovered={isHovered || undefined}
              onClick={() => onPick(item.messageIndex)}
              onMouseEnter={() => setHovered(item.messageIndex)}
              onMouseLeave={() => setHovered(null)}
              onFocus={() => setHovered(item.messageIndex)}
              onBlur={() => setHovered(null)}
              title={`${label}: ${summarizeQuestion(item.text, 80)}`}
              aria-label={label}
            >
              <span />
            </button>
          );
        })}
      </div>
      {previewItem ? (
        <div className="question-nav-pop" role="status">
          <div className="question-nav-kicker">
            {t("questionNav.question")} #{previewItem.ordinal}
          </div>
          <div className="question-nav-text">{summarizeQuestion(previewItem.text, 96)}</div>
        </div>
      ) : null}
    </nav>
  );
}

function summarizeQuestion(text: string, max: number): string {
  const oneLine = text.replace(/\s+/g, " ").trim();
  if (oneLine.length <= max) return oneLine;
  return `${oneLine.slice(0, Math.max(0, max - 1))}…`;
}
