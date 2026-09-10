import { desktopHost } from "./desktopHost";

export const REMOTE_MARKDOWN_IMAGE_PATH = "/__reasonix_remote_markdown_image";

export interface MarkdownImageView {
  url: string;
  filename?: string;
  mime?: string;
  size?: number;
  openHref?: string;
  errorCode?: string;
}

function runningInDesktopShell(): boolean {
  return desktopHost().kind !== "none";
}

export function hasMarkdownImageResolver(): boolean {
  return typeof desktopHost().app?.ResolveMarkdownImageForTab === "function";
}

// Route only absolute remote Markdown images back through the local desktop
// asset origin; relative, data, blob, and workspace-media URLs remain local
// and unchanged.
export function markdownImageSource(src: string | undefined, nativeShell = runningInDesktopShell()): string {
  const value = src?.trim() ?? "";
  if (!nativeShell || value === "") return value;

  let remoteURL = value;
  if (value.startsWith("//")) {
    const protocol = typeof window !== "undefined" && /^https?:$/.test(window.location.protocol)
      ? window.location.protocol
      : "https:";
    remoteURL = protocol + value;
  } else if (!/^https?:\/\//i.test(value)) {
    return value;
  }

  return `${REMOTE_MARKDOWN_IMAGE_PATH}?url=${encodeURIComponent(remoteURL)}`;
}
