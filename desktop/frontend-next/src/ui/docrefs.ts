// How a rendered workspace document's own references are read. A reference
// with a scheme ("https:", "mailto:") or "//" leaves the workspace and is left
// to the link routing every other link goes through.

/** Whether href names somewhere outside the workspace, or only a place in
 *  this document, rather than another file in it. */
export function outsideRef(href: string): boolean {
  return /^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith("//");
}

/** docRef resolves href against the document at docPath, both workspace
 *  relative. "/x" is read from the workspace root, as a repository's own
 *  documents mean it. Null for an anchor alone, a reference outside the
 *  workspace, or one that climbs above its root. */
export function docRef(docPath: string, href: string): string | null {
  const raw = href.trim();
  if (!raw || outsideRef(raw)) return null;
  const bare = raw.replace(/[?#].*$/, "");
  if (!bare) return null;
  let decoded: string;
  try {
    decoded = decodeURIComponent(bare);
  } catch {
    decoded = bare;
  }
  const base = decoded.startsWith("/") ? [] : docPath.split("/").slice(0, -1);
  const out = base.filter(Boolean);
  for (const part of decoded.split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") {
      if (!out.length) return null;
      out.pop();
    } else {
      out.push(part);
    }
  }
  return out.length ? out.join("/") : null;
}
