// An attachment rides into a turn as a token its ref resolves to. That token is
// an implementation detail: the bytes live in the attachment, the bubble draws a
// thumbnail from it, and the turn parser only needs the token on the wire (CLI
// replay, cross-client round-trip). It must not be shown to the reader as if
// they had typed it — so the bubble and the copy payload strip it.
//
// A stored workspace image rides as "@.reasonix/attachments/<file>"; `keep`
// lists paths whose token must survive a failed thumbnail load, so the
// reference stays readable as a fallback; a message shown without a port (no
// thumbnail to draw) keeps every token too.
const attachmentRefRe = /@\.reasonix\/attachments\/[^\s]*/g;

export interface AttachmentPath {
  path: string;
}

export function stripAttachmentRefs(text: string, attachments?: AttachmentPath[], keep?: Set<string>): string {
  const paths = attachments?.map((a) => a.path);
  if (!text) return text;
  if (!paths || paths.length === 0) return text;
  // A workspace token stays only while its image is shown.
  const dropPaths = new Set(paths.filter((p) => !keep?.has(p)));
  if (dropPaths.size === 0) return text;
  return text
    .replace(attachmentRefRe, (tok) => (dropPaths.has(tok.slice(1)) ? "" : tok))
    .replace(/[ \t]{2,}/g, " ")
    .trim();
}
