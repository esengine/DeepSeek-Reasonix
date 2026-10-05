import { useRef, useState } from "react";
import { t } from "../i18n";
import type { ExtensionSurface } from "../port/wire";
import { ExtensionView } from "./cards/ExtensionView";
import { SLOTS, placement } from "./slots";

// A view plus the one control the host adds to it: where it goes. The handle is
// the host's, not the extension's — an extension asks for a place, and this is
// where the user overrules it. It stays hidden until the surface is hovered,
// because the common case is reading the thing, not moving it.
export function SlottedView({
  ext, assigned = {}, onAction, onMove,
}: {
  ext: ExtensionSurface;
  assigned?: Record<string, string>;
  onAction: (actionId: string) => void;
  onMove: (slot: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const grip = useRef<HTMLButtonElement>(null);
  const here = placement(ext, assigned);
  // Hiding the menu takes the row that had focus with it, and the caret drops
  // to the document — so closing hands it back the way Menu does.
  const close = (then: () => void) => { setOpen(false); grip.current?.focus(); then(); };
  return (
    <div className="slotted">
      <ExtensionView body={ext.view?.body ?? []} onAction={onAction} />
      <button
        ref={grip}
        className="movegrip"
        aria-label={t("移动 {id} 的界面", { id: ext.pluginId })}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        ⋯
      </button>
      {/* hidden rather than conditional: the menu has to stay in the DOM to have
          anything to animate on the way out. */}
      <div className="movemenu" role="menu" hidden={!open}>
        <span className="who">{ext.pluginId}</span>
        {SLOTS.map((slot) => (
          <button
            key={slot.id}
            role="menuitemradio"
            data-action="panel.move"
            aria-checked={here === slot.id}
            onClick={() => close(() => onMove(slot.id))}
          >
            {slot.label}
          </button>
        ))}
        {/* Clearing hands the choice back to the extension rather than
            hiding the surface: it is a different act from moving it. */}
        <button role="menuitem" data-action="panel.delegate-placement" onClick={() => close(() => onMove(""))}>
          {t("交还给插件")}
        </button>
      </div>
    </div>
  );
}
