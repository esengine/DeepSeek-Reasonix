import type { ReactNode, RefObject } from "react";

export function PromptShelf({
  className,
  cardClassName,
  titleId,
  title,
  badges,
  meta,
  actions,
  children,
  crumbs,
  note,
  quickActions,
  headerActions,
  barRef,
  role = "dialog",
}: {
  className?: string;
  cardClassName?: string;
  titleId: string;
  title: ReactNode;
  badges?: ReactNode;
  meta?: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
  crumbs?: ReactNode;
  // Rendered between the actions grid and the quick actions; used for
  // focus-following detail previews and similar footnotes.
  note?: ReactNode;
  quickActions?: ReactNode;
  headerActions?: ReactNode;
  barRef?: RefObject<HTMLDivElement | null>;
  role?: "dialog" | "region";
}) {
  return (
    <div className={["prompt-shelf", className ?? ""].filter(Boolean).join(" ")} aria-live="polite">
      <div
        ref={barRef}
        className={["prompt-shelf__card", cardClassName ?? ""].filter(Boolean).join(" ")}
        role={role}
        aria-modal={role === "dialog" ? "false" : undefined}
        aria-labelledby={titleId}
        tabIndex={-1}
      >
        <div className="prompt-shelf__header">
          <div className="prompt-shelf__copy">
            <div id={titleId} className="prompt-shelf__title">
              <span className="prompt-shelf__heading">{title}</span>
              {badges && <span className="prompt-shelf__badges">{badges}</span>}
            </div>
            {meta && <div className="prompt-shelf__meta">{meta}</div>}
          </div>
          {headerActions && <div className="prompt-shelf__header-actions">{headerActions}</div>}
        </div>
        {crumbs}
        {children && <div className="prompt-shelf__body">{children}</div>}
        {actions && <div className="prompt-shelf__actions">{actions}</div>}
        {note && <div className="prompt-shelf__footnote">{note}</div>}
        {quickActions && <div className="prompt-shelf__quick-actions">{quickActions}</div>}
      </div>
    </div>
  );
}

export function PromptBadge({ children }: { children: ReactNode }) {
  return <span className="prompt-shelf__badge">{children}</span>;
}

export function PromptHeaderAction({
  children,
  onClick,
  ariaLabel,
  disabled = false,
}: {
  children: ReactNode;
  onClick: () => void;
  ariaLabel?: string;
  disabled?: boolean;
}) {
  return (
    <button
      className="prompt-shelf__header-button"
      type="button"
      onClick={onClick}
      aria-label={ariaLabel}
      disabled={disabled}
    >
      {children}
    </button>
  );
}

export function PromptAction({
  keyLabel,
  label,
  description,
  onClick,
  ariaLabel,
  title,
  onHoverChange,
  primary = false,
  selected = false,
  quiet = false,
  disabled = false,
}: {
  keyLabel: string;
  label?: ReactNode;
  description?: ReactNode;
  onClick: () => void;
  ariaLabel?: string;
  // Native tooltip fallback for truncated descriptions.
  title?: string;
  // Fires on mouse enter/focus (true) and mouse leave/blur (false) so the
  // parent can drive a focus-following detail preview.
  onHoverChange?: (hovering: boolean) => void;
  primary?: boolean;
  selected?: boolean;
  quiet?: boolean;
  disabled?: boolean;
}) {
  const hasCopy = description != null || (label != null && label !== "");
  return (
    <button
      type="button"
      className={[
        "prompt-action",
        primary || selected ? " prompt-action--selected" : "",
        quiet ? " prompt-action--quiet" : "",
        description ? " prompt-action--descriptive" : "",
        !hasCopy ? " prompt-action--key-only" : "",
      ].join("")}
      onClick={onClick}
      disabled={disabled}
      aria-label={ariaLabel}
      title={title}
      onMouseEnter={onHoverChange ? () => onHoverChange(true) : undefined}
      onMouseLeave={onHoverChange ? () => onHoverChange(false) : undefined}
      onFocus={onHoverChange ? () => onHoverChange(true) : undefined}
      onBlur={onHoverChange ? () => onHoverChange(false) : undefined}
    >
      {keyLabel && <span className="prompt-action__key">{keyLabel}</span>}
      {hasCopy && (
        <span className="prompt-action__copy">
          {label != null && label !== "" && <span className="prompt-action__label">{label}</span>}
          {description && <span className="prompt-action__desc">{description}</span>}
        </span>
      )}
    </button>
  );
}
