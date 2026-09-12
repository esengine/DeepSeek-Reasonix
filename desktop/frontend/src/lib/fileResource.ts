import type { FileResourceRef } from "./presentedFileNavigation";

const extension = (path: string) => path.replaceAll("\\", "/").split("/").pop()?.split(".").pop()?.toLowerCase() ?? "";
const MEDIA = new Set(["html", "htm", "pdf", "png", "jpg", "jpeg", "gif", "webp", "bmp", "ico", "svg", "mp3", "wav", "ogg", "m4a", "aac", "mp4", "webm", "mov", "m4v", "ogv"]);
const BINARY = new Set(["png", "jpg", "jpeg", "gif", "webp", "bmp", "ico", "svg", "pdf", "mp3", "wav", "ogg", "m4a", "aac", "flac", "mp4", "webm", "mov", "m4v", "ogv", "zip", "tar", "gz", "7z", "rar", "doc", "docx", "xls", "xlsx", "ppt", "pptx"]);

export interface FileResourceCapabilities {
  preview: boolean;
  source: boolean;
  browser: boolean;
  revealTree: boolean;
  copyPath: boolean;
  openNative: boolean;
  revealNative: boolean;
  saveCopy: boolean;
}

/** The host still revalidates every action; this snapshot only controls honest UI affordances. */
export function fileResourceCapabilities(ref: FileResourceRef): FileResourceCapabilities {
  const remote = ref.hostId !== "local";
  const ext = extension(ref.path);
  return {
    preview: true,
    source: !BINARY.has(ext),
    browser: !remote && MEDIA.has(ext),
    revealTree: true,
    copyPath: true,
    openNative: !remote,
    revealNative: !remote,
    saveCopy: true,
  };
}
