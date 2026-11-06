import type { ReactNode } from "react";
import { ApplyNote, ScopeTag } from "./Group";

export function SettingRow({ id, title, desc, more, children }: { id: string; title: string; desc?: string; more?: string; children?: ReactNode }) {
  return (
    <div className="srow" id={`set-${id}`} data-setting={id}>
      <div className="srow-meta">
        <span className="srow-name">{title}<ScopeTag id={id} /></span>
        {desc && <span className="srow-desc" title={more ?? desc}>{desc}</span>}
        <ApplyNote id={id} />
      </div>
      {children && <div className="srow-ctl">{children}</div>}
    </div>
  );
}

export function SegChoice<V extends string>({ label, value, options, disabled, action, onPick }: {
  label: string;
  value: V;
  options: readonly (readonly [V, string])[];
  disabled?: boolean;
  action: string;
  onPick: (next: V) => void;
}) {
  return (
    <div className="seg" data-text role="radiogroup" aria-label={label}>
      {options.map(([id, name]) => (
        <button key={id} role="radio" data-action={action} data-value={id} aria-checked={value === id} disabled={disabled} onClick={() => onPick(id)}>
          {name}
        </button>
      ))}
    </div>
  );
}

export function PillButton({ action, target, disabled, children, onClick }: { action: string; target?: string; disabled?: boolean; children: ReactNode; onClick: () => void }) {
  return (
    <button type="button" className="spill" data-action={action} data-target={target} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  );
}
