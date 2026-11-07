import { useState, type CSSProperties } from "react";
import type { MenuItem } from "./Menu";

/** The reasoning levels as one slider. Moving it only previews a level; letting
 *  go chooses it, so dragging across four steps asks the model once, not four
 *  times. */
export function EffortSlider({ name, model, levels, current, onPick }: {
  name: string;
  model?: string;
  levels: MenuItem[];
  current: string;
  onPick: (value: string) => void;
}) {
  const at = Math.max(0, levels.findIndex((level) => level.value === current));
  const [preview, setPreview] = useState<number | null>(null);
  const shown = preview ?? at;
  const commit = () => {
    if (preview !== null && preview !== at) onPick(levels[preview].value);
    setPreview(null);
  };
  const last = levels.length - 1;
  return (
    <div className="effort-slider" style={{ "--p": shown / last } as CSSProperties}>
      <div className="es-head">
        <span className="es-name">{levels[shown].label}</span>
        {model && <span className="es-model">{model}</span>}
      </div>
      <input
        type="range" min={0} max={last} step={1} value={shown}
        aria-label={name} aria-valuetext={levels[shown].label}
        onChange={(e) => setPreview(Number(e.target.value))}
        onPointerUp={commit} onKeyUp={commit} onBlur={commit}
      />
      <div className="es-ends" aria-hidden="true">
        <span>{levels[0].label}</span>
        <span>{levels[last].label}</span>
      </div>
    </div>
  );
}
