const attachmentRefRe = /@(\.reasonix\/attachments\/[^\s]+)/g;
const namedAttachmentRefRe = /(^|\s)@\[([^\]\r\n]+)\]\(([^)\s]+)\)/g;
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

export function baseName(path: string): string {
  const clean = path.replace(/[\\/]+$/, "");
  const slash = clean.lastIndexOf("/");
  const backslash = clean.lastIndexOf("\\");
  const idx = Math.max(slash, backslash);
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

function cleanDisplayName(name: string): string {
  return name.replace(/\\([\[\]])/g, "$1").replace(/\s+/g, " ").trim();
}

export function replaceAttachmentRefsForDisplay(text: string): string {
  return text
    .replace(namedAttachmentRefRe, (_full, lead: string, label: string, token: string) => {
      const { core, suffix } = splitTrailingPunctuation(token);
      if (!core || !isDisplayReference(core)) return _full;
      const name = cleanDisplayName(label) || baseName(core) || "attachment";
      return `${lead}${isImageAttachmentRef(core) ? "[image]" : `[file:${name}]`}${suffix}`;
    })
    .replace(attachmentRefRe, (_full, token: string) => {
      const { core, suffix } = splitTrailingPunctuation(token);
      if (!core) return _full;
      if (isImageAttachmentRef(core)) return `[image]${suffix}`;
      const name = baseName(core) || "attachment";
      return `[file:${name}]${suffix}`;
    });
}

export function restoreAttachmentRefsForSubmit(text: string): string {
  return text.replace(namedAttachmentRefRe, (_full, lead: string, _label: string, token: string) => {
    const { core, suffix } = splitTrailingPunctuation(token);
    if (!core || !isDisplayReference(core)) return _full;
    return `${lead}@${core}${suffix}`;
  });
}

function displayRefName(name: string): string {
  return name.replace(/[\[\]\(\)\r\n]+/g, " ").replace(/\s+/g, " ").trim() || "attachment";
}

export function formatAttachmentRefForDisplay(attachment: Pick<DisplayAttachment, "path" | "name" | "source">): string {
  if (attachment.source === "attachment") {
    return `@[${displayRefName(attachment.name || baseName(attachment.path) || "attachment")}](${attachment.path})`;
  }
  return `@${attachment.path}`;
}

export function formatAttachmentRefForSubmit(attachment: Pick<DisplayAttachment, "path">): string {
  return `@${attachment.path}`;
}

export function parseAttachmentRefsForDisplay(text: string): { text: string; attachments: DisplayAttachment[] } {
  const attachments: DisplayAttachment[] = [];
  const cleaned = text
    .replace(namedAttachmentRefRe, (_full, lead: string, label: string, token: string) => {
      const { core, suffix } = splitTrailingPunctuation(token);
      if (!core || !isDisplayReference(core)) return _full;
      const name = cleanDisplayName(label) || baseName(core) || "attachment";
      attachments.push(displayAttachment(core, name));
      return lead + suffix;
    })
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

export function sortDisplayAttachments<T extends { kind: "image" | "file" | "folder" }>(attachments: T[]): T[] {
  return [...attachments].sort((a, b) => {
    if (a.kind === b.kind) return 0;
    if (a.kind === "image") return -1;
    if (b.kind === "image") return 1;
    return 0;
  });
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
