import { copyText } from "./CopyButton";

// What "copy all" puts on the clipboard: the message as text, its pictures as
// pixels, and the stored paths as a manifest the paste side can resolve. The
// manifest is the only place a path travels — the text never carries one.
export const CLIP_MANIFEST = "application/x-reasonix-clipboard+json";

const MARKER_OPEN = "<!--reasonix:clipboard";
const MARKER = /^<!--reasonix:clipboard\s*([\s\S]*?)\s*-->\s*$/;

// One stored picture a copy carried: where the workspace keeps it. The path is
// the whole record — whether it is a picture is the host's answer on paste, not
// something this side re-decides from a file name.
export interface ClipImage {
  path: string;
}

export interface ClipContent {
  text: string;
  images: ClipImage[];
}

interface ClipPayload extends ClipContent {
  v: 1;
}

/** clipText is the text/plain half: the words, then one comment naming what
 *  they carried. A reader that is not Reasonix sees a comment; a Reasonix paste
 *  sees the pictures back, and never a path dressed as prose. */
export function clipText({ text, images }: ClipContent): string {
  if (images.length === 0) return text;
  const payload: ClipPayload = { v: 1, text, images };
  const body = text.trimEnd();
  return `${body}${body ? "\n\n" : ""}${MARKER_OPEN} ${JSON.stringify(payload)}-->`;
}

/** readClipText takes the words back out of a copied string, with the pictures
 *  the marker named. Anything it cannot parse is returned untouched — a paste
 *  must never lose text to a format it did not write. */
export function readClipText(raw: string): ClipContent {
  const trimmed = raw.trimEnd();
  const at = trimmed.lastIndexOf(MARKER_OPEN);
  if (at < 0) return { text: raw, images: [] };
  const match = MARKER.exec(trimmed.slice(at));
  if (!match) return { text: raw, images: [] };
  const payload = parsePayload(match[1]);
  if (!payload) return { text: raw, images: [] };
  return { text: trimmed.slice(0, at).trimEnd(), images: payload.images };
}

/** pasteContent reads whichever half the browser handed over: our own manifest
 *  when the copy came from Reasonix, the text marker otherwise. Reading the
 *  manifest first is what keeps a re-paste from uploading the same pixels a
 *  second time as a new attachment. */
export function pasteContent(data: DataTransfer): ClipContent {
  const manifest = data.getData(CLIP_MANIFEST);
  if (manifest) {
    const payload = parsePayload(manifest);
    if (payload) return { text: payload.text, images: payload.images };
  }
  return readClipText(data.getData("text/plain"));
}

/** copyAll writes the message and its pictures together. One picture travels as
 *  pixels — a clipboard item holds one blob per type — and every picture travels
 *  as its stored path, which is what a paste in the same workspace restores. */
export async function copyAll({ text, images }: { text: string; images: { path: string; url: string }[] }) {
  if (images.length === 0) {
    // Nothing to restore, so nothing names the message but its words.
    await copyText(text);
    return;
  }
  const read = await Promise.all(images.map(async (image) => {
    try {
      const response = await fetch(image.url);
      if (!response.ok) throw new Error(`image ${response.status}`);
      const blob = await response.blob();
      // The response header is what the host said the bytes are; a blob built
      // by a test double can lose its own type, and an unnamed picture still
      // travels by its path.
      const header = (response.headers.get("content-type") ?? "").split(";")[0].trim();
      const type = header || blob.type;
      return { path: image.path, blob, type: type.startsWith("image/") ? type : "" };
    } catch {
      // A picture this window cannot read still names itself: the path is what
      // the paste side resolves, and the words are worth more than the pixels.
      return { path: image.path, blob: undefined, type: "" };
    }
  }));
  const named = read.map(({ path }) => ({ path }));
  const plain = clipText({ text, images: named });
  if (!navigator.clipboard?.write || typeof ClipboardItem === "undefined") {
    await copyText(plain);
    return;
  }
  const first = read.find((image) => image.type);
  const item: Record<string, Blob> = {
    "text/plain": new Blob([plain], { type: "text/plain" }),
    [CLIP_MANIFEST]: new Blob([JSON.stringify({ v: 1, text, images: named })], { type: CLIP_MANIFEST }),
  };
  // One blob per type: a second picture would overwrite the first. It travels
  // by path instead, so only one picture's pixels ride along.
  if (first?.blob) item[first.type] = first.blob;
  try {
    await navigator.clipboard.write([new ClipboardItem(item)]);
  } catch {
    // A host that refuses the rich write still accepts the text, which keeps
    // every path restorable even when the pixels cannot travel.
    await copyText(plain);
  }
}

function parsePayload(raw: string): ClipPayload | null {
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!value || typeof value !== "object") return null;
  const payload = value as Partial<ClipPayload>;
  if (payload.v !== 1 || !Array.isArray(payload.images)) return null;
  const images = payload.images.flatMap((image) => {
    if (!image || typeof image.path !== "string" || !image.path.trim()) return [];
    return [{ path: image.path.trim() }];
  });
  return { v: 1, text: typeof payload.text === "string" ? payload.text : "", images };
}
