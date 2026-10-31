// The window crops a photo before it leaves: a 160px square from the middle
// of the picture, compressed until it fits what the kernel keeps for one.
const SIDE = 160;
const MAX_CHARS = 150_000;
const ACCEPTED = ["image/png", "image/jpeg", "image/webp", "image/gif"];
const MAX_SOURCE_BYTES = 25 << 20;

export class PhotoError extends Error {
  constructor(readonly reason: "type" | "size") {
    super(reason);
  }
}

export async function cropPhoto(file: File): Promise<string> {
  if (!ACCEPTED.includes(file.type)) throw new PhotoError("type");
  if (file.size > MAX_SOURCE_BYTES) throw new PhotoError("size");
  const bitmap = await createImageBitmap(file);
  const edge = Math.min(bitmap.width, bitmap.height);
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = SIDE;
  const g = canvas.getContext("2d");
  if (!g) throw new PhotoError("type");
  g.drawImage(bitmap, (bitmap.width - edge) / 2, (bitmap.height - edge) / 2, edge, edge, 0, 0, SIDE, SIDE);
  bitmap.close();
  for (const quality of [0.9, 0.75, 0.6]) {
    const url = canvas.toDataURL("image/webp", quality);
    if (url.startsWith("data:image/webp") && url.length <= MAX_CHARS) return url;
  }
  const png = canvas.toDataURL("image/png");
  if (png.length <= MAX_CHARS) return png;
  throw new PhotoError("size");
}
