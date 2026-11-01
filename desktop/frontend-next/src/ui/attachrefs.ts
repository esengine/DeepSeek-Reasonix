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

  // Walk the string once. For each dropped token we remove the marker and at
  // most one adjacent space, then compact only the whitespace the removal
  // leaves behind — never the whitespace elsewhere in the message. A code
  // block's indentation (a four-space Python block, a pasted snippet) must
  // survive in both the bubble and the copy payload, so we never run a global
  // whitespace-compacting pass over the whole text.
  const shouldDrop = (tok: string): boolean =>
    tok.startsWith("@__") ? refs.includes(tok.slice(1)) : dropPaths.has(tok.slice(1));

  // Collect every marker across both token shapes, in document order.
  const markers: { start: number; end: number; tok: string }[] = [];
  for (const re of [attachmentRefRe, externalRefRe]) {
    re.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = re.exec(text)) !== null) markers.push({ start: m.index, end: m.index + m[0].length, tok: m[0] });
  }
  markers.sort((a, b) => a.start - b.start);

  let out = "";
  let cursor = 0;
  for (const { start, end, tok } of markers) {
    if (!shouldDrop(tok)) {
      out += text.slice(cursor, end);
      cursor = end;
      continue;
    }
    let pre = text.slice(cursor, start);
    let dropTrailing = 0;
    if (pre.length > 0 && (pre[pre.length - 1] === " " || pre[pre.length - 1] === "\t")) dropTrailing = 1;
    let dropLeading = 0;
    if (text[end] === " " || text[end] === "\t") dropLeading = 1;
    if (dropTrailing && dropLeading) {
      dropTrailing = 1;
      dropLeading = 0;
    }
    out += pre.slice(0, pre.length - dropTrailing);
    cursor = end + dropLeading;
  }
  out += text.slice(cursor);
  return out;
}
