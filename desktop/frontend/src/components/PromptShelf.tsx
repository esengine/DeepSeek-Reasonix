import type { ReactNode, RefObject } from "react";
import { ChevronDown, ChevronUp, PauseCircle } from "lucide-react";

export function PromptShelf({
  titleId,
  title,
  badges,
  meta,
  actions,
  children,
  crumbs,
  quickActions,
  barRef,
  actionsWrap = false,
}: {
  titleId: string;
  title: ReactNode;
  badges?: ReactNode;
  meta: ReactNode;
  actions: ReactNode;
  children?: ReactNode;
  crumbs?: ReactNode;
  quickActions?: ReactNode;
  barRef?: RefObject<HTMLDivElement>;
  actionsWrap?: boolean;
}) {
  return (
    <div className={`prompt-shelf${actionsWrap ? " prompt-shelf--actions-wrap" : ""}`} aria-live="polite">
      <div
        ref={barRef}
        className="prompt-shelf__bar"
        role="dialog"
        aria-modal="false"
        aria-labelledby={titleId}
        tabIndex={-1}
      >
        <div className="prompt-shelf__summary">
          <PauseCircle size={16} aria-hidden="true" />
          <div className="prompt-shelf__copy">
            <div id={titleId} className="prompt-shelf__title">
              <span className="prompt-shelf__heading">{title}</span>
              {badges && <span className="prompt-shelf__badges">{badges}</span>}
            </div>
            <div className="prompt-shelf__meta">{meta}</div>
          </div>
        </div>
        <div className="prompt-shelf__actions">{actions}</div>
      </div>
      {crumbs}
      {children && <div className="prompt-shelf__panel">{children}</div>}
      {quickActions}
    </div>
  );
}

export function PromptBadge({ children }: { children: ReactNode }) {
  return <span className="prompt-shelf__badge">{children}</span>;
}

export function PromptAction({
  keyLabel,
  label,
  onClick,
  primary = false,
  selected = false,
}: {
  keyLabel: string;
  label: ReactNode;
  onClick: () => void;
  primary?: boolean;
  selected?: boolean;
}) {
  return (
    <button className={`prompt-action${primary || selected ? " prompt-action--selected" : ""}`} onClick={onClick}>
      <span className="prompt-action__key">{keyLabel}</span>
      <span className="prompt-action__label">{label}</span>
    </button>
  );
}

export function PromptDetailToggle({
  open,
  label,
  openLabel = label,
  onClick,
}: {
  open: boolean;
  label: ReactNode;
  openLabel?: ReactNode;
  onClick: () => void;
}) {
  return (
    <button className="prompt-detail-toggle" onClick={onClick}>
      <span>{open ? openLabel : label}</span>
      {open ? <ChevronUp size={14} aria-hidden="true" /> : <ChevronDown size={14} aria-hidden="true" />}
    </button>
  );
}
