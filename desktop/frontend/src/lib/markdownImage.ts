export const REMOTE_MARKDOWN_IMAGE_PATH = "/__reasonix_remote_markdown_image";

export interface MarkdownImageView {
  url: string;
  filename?: string;
  mime?: string;
  size?: number;
  openHref?: string;
  errorCode?: string;
}

function runningInWailsShell(): boolean {
  return typeof window !== "undefined" && window.runtime != null;
}

export function hasMarkdownImageResolver(): boolean {
  return typeof window !== "undefined"
    && typeof window.go?.main?.App?.ResolveMarkdownImageForTab === "function";
}

// WebView2 runs without the Windows system proxy. Route only absolute remote
// Markdown images back through the local Wails asset server; relative, data,
// blob, and workspace-media URLs remain local and unchanged.
export function markdownImageSource(src: string | undefined, nativeShell = runningInWailsShell()): string {
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
