import type { ExtensionSurface } from "../port/wire";

// The places this frontend offers. The list is ours alone: a terminal frontend
// has different ones and neither has to tell the protocol, so adding a place
// here is a frontend change rather than a protocol version. An extension names
// a slot as a request; a name we do not have falls back to the rail, which is
// what lets the same extension show up in a window and in a terminal.
export const SLOTS: { id: string; label: string }[] = [
  { id: "sidebar", label: "侧栏" },
  { id: "composer-trailing", label: "输入框上方" },
];

export const DEFAULT_SLOT = "sidebar";

/** key is how a placement is addressed: one extension's two surfaces can sit
 *  in different places, so the surface is the unit, not the plugin. */
export function key(ext: ExtensionSurface): string {
  return `${ext.pluginId}:${ext.surfaceId}`;
}

/** placement resolves the three answers in order — what the user said, what the
 *  extension asked for, and where this frontend puts things it was told nothing
 *  about. A name none of us knows resolves to the default rather than nowhere. */
export function placement(ext: ExtensionSurface, assigned: Record<string, string>): string {
  const candidate = assigned[key(ext)] || ext.view?.slot || DEFAULT_SLOT;
  return SLOTS.some((s) => s.id === candidate) ? candidate : DEFAULT_SLOT;
}
