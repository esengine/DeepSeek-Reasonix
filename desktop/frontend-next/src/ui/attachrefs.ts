// An attachment rides into a turn as a token its ref resolves to. That token is
// an implementation detail: the bytes live in the attachment, the bubble draws a
// thumbnail from it, and the turn parser only needs the token on the wire (CLI
// replay, cross-client round-trip). It must not be shown to the reader as if
// they had typed it — so the bubble and the copy payload strip it.
//
// Two tokens exist. A stored workspace image rides as
// "@.reasonix/attachments/<file>"; an image the session was handed outside the
// workspace rides as "@__reasonix_external_folder/<hash>/<file>". `keep` lists
// paths whose token must survive a failed thumbnail load, so the reference
// stays readable as a fallback; a message shown without a port (no thumbnail to
// draw) keeps every token too.
const attachmentRefRe = /@\.reasonix\/attachments\/[^\s]*/g;
const externalRefRe = /@__reasonix_external_folder\/[^\s]*/g;

export interface AttachmentPath {
  path: string;
  // The opaque external-folder token an in-place external image arrived as,
  // present only for external images. Once an image backs the token, the surface
  // hides it regardless of a load failure — the token is a hash, and the
  // bubble's "unavailable" frame says more than the hash would.
  ref?: string;
}

export function stripAttachmentRefs(text: string, attachments?: AttachmentPath[], keep?: Set<string>): string {
  const paths = attachments?.map((a) => a.path);
  const refs = attachments?.map((a) => a.ref).filter((r): r is string => !!r) ?? [];
  if (!text) return text;
  if ((!paths || paths.length === 0) && refs.length === 0) return text;
  // A workspace token stays only while its image is shown; an external token is
  // an opaque hash, so once an image names it the surface hides it either way.
  const dropPaths = new Set(paths?.filter((p) => !keep?.has(p)) ?? []);
  if (dropPaths.size === 0 && refs.length === 0) return text;
  return text
    .replace(attachmentRefRe, (tok) => (dropPaths.has(tok.slice(1)) ? "" : tok))
    .replace(externalRefRe, (tok) => (refs.includes(tok.slice(1)) ? "" : tok))
    .replace(/[ \t]{2,}/g, " ")
    .trim();
}
