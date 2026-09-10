import { useRef, useState, type ReactNode } from "react";
import { Check, ChevronDown } from "lucide-react";
import { AnchoredPopover } from "./AnchoredPopover";

export function ComposerChoice({ label, ariaLabel, icon, showChevron, value, options, disabled, onPick, tone }: {
  label: string;
  ariaLabel?: string;
  showChevron?: boolean;
  icon?: ReactNode;
  value: string;
  options: { value: string; label: string; description?: string; icon?: ReactNode; title?: string }[];
  disabled?: boolean;
  onPick: (value: string) => void;
  tone?: string;
}) {
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLButtonElement>(null);
  return <>
    <button ref={anchor} type="button" className={`composer-choice ${tone || ""}`} disabled={disabled}
      aria-label={ariaLabel || label} aria-haspopup="menu" aria-expanded={open && !disabled} onClick={() => setOpen(!open)}>
      {icon}<span>{label}</span>{(showChevron ?? !icon) && <ChevronDown size={12} />}
    </button>
    <AnchoredPopover open={open && !disabled} anchorRef={anchor} onClose={() => setOpen(false)} className="composer-access-menu composer-menu-surface" align="start">
      <div role="menu" aria-label={ariaLabel || label}>
        {options.map(option => <button key={option.value} type="button" role="menuitemradio"
          data-value={option.value} title={option.title}
          aria-checked={value === option.value} className={`composer-access-menu__item${value === option.value ? " composer-access-menu__item--active" : ""}`}
          onClick={() => { setOpen(false); onPick(option.value); }}>
          {option.icon}<span className="composer-access-menu__copy"><span className="composer-access-menu__title">{option.label}</span>
            {option.description && <span className="composer-access-menu__desc">{option.description}</span>}</span>
          {value === option.value && <Check size={14} />}
        </button>)}
      </div>
    </AnchoredPopover>
  </>;
}
