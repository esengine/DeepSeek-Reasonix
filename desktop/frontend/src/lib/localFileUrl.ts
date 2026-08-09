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
// isLoopbackHostname mirrors the backend's loopback set (localhost / 127.0.0.1
// / ::1). Any other file:// host decodes to a remote UNC path and must be
// refused before it reaches OpenLocalPath.
function isLoopbackHostname(host: string): boolean {
  const h = host.toLowerCase();
  return h === "localhost" || h === "127.0.0.1" || h === "::1";
}

export function localPathFromHref(href?: string): string | null {
  if (!href || !href.startsWith("file://")) return null;
  if (hasDisallowedRawFileUrlSyntax(href)) return null;

  // Plain-UNC linkification (linkifyLocalPaths → localPathHref) emits
  // file:///\nas\share\... hrefs (three slashes then a backslash). WHATWG
  // URL normalization would turn the backslash into a fourth slash, making
  // it look like the empty-authority remote-UNC form that is refused below.
  // Recognize the literal spelling and hand the UNC path through directly —
  // this matches the product rule that plain UNC paths (\\host\share) open
  // normally. The remote-authority and 4+-slash attack spellings do NOT
  // match (they have "/" where this regex requires "\") and stay refused.
  if (/^file:\/\/\/\\/.test(href)) {
    const unc = "\\" + href.slice("file:///".length); // \nas\... → \\nas\...
    if (hasDisallowedWindowsPathSyntax(unc)) return null;
    return unc.replace(/\\/g, "/"); // //nas/share/... (backend FromSlash form)
  }
  // Linkified UNC hrefs are %5C-encoded by localPathHref so markdown URL
  // normalization cannot fold backslashes into extra slashes; decode back.
  if (href.startsWith("file:///%5C")) {
    const decoded = decodeURIComponent(href.slice("file:///".length)); // \\nas\share\...
    if (hasDisallowedWindowsPathSyntax(decoded)) return null;
    return decoded.replace(/\\/g, "/"); // //nas/share/...
  }

  try {
    const url = new URL(href);
    if (url.protocol !== "file:") return null;
    if (url.username || url.password || url.port) return null;
    if (url.search || url.hash) return null;
    if (url.hostname === "." || url.hostname === "?") return null;

    let path = decodeURIComponent(url.pathname);
    // file:////host/share (4+ slashes) parses with an EMPTY hostname and a
    // path already starting with "//" — collapsing it would hand the remote
    // UNC to OpenLocalPath, which legitimately allows plain UNC paths, and
    // trigger an SMB connection on click. Refuse like the backend does;
    // file:///C:/x.txt (3 slashes) is unaffected.
    if (url.hostname === "" && path.startsWith("//")) return null;
    if (url.hostname) {
      // Remote authorities are refused outright — same rule as the backend
      // (open_local_path.go). localPathFromHref decodes file:// URLs into
      // plain UNC paths, which OpenLocalPath legitimately allows; letting a
      // non-loopback host through would hand the remote UNC to the OS opener
      // and trigger an SMB connection (Net-NTLM credential negotiation) on
      // click. Loopback hosts are dropped like the backend does.
      if (!isLoopbackHostname(url.hostname)) return null;
    }

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
