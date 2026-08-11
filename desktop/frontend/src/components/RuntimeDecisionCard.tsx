import { useEffect, useRef } from "react";
import { PromptAction, PromptBadge, PromptShelf } from "./PromptShelf";

export type RuntimeDecisionAction = {
  key: string;
  label: string;
  description: string;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
};

export function RuntimeDecisionCard({
  id,
  title,
  badge,
  meta,
  note,
  actions,
  secondaryAction,
  onCancel,
}: {
  id: string;
  title: string;
  badge: string;
  meta: string;
  note?: string;
  actions: RuntimeDecisionAction[];
  secondaryAction?: RuntimeDecisionAction;
  onCancel: () => void;
}) {
  const shelfRef = useRef<HTMLDivElement | null>(null);
  const actionsRef = useRef(actions);
  const onCancelRef = useRef(onCancel);
  actionsRef.current = actions;
  onCancelRef.current = onCancel;

  useEffect(() => {
    shelfRef.current?.focus();
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const target = event.target instanceof Element ? event.target : null;
      const tag = target?.tagName.toLowerCase();
      if (tag === "input" || tag === "textarea" || (target instanceof HTMLElement && target.isContentEditable)) return;

      if (event.key === "Escape") {
        event.preventDefault();
        onCancelRef.current();
        return;
      }
      if (event.repeat || event.metaKey || event.ctrlKey || event.altKey) return;
      const action = actionsRef.current.find((candidate) => candidate.key === event.key);
      if (!action || action.disabled) return;
      event.preventDefault();
      action.onClick();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  return (
    <PromptShelf
      decision
      className="prompt-shelf--runtime-decision"
      barRef={shelfRef}
      titleId={`${id}-title`}
      title={title}
      badges={<PromptBadge tone="amber">{badge}</PromptBadge>}
      meta={meta}
      actionsRole="group"
      actions={actions.map((action) => (
        <PromptAction
          key={action.key}
          keyLabel={action.key}
          label={action.label}
          description={action.description}
          descriptionDisclosure
          descriptionAlwaysVisible={Boolean(action.danger)}
          onClick={action.onClick}
          disabled={action.disabled}
          tone={action.danger ? "danger" : "default"}
          role="button"
        />
      ))}
      footer={secondaryAction ? (
        <div className="runtime-decision-footer">
          <button
            type="button"
            className="btn btn--small runtime-decision-footer__secondary"
            onClick={secondaryAction.onClick}
            disabled={secondaryAction.disabled}
          >
            {secondaryAction.label}
          </button>
        </div>
      ) : undefined}
    >
      {note && <p className="prompt-shelf__note">{note}</p>}
    </PromptShelf>
  );
}
