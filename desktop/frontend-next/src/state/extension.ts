import type { ExtensionSurface } from "../port/wire";
import type { SessionState } from "./session_types";
import { nextId } from "./ids";

export function foldExtension(s: SessionState, ext: ExtensionSurface): SessionState {
  const same = (p: ExtensionSurface) => p.pluginId === ext.pluginId && p.surfaceId === ext.surfaceId;
  if (ext.code) {
    const at = s.items.findIndex(i => i.t === "extension" && same(i.ext));
    const items = s.items.slice();
    const item = { t: "extension" as const, id: at < 0 ? nextId() : items[at].id, ext };
    if (at < 0) items.push(item);
    else items[at] = item;
    return {
      ...s, items,
      panels: s.panels.filter(p => !same(p)),
      views: s.views.filter(p => !same(p)),
      takeovers: Object.fromEntries(Object.entries(s.takeovers).filter(([, p]) => !same(p))),
    };
  }
  if (s.items.some(i => i.t === "extension" && same(i.ext) && i.ext.code)) {
    s = { ...s, items: s.items.filter(i => !(i.t === "extension" && same(i.ext) && i.ext.code)) };
  }
  if (ext.kind === "panel") {
    const at = s.panels.findIndex(same);
    const panels = s.panels.slice();
    if (at < 0) panels.push(ext);
    else panels[at] = ext;
    return { ...s, panels };
  }
  if (ext.kind === "view" && ext.view?.anchor) {
    return { ...s, takeovers: { ...s.takeovers, [ext.view.anchor]: ext } };
  }
  if (ext.kind === "view" || ext.kind === "frame") {
    const at = s.views.findIndex(same);
    const views = s.views.slice();
    if (at < 0) views.push(ext);
    else views[at] = ext;
    return { ...s, views };
  }
  const at = s.items.findIndex(i => i.t === "extension" && same(i.ext));
  const items = s.items.slice();
  if (at < 0) items.push({ t: "extension", id: nextId(), ext });
  else items[at] = { t: "extension", id: items[at].id, ext };
  return { ...s, items };
}
