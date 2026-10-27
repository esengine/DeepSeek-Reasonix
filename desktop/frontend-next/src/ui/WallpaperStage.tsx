import type { Appearance } from "../port/port";
import { Backdrop } from "./Backdrop";
import { WallpaperLayers } from "./WallpaperLayers";
import { WallpaperScene } from "./WallpaperScene";
import { WallpaperVolume } from "./WallpaperVolume";

/** Everything behind the window: the pack's sky, a picture or video
 *  wallpaper, and a Wallpaper Engine scene's sound and controls. */
export function WallpaperStage({ sky, look, onLook }: { sky: boolean; look: Appearance; onLook: (look: Appearance) => void }) {
  return (
    <>
      <Backdrop sky={sky} wallpaper={look.wallpaper} />
      <WallpaperLayers wallpaper={look.wallpaper} />
      <WallpaperScene wallpaper={look.wallpaper} />
      <WallpaperVolume look={look} onLook={onLook} />
    </>
  );
}
