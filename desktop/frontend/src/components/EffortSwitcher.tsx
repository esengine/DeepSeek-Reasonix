import { useCallback, useEffect, useRef, useState } from "react";
import { Check, ChevronsUpDown, Gauge } from "lucide-react";
import { asArray } from "../lib/array";
import type { EffortInfo } from "../lib/types";
import { useT, type DictKey } from "../lib/i18n";
import { ANCHORED_POPOVER_CLOSE_MS, AnchoredPopover } from "./AnchoredPopover";

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
  const [closing, setClosing] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const closeTimerRef = useRef<number | null>(null);
  const levels = asArray(effort?.levels);
  const current = effort?.current || "auto";
  // 思考模式级别按界面语言显示（如 auto→自动、high→高、max→最高）；
  // 未定义键时回退原始英文值（兼容自定义级别）。
  const effortLabel = (level: string) => {
    const localized = t(`effort.${level}` as DictKey);
    return localized && localized !== `effort.${level}` ? localized : level;
  };

  const clearCloseTimer = useCallback(() => {
    if (closeTimerRef.current === null) return;
    window.clearTimeout(closeTimerRef.current);
    closeTimerRef.current = null;
  }, []);

  const openMenu = useCallback(() => {
    clearCloseTimer();
    setClosing(false);
    setOpen(true);
  }, [clearCloseTimer]);

  const closeMenu = useCallback((afterClose?: () => void) => {
    clearCloseTimer();
    setClosing(true);
    window.requestAnimationFrame(() => setOpen(false));
    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    closeTimerRef.current = window.setTimeout(() => {
      closeTimerRef.current = null;
      setClosing(false);
      afterClose?.();
    }, reduceMotion ? 0 : ANCHORED_POPOVER_CLOSE_MS);
  }, [clearCloseTimer]);

  useEffect(() => () => clearCloseTimer(), [clearCloseTimer]);

  const pick = (level: string) => {
    closeMenu(() => {
      if (level !== current) onPick(level);
    });
  };

  if (!effort?.supported || levels.length === 0) return null;

  return (
    <div className="modelsw effortsw">
      <button
        ref={triggerRef}
        type="button"
        className={`modelsw__trigger effortsw__trigger ${current !== "auto" ? "effortsw__trigger--explicit" : ""}`}
        disabled={disabled}
        aria-expanded={open && !closing}
        onClick={() => (open || closing ? closeMenu() : openMenu())}
      >
        <Gauge size={14} className="modelsw__kind" />
        <span className="modelsw__label">{effortLabel(current)}</span>
        <ChevronsUpDown size={11} />
      </button>
      <AnchoredPopover
        open={open && !disabled}
        closing={closing}
        anchorRef={triggerRef}
        onClose={() => closeMenu()}
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
              <span className="modelsw__model">{effortLabel(level)}</span>
              {level === current && <Check size={13} className="modelsw__check" />}
            </button>
          ))}
        </div>
      </AnchoredPopover>
    </div>
  );
}
