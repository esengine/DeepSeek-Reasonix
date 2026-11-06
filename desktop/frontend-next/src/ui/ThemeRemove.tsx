import { useState } from "react";
import { t } from "../i18n";

/** Deleting a pack from the user's library, asked twice because the files go
 *  with it: the first press arms the button, leaving it or the second press
 *  settles. */
export function ThemeRemove({ name, onRemove }: { name: string; onRemove: () => Promise<void> }) {
  const [phase, setPhase] = useState<"idle" | "confirm" | "pending">("idle");
  const press = async () => {
    if (phase === "idle") return setPhase("confirm");
    setPhase("pending");
    try {
      await onRemove();
    } finally {
      setPhase("idle");
    }
  };
  return (
    <button
      type="button"
      className="btn sm pal-rm"
      data-action="theme.remove"
      data-confirming={phase === "confirm" ? "" : undefined}
      aria-label={t("look.delete_theme_name", { name })}
      disabled={phase === "pending"}
      aria-busy={phase === "pending" || undefined}
      onClick={() => void press()}
      onMouseLeave={() => setPhase((p) => (p === "confirm" ? "idle" : p))}
      onBlur={() => setPhase((p) => (p === "confirm" ? "idle" : p))}
    >
      {t(phase === "confirm" ? "common.confirm_delete" : "common.delete")}
    </button>
  );
}
