/** Models sometimes emit HTML-escaped code (issue #657: `&quot;` instead of `"`). Terminals don't render entities, so decode at the markdown boundary. */

const NAMED: Record<string, string> = {
  quot: '"',
  apos: "'",
  amp: "&",
  lt: "<",
  gt: ">",
  nbsp: "\u00a0",
};

const ENTITY_RE = /&(?:#x([0-9A-Fa-f]+)|#(\d+)|([a-zA-Z]+));/g;

/** Decode the entity shapes LLMs leak into code (`&quot;` / `&amp;` / `&#34;` / `&#x22;`). Unknown names are left as-is, since legitimate text may quote entities by name. */
export function decodeHtmlEntities(text: string): string {
  if (text.indexOf("&") === -1) return text;
  return text.replace(
    ENTITY_RE,
    (match, hex: string | undefined, dec: string | undefined, name: string | undefined) => {
      if (hex !== undefined) {
        const code = Number.parseInt(hex, 16);
        return Number.isFinite(code) && code > 0 ? safeFromCodePoint(code, match) : match;
      }
      if (dec !== undefined) {
        const code = Number.parseInt(dec, 10);
        return Number.isFinite(code) && code > 0 ? safeFromCodePoint(code, match) : match;
      }
      if (name !== undefined) {
        const lower = name.toLowerCase();
        return Object.hasOwn(NAMED, lower) ? NAMED[lower]! : match;
      }
      return match;
    },
  );
}

function safeFromCodePoint(code: number, fallback: string): string {
  try {
    return String.fromCodePoint(code);
  } catch {
    return fallback;
  }
}
