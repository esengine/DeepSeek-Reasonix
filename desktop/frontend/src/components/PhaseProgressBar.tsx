import { ProcessStatusIcon } from "./ProcessCard";

interface PhaseProgressBarProps {
  phases: { text: string; state: "done" | "running" | "waiting" | "failed" }[];
  collapsed?: boolean;
}

export function PhaseProgressBar({ phases, collapsed }: PhaseProgressBarProps) {
  if (phases.length === 0) return null;

  const visible = collapsed ? phases.slice(0, 5) : phases;
  const total = phases.length;

  return (
    <div className="phase-progress" role="progressbar" aria-valuenow={phases.filter(p => p.state === "done").length} aria-valuemax={total}>
      <div className="phase-progress__track">
        {visible.map((p, i) => (
          <div key={i} className={`phase-progress__step phase-progress__step--${p.state}`}>
            <div className="phase-progress__dot">
              <ProcessStatusIcon state={p.state} label={p.text} />
            </div>
            <span className="phase-progress__label">{p.text}</span>
          </div>
        ))}
        {collapsed && total > 5 && (
          <div className="phase-progress__step phase-progress__step--waiting">
            <span className="phase-progress__more">+{total - 5}</span>
          </div>
        )}
      </div>
    </div>
  );
}
