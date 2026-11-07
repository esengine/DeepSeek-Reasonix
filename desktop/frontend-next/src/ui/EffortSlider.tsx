import { useEffect, useState, type CSSProperties, type KeyboardEvent } from "react";
import type { MenuItem } from "./Menu";
import { RMark } from "./RMark";

/** The reasoning levels as one slider. It moves continuously under the finger
 *  and settles on the nearest level when let go; moving it only previews, and
 *  letting go chooses, so dragging across four levels asks the model once. */
export function EffortSlider({ name, model, levels, current, onPick }: {
  name: string;
  model?: string;
  levels: MenuItem[];
  current: string;
  onPick: (value: string) => void;
}) {
  const last = levels.length - 1;
  const clamp = (n: number) => Math.min(last, Math.max(0, n));
  const at = Math.max(0, levels.findIndex((level) => level.value === current));
  const [value, setValue] = useState<number | null>(null);
  const [held, setHeld] = useState(false);
  const pos = value ?? at;
  const near = clamp(Math.round(pos));

  // Once the level it settled on is the current one, the position is the
  // current one's again, and nothing is left to remember.
  useEffect(() => {
    if (!held && value !== null && clamp(Math.round(value)) === at) setValue(null);
  });

  const release = () => {
    setHeld(false);
    if (value === null) return;
    const to = clamp(Math.round(value));
    setValue(to);
    if (to !== at) onPick(levels[to].value);
  };
  const step = (e: KeyboardEvent<HTMLInputElement>) => {
    const to = e.key === "ArrowRight" || e.key === "ArrowUp" ? near + 1
      : e.key === "ArrowLeft" || e.key === "ArrowDown" ? near - 1
      : e.key === "Home" ? 0
      : e.key === "End" ? last
      : null;
    if (to === null) return;
    e.preventDefault();
    setValue(clamp(to));
  };

  return (
    <div className="effort-slider">
      <div className="es-head">
        <span className="es-name">{levels[near].label}</span>
        {model && <span className="es-model">{model}</span>}
      </div>
      <div className="es-track" data-drag={held ? "" : undefined} style={{ "--p": pos / last } as CSSProperties}>
        <span className="es-line" aria-hidden="true" />
        <span className="es-fill" aria-hidden="true" />
        {levels.map((level, i) => (
          <i key={level.value} className="es-tick" aria-hidden="true" data-on={i <= near ? "" : undefined} style={{ "--k": i / last } as CSSProperties} />
        ))}
        <span className="es-thumb" aria-hidden="true"><RMark className="es-r" /></span>
        <input
          type="range" min={0} max={last} step="any" value={pos}
          aria-label={name} aria-valuetext={levels[near].label}
          data-action-change="reasoning.effort-preview"
          data-action-pointerdown="reasoning.effort-preview"
          data-action-pointerup="reasoning.effort"
          data-action-pointercancel="reasoning.effort"
          data-action-keydown="reasoning.effort-preview"
          data-action-keyup="reasoning.effort"
          onChange={(e) => setValue(Number(e.target.value))}
          onPointerDown={() => setHeld(true)}
          onPointerUp={release} onPointerCancel={release}
          onKeyDown={step} onKeyUp={release} onBlur={release}
        />
      </div>
      <div className="es-marks" aria-hidden="true">
        {levels.map((level, i) => (
          <span key={level.value} className="es-mark" data-on={i === near ? "" : undefined} style={{ "--k": i / last } as CSSProperties}>{level.label}</span>
        ))}
      </div>
    </div>
  );
}
