// Two screenshots pasted in a row are one filename apart, which is the one
// thing the chip has to tell them by. The preview comes off the blob that was
// just attached — the kernel keeps the bytes, this keeps a handle to look at.
export function previewURL(blob: Blob): string | undefined {
  try {
    return URL.createObjectURL(blob);
  } catch {
    return undefined;
  }
}

// A dropped file has no preview to stand behind: the host named it, it was
// never read. Its kind fills the square, because a blank one reads as an image
// that failed to load.
export function kindOf(path: string): string {
  const name = nameOf(path);
  const dot = name.lastIndexOf(".");
  return dot > 0 ? name.slice(dot + 1).toUpperCase().slice(0, 4) : "FILE";
}

export function nameOf(path: string): string {
  return path.split(/[\\/]/).pop() ?? path;
}
