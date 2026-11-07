import { t } from "../i18n";

/** The button that ends a running turn. It carries both the square and the
 *  pause mark; the stylesheet shows the pause one where the stop is a round
 *  button on a phone. */
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
        <svg viewBox="0 0 16 16">
          <rect className="ic-stop" x="4.8" y="4.8" width="6.4" height="6.4" rx="1.3" />
          <path className="ic-pause" d="M5.5 3.5v9M10.5 3.5v9" />
        </svg>
      </span>
      <span>{label}</span>
    </button>
  );
}
