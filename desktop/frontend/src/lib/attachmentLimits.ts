export const MAX_IMAGE_ATTACHMENT_BYTES = 64 * 1024 * 1024;

const MIB = 1024 * 1024;

export interface OversizeAttachment {
  file: File;
  limitMiB: number;
}

// Sizes come from File metadata, so a refused file is never read, hashed or
// base64-encoded.
export function splitOversizeAttachments(files: File[]): { accepted: File[]; oversize: OversizeAttachment[] } {
  const accepted: File[] = [];
  const oversize: OversizeAttachment[] = [];
  for (const file of files) {
    const limit = file.type.startsWith("image/") ? MAX_IMAGE_ATTACHMENT_BYTES : Infinity;
    if (file.size > limit) oversize.push({ file, limitMiB: limit / MIB });
    else accepted.push(file);
  }
  return { accepted, oversize };
}
