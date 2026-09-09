import { useEffect, useId, useRef, useState } from "react";
import { Info } from "lucide-react";
import { AnchoredPopover } from "./AnchoredPopover";
import { useProviderT } from "../lib/providerSettingsLocale";

export function ModelSettingHelp({ label, text }: { label: string; text: string }) {
  const t = useProviderT();
  const id = useId();
  const anchor = useRef<HTMLButtonElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pinned = useRef(false);
  const [open, setOpen] = useState(false);
  const clear = () => {
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = null;
  };
  const show = () => { clear(); setOpen(true); };
  const close = () => { clear(); pinned.current = false; setOpen(false); };
  const leave = () => {
    clear();
    if (!pinned.current) timer.current = setTimeout(() => setOpen(false), 150);
  };
  useEffect(() => () => { if (timer.current !== null) clearTimeout(timer.current); }, []);
  return <span className="model-setting-label">
    <span>{label}</span>
    <button ref={anchor} type="button" className="model-setting-help"
      aria-label={t("providerUI.modelHelpLabel", { name: label })}
      aria-expanded={open} aria-controls={open ? id : undefined} aria-describedby={open ? id : undefined}
      onMouseEnter={show} onMouseLeave={leave} onFocus={show} onBlur={leave}
      onClick={() => { if (pinned.current) close(); else { pinned.current = true; show(); } }}>
      <Info size={14} aria-hidden="true" />
    </button>
    <AnchoredPopover open={open} anchorRef={anchor} onClose={close} className="model-setting-help-popover" placement="bottom">
      <div id={id} role="tooltip" onMouseEnter={show} onMouseLeave={leave}>
        <strong>{label}</strong>
        {text.split("\n\n").map((paragraph, index) => <p key={index}>{paragraph}</p>)}
      </div>
    </AnchoredPopover>
  </span>;
}
