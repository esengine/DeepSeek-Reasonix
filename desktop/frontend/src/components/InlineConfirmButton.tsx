import { useEffect, useState } from "react";
import type { ReactNode } from "react";

// Compact row and menu actions confirm in place instead of opening a global
// modal. First click arms the action, second click confirms it, and the adjacent
// Cancel button or any disabled state returns the button to normal.
export function InlineConfirmButton({
  label,
  confirmLabel,
  cancelLabel,
  disabled = false,
  danger = false,
  primary = false,
  buttonRole,
  onConfirm,
}: {
  label: ReactNode;
  confirmLabel: ReactNode;
  cancelLabel: ReactNode;
  disabled?: boolean;
  danger?: boolean;
  primary?: boolean;
  buttonRole?: "menuitem";
  onConfirm: () => void | Promise<void>;
}) {
  const [armed, setArmed] = useState(false);

  useEffect(() => {
    if (disabled) setArmed(false);
  }, [disabled]);

  const run = async () => {
    if (!armed) {
      setArmed(true);
      return;
    }
    setArmed(false);
    await onConfirm();
  };

  return (
    <span className="inline-confirm" role={buttonRole ? "none" : undefined}>
      <button
        className={`btn btn--small${armed && danger ? " btn--danger" : primary ? " btn--primary" : ""}`}
        disabled={disabled}
        type="button"
        role={buttonRole}
        onClick={run}
      >
        {armed ? confirmLabel : label}
      </button>
      {armed && (
        <button className="btn btn--small" disabled={disabled} type="button" role={buttonRole} onClick={() => setArmed(false)}>
          {cancelLabel}
        </button>
      )}
    </span>
  );
}
