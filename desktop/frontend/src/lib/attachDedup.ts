// attachDedup centralizes the small deduplication helpers the composer
// uses when adding attachments. The composer's image paste/drop already
// works, but a user can drop the same file twice (or paste the same
// clipboard twice) and end up with two @path references pointing to
// the same on-disk blob — which the kernel would re-process. Dedup
// keys on the SHA-256 of the file bytes, with a path fallback for the
// case where a file:// URL or data: URL is the only available signal.

const HEX = "0123456789abcdef";

function bytesToHex(bytes: Uint8Array): string {
  let out = "";
  for (let i = 0; i < bytes.length; i++) {
    const b = bytes[i];
    out += HEX[(b >> 4) & 0xf] + HEX[b & 0xf];
  }
  return out;
}

// sha256 returns the hex SHA-256 of `blob`. The Web Crypto Subtle API
// is available in Wails' WebView (Chromium / WebKitGTK 4.1+); we
// don't fall back to a JS implementation because a no-op (returning
// "") would silently disable dedup, which is worse than no dedup
// at all. The caller checks the empty-string return and skips the
// dedup step in that case.
export async function sha256(blob: Blob): Promise<string> {
  if (typeof crypto === "undefined" || !crypto.subtle) return "";
  try {
    const buf = await blob.arrayBuffer();
    const digest = await crypto.subtle.digest("SHA-256", buf);
    return bytesToHex(new Uint8Array(digest));
  } catch {
    return "";
  }
}

// DedupIndex tracks the SHA-256 hashes the user has already attached
// in the current composer session (lives for the life of the App
// mount; cleared on new session because the user expects a fresh
// palette). A path-keyed fallback lets a non-Crypto-capable browser
// still dedup by URL when the same path is dropped twice — the
// fallback is weaker (the same content from two paths won't match)
// but covers the common "dropped the same file twice" case.
export class DedupIndex {
  private hashes = new Set<string>();
  private paths = new Set<string>();

  seen(hash: string, path: string): boolean {
    if (hash) {
      if (this.hashes.has(hash)) return true;
      return false;
    }
    return this.paths.has(path);
  }

  add(hash: string, path: string): void {
    if (hash) this.hashes.add(hash);
    this.paths.add(path);
  }

  forget(hash: string, path: string): void {
    if (hash) this.hashes.delete(hash);
    this.paths.delete(path);
  }

  clear(): void {
    this.hashes.clear();
    this.paths.clear();
  }
}
