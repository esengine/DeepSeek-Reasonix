// Canonical parsing and validation for local file URLs used by Markdown links.

export function hasDisallowedWindowsPathSyntax(path: string): boolean {
  const slashPath = path.replace(/\\/g, "/");
  if (slashPath.includes("\0")) return true;
  if (/^\/\/[.?](?:\/|$)/.test(slashPath)) return true;
  const isDrivePath = /^[A-Za-z]:\//.test(slashPath);
  const isUncPath = slashPath.startsWith("//");
  if (!isDrivePath && !isUncPath) return false;
  const remainder = slashPath.slice(2);
  if (remainder.includes(":")) return true;
  return remainder.split("/").some((component) => {
    const base = component.trimEnd().replace(/\.+$/, "").split(".", 1)[0]?.toUpperCase() ?? "";
    return /^(?:CON|PRN|AUX|NUL|CLOCK\$|CONIN\$|CONOUT\$|COM[1-9¹²³]|LPT[1-9¹²³])$/.test(base);
  });
}

function hasDisallowedRawFileUrlSyntax(href: string): boolean {
  let rawPath: string;
  try {
    rawPath = decodeURIComponent(href.slice("file:".length)).replace(/\\/g, "/");
  } catch {
    return true;
  }
  // URL normalisation removes dot segments. Reject Windows device authorities
  // before constructing URL so file:////./PhysicalDrive0 cannot become the
  // apparently ordinary //PhysicalDrive0 UNC path.
  return /^\/\/[.?](?:\/|$)/.test(rawPath) || /^\/{4,}[.?](?:\/|$)/.test(rawPath);
}

/**
 * Returns the decoded filesystem path represented by a local file URL.
 * The scheme check is intentionally case-sensitive so `FILE:` cannot bypass
 * the Markdown URL allowlist and reach a browser or native opener.
 */
export function localPathFromHref(href?: string): string | null {
  if (!href || !href.startsWith("file://")) return null;
  if (hasDisallowedRawFileUrlSyntax(href)) return null;

  try {
    const url = new URL(href);
    if (url.protocol !== "file:") return null;
    if (url.username || url.password || url.port) return null;
    if (url.search || url.hash) return null;
    if (url.hostname === "." || url.hostname === "?") return null;

    let path = decodeURIComponent(url.pathname);
    if (url.hostname) path = `//${url.hostname}${path}`;

    // file:///D:/... has a URL root slash that is not part of the Windows
    // drive path. Multiple leading slashes are the slash-form UNC variant.
    if (/^\/[A-Za-z]:\//.test(path)) path = path.slice(1);
    if (path.startsWith("//")) path = `//${path.replace(/^\/+/, "")}`;
    if (hasDisallowedWindowsPathSyntax(path)) return null;
    if (/^[A-Za-z]:\//.test(path)) return path;
    if (!path.startsWith("/")) return null;
    return path;
  } catch {
    return null;
  }
}

export function isLocalFileHref(href?: string): boolean {
  return localPathFromHref(href) !== null;
}
