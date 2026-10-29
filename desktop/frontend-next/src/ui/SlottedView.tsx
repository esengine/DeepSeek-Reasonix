import { t } from "../i18n";
import type { ExtensionSurface, ExtensionViewNode } from "../port/wire";
import { ExtensionView } from "./cards/ExtensionView";
import { Picker } from "./Menu";
import { SLOTS, placement } from "./slots";

// A view plus the one control the host adds to it: where it goes. The handle is
// the host's, not the extension's — an extension asks for a place, and this is
// where the user overrules it. It stays hidden until the surface is hovered,
// because the common case is reading the thing, not moving it.
export function SlottedView({
  ext, body, assigned = {}, onAction, onMove, shown = true,
}: {
  ext: ExtensionSurface;
  // What a narrow place can draw of the view, when it cannot draw all of it.
  body?: ExtensionViewNode[];
  assigned?: Record<string, string>;
  onAction: (actionId: string) => void;
  onMove: (slot: string) => void;
  shown?: boolean;
}) {
  return (
    <div className="slotted">
      <ExtensionView body={body ?? ext.view?.body ?? []} onAction={onAction} />
      {shown && <Picker
        key={`${ext.pluginId}:${ext.surfaceId}`}
        label="⋯"
        className="movegrip"
        ariaLabel={t("移动 {id} 的界面", { id: ext.pluginId })}
        menuTitle={ext.pluginId}
        place="top"
        align="end"
        current={placement(ext, assigned)}
        items={[
          ...SLOTS.map((slot) => ({ value: slot.id, label: t(slot.label) })),
          { value: "", label: t("交还给插件"), plain: true, divide: true },
        ]}
        data-action={(slot) => slot ? "panel.move" : "panel.delegate-placement"}
        onPick={onMove}
      />}
    </div>
  );
}
