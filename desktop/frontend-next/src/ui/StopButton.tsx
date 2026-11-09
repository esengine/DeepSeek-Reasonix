import { Square } from "lucide-react";
import { t } from "../i18n";

export function StopButton({ stopping, onStop }: { stopping: boolean; onStop: () => void }) {
  const label = t(stopping ? "composer.stopping" : "composer.stop");
  return (
    <button
      className="btn stop"
      data-primary
      data-action="session.stop"
      data-pending={stopping ? "" : undefined}
      aria-label={label}
      disabled={stopping}
      aria-busy={stopping}
      onClick={onStop}
    >
      <span className="ic" aria-hidden="true">
        <Square fill="currentColor" strokeWidth={0} />
      </span>
      <span>{label}</span>
    </button>
  );
}
