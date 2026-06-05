import { useState } from "react";
import { Check, ChevronsUpDown } from "lucide-react";
import { useT } from "../lib/i18n";
import type { EffortInfo } from "../lib/types";

export function EffortSwitcher({
  effort,
  disabled,
  onPick,
}: {
  effort?: EffortInfo;
  disabled: boolean;
  onPick: (level: string) => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  if (!effort?.supported || effort.levels.length === 0) return null;

  const current = effort.current || "auto";
  const pick = (level: string) => {
    setOpen(false);
    if (level !== current) onPick(level);
  };

  return (
    <div className="modelsw effortsw">
      <button
        className={`modelsw__trigger effortsw__trigger ${current !== "auto" ? "effortsw__trigger--explicit" : ""}`}
        disabled={disabled}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="modelsw__label">{t("status.effort", { level: current })}</span>
        <ChevronsUpDown size={11} />
      </button>
      {open && !disabled && (
        <>
          <div className="modelsw__backdrop" onClick={() => setOpen(false)} />
          <div className="modelsw__menu effortsw__menu" role="listbox">
            {effort.levels.map((level) => (
              <button
                key={level}
                role="option"
                aria-selected={level === current}
                className={`modelsw__item ${level === current ? "modelsw__item--current" : ""}`}
                onClick={() => pick(level)}
              >
                <span className="modelsw__model">{level}</span>
                {level === current && <Check size={13} className="modelsw__check" />}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
