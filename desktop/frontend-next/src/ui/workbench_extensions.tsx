import type { ExtensionSurface } from "../port/wire";
import { Extensions } from "./panels/Extensions";
import { SlottedView } from "./SlottedView";
import { DEFAULT_SLOT } from "./slots";
import type { Surface } from "./workbench_tabs";
import type { FrameHost } from "./frame/FrameView";

/** WorkbenchExtensions is what a pane hands its workbench of the extension
 *  surfaces: the rail's views and panels, and the views that asked for a tab. */
export interface WorkbenchExtensions {
  rail: ExtensionSurface[];
  panels: ExtensionSurface[];
  tabs: ExtensionSurface[];
  assigned: Record<string, string>;
  frames?: FrameHost;
  onInvoke: (actionId: string) => void;
  onMove: (ext: ExtensionSurface, slot: string) => void;
}

/** extensionSurfaces lists the workbench tabs extensions own, after every tab
 *  a person or the agent opened, so a published view never becomes the one
 *  shown first. The rail is one tab, present only while it holds something. */
export function extensionSurfaces(ext: WorkbenchExtensions | undefined): Surface[] {
  if (!ext) return [];
  const tabs = ext.tabs.map((v) => ({ kind: "extension", ext: v }) as const);
  return ext.rail.length + ext.panels.length > 0 ? [...tabs, { kind: "extensions" }] : tabs;
}

/** releaseTabs takes a view's tab out of the workbench by moving the view to
 *  the rail, and returns the tabs left for the ordinary close. */
export function releaseTabs(targets: Surface[], ext: WorkbenchExtensions | undefined): Surface[] {
  for (const s of targets) if (s.kind === "extension") ext?.onMove(s.ext, DEFAULT_SLOT);
  return targets.filter((s) => s.kind !== "extension" && s.kind !== "extensions");
}

export function WorkbenchExtensionBody({ surface, ext, shown }: { surface: Surface; ext: WorkbenchExtensions; shown: boolean }) {
  if (surface.kind === "extensions") {
    return (
      <div className="workbench-ext">
        <Extensions panels={ext.panels} views={ext.rail} assigned={ext.assigned} frames={ext.frames} onInvoke={ext.onInvoke} onMove={ext.onMove} shown={shown} />
      </div>
    );
  }
  if (surface.kind !== "extension") return null;
  return (
    <div className="workbench-ext">
      <SlottedView ext={surface.ext} assigned={ext.assigned} frames={ext.frames} onAction={ext.onInvoke} onMove={(slot) => ext.onMove(surface.ext, slot)} shown={shown} />
    </div>
  );
}
