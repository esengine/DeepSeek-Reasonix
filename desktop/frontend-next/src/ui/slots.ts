import type { ExtensionSurface, ExtensionViewNode } from "../port/wire";

export type Slot = "sidebar" | "composer-trailing" | "session-header" | "pane-tab";

// The places this frontend offers. The list is ours alone: a terminal frontend
// has different ones and neither has to tell the protocol, so adding a place
// here is a frontend change rather than a protocol version. An extension names
// a slot as a request; a name we do not have falls back to the rail, which is
// what lets the same extension show up in a window and in a terminal. Room is
// how many views a place holds before the rest go to the rail.
export const SLOTS: { id: Slot; label: string; room: number }[] = [
  { id: "sidebar", label: "扩展页签", room: Infinity },
  { id: "composer-trailing", label: "输入框上方", room: 2 },
  { id: "session-header", label: "会话标题栏", room: 3 },
  { id: "pane-tab", label: "单独的页签", room: 4 },
];

export const DEFAULT_SLOT: Slot = "sidebar";

// Names the protocol uses for places this frontend draws under its own name.
const ALIASES: Record<string, Slot> = {
  rail: "sidebar",
  "composer.above": "composer-trailing",
  above_prompt: "composer-trailing",
  "session.header": "session-header",
  "pane.tab": "pane-tab",
};

const known = (name = ""): Slot | undefined => {
  const slot = ALIASES[name] ?? name;
  return SLOTS.some((s) => s.id === slot) ? (slot as Slot) : undefined;
};

/** key is how a placement is addressed: one extension's two surfaces can sit
 *  in different places, so the surface is the unit, not the plugin. */
export function key(ext: ExtensionSurface): string {
  return `${ext.pluginId}:${ext.surfaceId}`;
}

/** placement resolves the three answers in order — what the user said, what the
 *  extension asked for, and where this frontend puts things it was told nothing
 *  about. A name none of us knows resolves to the next answer rather than nowhere. */
export function placement(ext: ExtensionSurface, assigned: Record<string, string>): Slot {
  return known(assigned[key(ext)]) ?? known(ext.view?.slot) ?? DEFAULT_SLOT;
}

// Code units rather than a collator: the order must not change with the locale.
const units = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0);
const byIdentity = (a: ExtensionSurface, b: ExtensionSurface) =>
  units(a.pluginId, b.pluginId) || units(a.surfaceId, b.surfaceId);

/** arrange places every view of a pane. The order is the surfaces' identities,
 *  never their arrival, so a republish cannot move anything. */
export function arrange(views: ExtensionSurface[], assigned: Record<string, string>): Record<Slot, ExtensionSurface[]> {
  const out = Object.fromEntries(SLOTS.map((s) => [s.id, []])) as unknown as Record<Slot, ExtensionSurface[]>;
  for (const v of [...views].sort(byIdentity)) {
    const want = placement(v, assigned);
    const room = SLOTS.find((s) => s.id === want)!.room;
    out[out[want].length < room ? want : DEFAULT_SLOT].push(v);
  }
  return out;
}

const HEADER_KINDS = new Set<ExtensionViewNode["kind"]>(["text", "pip", "meter", "button"]);

/** headerBody is what a one-line place can draw of a view: the inline kinds,
 *  lifted out of their rows and stacks, and every other node as its text. */
export function headerBody(body: ExtensionViewNode[]): ExtensionViewNode[] {
  return body.flatMap((n): ExtensionViewNode[] => {
    if (n.kind === "row" || n.kind === "stack") return headerBody(n.children ?? []);
    if (HEADER_KINDS.has(n.kind)) return [n];
    const value = n.kind === "kv" ? [n.key, n.value].filter(Boolean).join(": ") : n.value || n.label || "";
    return value ? [{ kind: "text", value, ...(n.tone ? { tone: n.tone } : {}) }] : [];
  });
}
