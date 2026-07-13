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
  footer,
  barRef,
  role = "dialog",
  decision = false,
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
  // Sticky confirm bar for select-then-confirm decision surfaces.
  footer?: ReactNode;
  barRef?: RefObject<HTMLDivElement | null>;
  role?: "dialog" | "region";
  // Decision surfaces keep a vertical full-width option list and a fixed
  // confirm footer; content scrolls within 55vh.
  decision?: boolean;
}) {
  return (
    <div
      className={[
        "prompt-shelf",
        decision ? "prompt-shelf--decision" : "",
        className ?? "",
      ]
        .filter(Boolean)
        .join(" ")}
      aria-live="polite"
    >
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
        {actions && <div className="prompt-shelf__actions" role="listbox">{actions}</div>}
        {note && <div className="prompt-shelf__footnote">{note}</div>}
        {quickActions && <div className="prompt-shelf__quick-actions">{quickActions}</div>}
        {footer && <div className="prompt-shelf__footer">{footer}</div>}
      </div>
    </div>
  );
}

export function PromptBadge({ children, tone }: { children: ReactNode; tone?: "default" | "amber" | "danger" }) {
  return (
    <span
      className={[
        "prompt-shelf__badge",
        tone === "amber" ? " prompt-shelf__badge--amber" : "",
        tone === "danger" ? " prompt-shelf__badge--danger" : "",
      ].join("")}
    >
      {children}
    </span>
  );
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
  // Keyboard cursor without implying a committed answer (multi-select).
  active = false,
  quiet = false,
  disabled = false,
  tone = "default",
  role = "option",
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
  active?: boolean;
  quiet?: boolean;
  disabled?: boolean;
  // Danger options (deny / clear) use semantic color but are never default-selected.
  tone?: "default" | "danger";
  role?: "option" | "button";
}) {
  const hasCopy = description != null || (label != null && label !== "");
  return (
    <button
      type="button"
      role={role}
      aria-selected={role === "option" ? selected : undefined}
      data-active={active ? "true" : undefined}
      className={[
        "prompt-action",
        primary || selected ? " prompt-action--selected" : "",
        active ? " prompt-action--active" : "",
        quiet ? " prompt-action--quiet" : "",
        description ? " prompt-action--descriptive" : "",
        !hasCopy ? " prompt-action--key-only" : "",
        tone === "danger" ? " prompt-action--danger" : "",
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

export function DecisionConfirmBar({
  hint,
  confirmLabel,
  onConfirm,
  disabled = false,
  confirmDisabled = false,
  danger = false,
}: {
  hint: ReactNode;
  confirmLabel: ReactNode;
  onConfirm: () => void;
  disabled?: boolean;
  confirmDisabled?: boolean;
  danger?: boolean;
}) {
  return (
    <div className="decision-confirm-bar">
      <div className="decision-confirm-bar__hint">{hint}</div>
      <button
        type="button"
        className={[
          "btn btn--small decision-confirm-bar__confirm",
          danger ? "btn--danger" : "btn--primary",
        ].join(" ")}
        onClick={onConfirm}
        disabled={disabled || confirmDisabled}
      >
        {confirmLabel}
      </button>
    </div>
  );
}
