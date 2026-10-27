import { useEffect, useState } from "react";

/** useCrop answers which focal axis has room to move. A `cover` picture wider
 *  than its box is cropped left and right and nowhere else, so the other axis
 *  cannot move a pixel however far its slider is dragged. It measures the
 *  preview, which is drawn at the window's shape, rather than assuming one.
 *  Both axes count as free until the image decodes: never disable on a guess. */
export function useCrop(url: string | undefined, kind: "image" | "video" | "scene" | undefined, box: HTMLElement | null) {
  const [image, setImage] = useState(0);
  const [frame, setFrame] = useState(0);

  useEffect(() => {
    setImage(0);
    if (!url) return;
    if (kind === "video") {
      const clip = document.createElement("video");
      clip.preload = "metadata";
      clip.onloadedmetadata = () => setImage(clip.videoHeight ? clip.videoWidth / clip.videoHeight : 0);
      clip.src = url;
      return () => {
        clip.onloadedmetadata = null;
        clip.removeAttribute("src");
        clip.load();
      };
    }
    const img = new Image();
    img.onload = () => setImage(img.naturalHeight ? img.naturalWidth / img.naturalHeight : 0);
    img.src = url;
    return () => {
      img.onload = null;
    };
  }, [url, kind]);

  useEffect(() => {
    if (!box) return;
    const read = () => setFrame(box.clientHeight ? box.clientWidth / box.clientHeight : 0);
    read();
    const ro = new ResizeObserver(read);
    ro.observe(box);
    return () => ro.disconnect();
  }, [box]);

  // Ratios a pixel apart leave no room either way, hence the 1% of slack.
  const known = image > 0 && frame > 0;
  return { known, x: !known || image > frame * 1.01, y: !known || image < frame / 1.01 };
}
