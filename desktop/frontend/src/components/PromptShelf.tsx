import type { ReactNode, RefObject } from "react";

export { PromptAction, PromptDescriptionDisclosure, PromptDescriptionToggle } from "./PromptAction";

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
  actionsRole = "listbox",
  cardCollapsible = false,
  collapsed = false,
  onToggleCollapse,
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
  // confirm footer; all preceding content shares one viewport-bounded scroll.
  decision?: boolean;
  // Select-then-confirm surfaces use listbox. Immediate actions are a group of
  // buttons so assistive technology does not announce them as pending choices.
  actionsRole?: "listbox" | "group";
  // Whole-card click-to-collapse (used by the todo shelf): clicking anywhere
  // on the card toggles collapsed; interactive children stop propagation.
  cardCollapsible?: boolean;
  collapsed?: boolean;
  onToggleCollapse?: () => void;
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
        className={[
          "prompt-shelf__card",
          cardCollapsible ? "prompt-shelf__card--collapsible" : "",
          cardCollapsible && collapsed ? "prompt-shelf__card--collapsed" : "",
          cardClassName ?? "",
        ]
          .filter(Boolean)
          .join(" ")}
        role={role}
        aria-modal={role === "dialog" ? "false" : undefined}
        aria-labelledby={titleId}
        tabIndex={cardCollapsible ? 0 : -1}
        onClick={cardCollapsible ? onToggleCollapse : undefined}
        onKeyDown={cardCollapsible && onToggleCollapse ? (event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            onToggleCollapse();
          }
        } : undefined}
      >
        <div className="prompt-shelf__content">
          <div className="prompt-shelf__header">
            <div className="prompt-shelf__copy">
              <div id={titleId} className="prompt-shelf__title">
                <span className="prompt-shelf__heading">{title}</span>
                {meta && <span className="prompt-shelf__meta">{meta}</span>}
                {badges && <span className="prompt-shelf__badges">{badges}</span>}
              </div>
            </div>
            {headerActions && (
              <div className="prompt-shelf__header-actions" onClick={(event) => event.stopPropagation()}>
                {headerActions}
              </div>
            )}
          </div>
          {crumbs}
          {children && <div className="prompt-shelf__body">{children}</div>}
          {actions && <div className="prompt-shelf__actions" role={actionsRole}>{actions}</div>}
          {note && <div className="prompt-shelf__footnote">{note}</div>}
          {quickActions && <div className="prompt-shelf__quick-actions">{quickActions}</div>}
        </div>
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

export function DecisionConfirmBar({
  hint,
  confirmLabel,
  onConfirm,
  secondaryLabel,
  onSecondary,
  disabled = false,
  confirmDisabled = false,
  danger = false,
}: {
  hint: ReactNode;
  confirmLabel: ReactNode;
  onConfirm: () => void;
  secondaryLabel?: ReactNode;
  onSecondary?: () => void;
  disabled?: boolean;
  confirmDisabled?: boolean;
  danger?: boolean;
}) {
  return (
    <div className="decision-confirm-bar">
      {secondaryLabel && onSecondary && (
        <button
          type="button"
          className="btn btn--small decision-confirm-bar__secondary"
          onClick={onSecondary}
          disabled={disabled}
        >
          {secondaryLabel}
        </button>
      )}
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
