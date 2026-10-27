import { t } from "../i18n";
import type { ExtensionSurface, ExtensionViewNode } from "../port/wire";
import { ExtensionView } from "./cards/ExtensionView";
import { Picker } from "./Menu";
import { SLOTS, fits, placement } from "./slots";
import { FrameView, type FrameHost } from "./frame/FrameView";

export function SlottedView({ ext, body, assigned = {}, frames, onAction, onMove, shown = true }: {
  ext: ExtensionSurface;
  body?: ExtensionViewNode[];
  assigned?: Record<string, string>;
  frames?: FrameHost;
  onAction: (actionId: string) => void;
  onMove: (slot: string) => void;
  shown?: boolean;
}) {
  const here = placement(ext, assigned);
  return (
    <div className="slotted">
      {ext.kind !== "frame" ? (
        <ExtensionView body={body ?? ext.view?.body ?? []} onAction={onAction} />
      ) : frames ? (
        <FrameView ext={ext} slot={here} host={frames} />
      ) : <p className="xframe-fb">{ext.frame?.fallback}</p>}
      {shown && <Picker
        key={`${ext.pluginId}:${ext.surfaceId}`}
        label="⋯"
        className="movegrip"
        ariaLabel={t("移动 {id} 的界面", { id: ext.pluginId })}
        menuTitle={ext.pluginId}
        place="top"
        align="end"
        current={here}
        items={[
          ...SLOTS.filter((slot) => fits(slot.id, ext)).map((slot) => ({ value: slot.id, label: t(slot.label) })),
          { value: "", label: t("交还给插件"), plain: true, divide: true },
        ]}
        data-action={(slot) => slot ? "panel.move" : "panel.delegate-placement"}
        onPick={onMove}
      />}
    </div>
  );
}
