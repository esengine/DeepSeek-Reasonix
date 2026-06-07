const attachmentRefRe = /@(\.reasonix\/attachments\/[^\s]+)/g;
const trailingPunctuationRe = /[.,;!?)\]}]+$/;

function splitTrailingPunctuation(token: string): { core: string; suffix: string } {
  const m = token.match(trailingPunctuationRe);
  if (!m || m.index === undefined) return { core: token, suffix: "" };
  return { core: token.slice(0, m.index), suffix: m[0] };
}

function baseName(path: string): string {
  const idx = path.lastIndexOf("/");
  return idx >= 0 ? path.slice(idx + 1) : path;
}

function isImageAttachmentRef(path: string): boolean {
  const ext = (() => {
    const name = baseName(path).toLowerCase();
    const dot = name.lastIndexOf(".");
    return dot >= 0 ? name.slice(dot) : "";
  })();
  switch (ext) {
    case ".png":
    case ".jpg":
    case ".jpeg":
    case ".gif":
    case ".webp":
    case ".bmp":
    case ".svg":
    case ".tif":
    case ".tiff":
      return true;
    default:
      return false;
  }
}

export function replaceAttachmentRefsForDisplay(text: string): string {
  return text.replace(attachmentRefRe, (_full, token: string) => {
    const { core, suffix } = splitTrailingPunctuation(token);
    if (!core) return _full;
    if (isImageAttachmentRef(core)) return `[image]${suffix}`;
    const name = baseName(core) || "attachment";
    return `[file:${name}]${suffix}`;
  });
}
