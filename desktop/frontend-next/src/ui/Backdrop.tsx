import type { Wallpaper } from "../port/look";
import { Sky } from "./Sky";
import { WallpaperVideo } from "./WallpaperVideo";

/** What is drawn behind the window besides the picture the stylesheet paints:
 *  an illustrated pack's sky, and a video wallpaper. */
export function Backdrop({ sky, wallpaper }: { sky: boolean; wallpaper?: Wallpaper }) {
  return (
    <>
      {sky && <Sky />}
      <WallpaperVideo wallpaper={wallpaper} />
    </>
  );
}
