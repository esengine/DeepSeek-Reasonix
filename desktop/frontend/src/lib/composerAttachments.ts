export interface Attachment {
  clientAttachmentId?: string;
  path: string;
  previewUrl?: string;
  displayName?: string;
  draftId?: string;
  file?: File;
}

export function baseName(path: string): string {
  const clean = path.replace(/[\\/]+$/, "");
  return clean.split(/[\\/]/).filter(Boolean).pop() ?? path;
}

export function attachmentName(attachment: Attachment): string {
  return (attachment.displayName || baseName(attachment.path) || "attachment").trim();
}

export function attachmentExt(name: string): string {
  const dot = name.lastIndexOf(".");
  return dot >= 0 ? name.slice(dot + 1).toUpperCase() : "";
}

export function hasImageAttachments(items: Attachment[]): boolean {
  return items.some((attachment) => Boolean(attachment.previewUrl));
}

export function displayRefName(name: string): string {
  return name.replace(/[\[\]\(\)\r\n]+/g, " ").replace(/\s+/g, " ").trim() || "attachment";
}

export function formatAttachmentDisplayReference(attachment: Attachment): string {
  return `@[${displayRefName(attachmentName(attachment))}](${attachment.path})`;
}

export function sortComposerAttachments(items: Attachment[]): Attachment[] {
  return [...items];
}
