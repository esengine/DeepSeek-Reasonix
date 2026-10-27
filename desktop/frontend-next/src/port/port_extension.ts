import type { ExtensionSurfaceReplay } from "./surface_replay";

/** The page's half of extension surfaces: they arrive on the event stream, and
 *  these carry the user's answers back and say where a frame document lives. */
export interface ExtensionUIPort {
  extensionSurfaces(): Promise<ExtensionSurfaceReplay>;
  invokeExtensionAction(name: string, args?: Record<string, string>): Promise<string>;
  submitExtensionForm(pluginId: string, surfaceId: string, values: Record<string, unknown>): Promise<void>;
  /** extensionFrameUrl is where this runtime serves one frame document. */
  extensionFrameUrl(pluginId: string, frameId: string, generation: number): string;
}
