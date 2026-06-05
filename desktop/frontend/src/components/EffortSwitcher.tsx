import { useRef, useState } from "react";
import { Check, ChevronsUpDown, Gauge } from "lucide-react";
import { asArray } from "../lib/array";
import { useT } from "../lib/i18n";
import type { EffortInfo } from "../lib/types";
import { AnchoredPopover } from "./AnchoredPopover";

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
  const triggerRef = useRef<HTMLButtonElement>(null);
  const levels = asArray(effort?.levels);
  if (!effort?.supported || levels.length === 0) return null;

  const current = effort.current || "auto";
  const pick = (level: string) => {
    setOpen(false);
    if (level !== current) onPick(level);
  };

  return (
    <div className="modelsw effortsw">
      <button
        ref={triggerRef}
        type="button"
        className={`modelsw__trigger effortsw__trigger ${current !== "auto" ? "effortsw__trigger--explicit" : ""}`}
        disabled={disabled}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <Gauge size={13} className="modelsw__kind" />
        <span className="modelsw__label">{t("status.effort", { level: current })}</span>
        <ChevronsUpDown size={11} />
      </button>
      <AnchoredPopover
        open={open && !disabled}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className="modelsw__menu modelsw__menu--portal effortsw__menu"
        align="end"
      >
        <div role="listbox">
          {levels.map((level) => (
            <button
              key={level}
              type="button"
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
      </AnchoredPopover>
    </div>
  );
}
