import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

/** The side question's switch, drawn beside attach. Whether the panel is open is
 *  the pane's to say; this reports the press, draws the panel it was handed into
 *  the composer it sits in, and takes the focus back when that panel closes. */
export function SideAskButton({ open, onToggle, panel }: { open: boolean; onToggle: () => void; panel?: ReactNode }) {
  const tip = useId();
  const button = useRef<HTMLButtonElement>(null);
  const was = useRef(open);
  const [host, setHost] = useState<Element | null>(null);
  useEffect(() => setHost(button.current?.closest(".compose") ?? null), []);
  useEffect(() => {
    if (was.current && !open && (!document.activeElement || document.activeElement === document.body)) button.current?.focus();
    was.current = open;
  }, [open]);
  return (
    <>
      <button
        ref={button}
        type="button"
        className="mode plain attach studio-attach"
        data-action="side.toggle"
        aria-label={t("composer.side_question")}
        aria-describedby={tip}
        aria-pressed={open}
        onClick={onToggle}
      >
        <StudioIcon name="chat" />
        <span className="studio-control-tip" id={tip} role="tooltip">
          <b>{t("composer.side_question")}</b>
          <span>{t("composer.read_only_not_in_the")}</span>
        </span>
      </button>
      {open && host && panel ? createPortal(panel, host) : null}
    </>
  );
}
