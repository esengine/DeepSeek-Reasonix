const attachmentRefRe = /@(\.reasonix\/attachments\/[^\s]+)/g;
const referenceRefRe = /(^|\s)@([^\s]+)/g;
const trailingPunctuationRe = /[.,;!?)\]}，。；！？）】]+$/;

export interface DisplayAttachment {
  path: string;
  name: string;
  kind: "image" | "file" | "folder";
  source: "attachment" | "workspace";
  ext: string;
}

function splitTrailingPunctuation(token: string): { core: string; suffix: string } {
  const m = token.match(trailingPunctuationRe);
  if (!m || m.index === undefined) return { core: token, suffix: "" };
  return { core: token.slice(0, m.index), suffix: m[0] };
}

function baseName(path: string): string {
  const clean = path.replace(/\/+$/, "");
  const idx = clean.lastIndexOf("/");
  return idx >= 0 ? clean.slice(idx + 1) : clean;
}

function isImageAttachmentRef(path: string): boolean {
  const ext = attachmentExt(path);
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

function attachmentExt(path: string): string {
  const name = baseName(path).toLowerCase();
  const dot = name.lastIndexOf(".");
  return dot >= 0 ? name.slice(dot) : "";
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

export function parseAttachmentRefsForDisplay(text: string): { text: string; attachments: DisplayAttachment[] } {
  const attachments: DisplayAttachment[] = [];
  const cleaned = text
    .replace(referenceRefRe, (_full, lead: string, token: string) => {
      const { core, suffix } = splitTrailingPunctuation(token);
      if (!core || !isDisplayReference(core)) return _full;
      const name = baseName(core) || "attachment";
      const attachment = displayAttachment(core, name);
      attachments.push(attachment);
      return lead + suffix;
    })
    .replace(/[ \t]+([.,;!?)\]}，。；！？）】])/g, "$1")
    .replace(/[ \t]{2,}/g, " ")
    .trim();
  return { text: cleaned, attachments };
}

function isDisplayReference(path: string): boolean {
  if (path.startsWith(".reasonix/attachments/")) return true;
  if (path.endsWith("/")) return true;
  if (path.includes("/")) return true;
  return attachmentExt(path) !== "";
}

function displayAttachment(path: string, name: string): DisplayAttachment {
  if (path.startsWith(".reasonix/attachments/")) {
    const kind = isImageAttachmentRef(path) ? "image" : "file";
    return {
      path,
      name,
      kind,
      source: "attachment",
      ext: attachmentExt(path).replace(/^\./, "").toUpperCase(),
    };
  }
  const isDir = path.endsWith("/");
  return {
    path,
    name,
    kind: isDir ? "folder" : "file",
    source: "workspace",
    ext: isDir ? "" : attachmentExt(path).replace(/^\./, "").toUpperCase(),
  };
}
