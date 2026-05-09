/** @url mentions — async sibling of @path. Fetches each URL once and inlines under "Referenced URLs". */

/** Trailing punctuation stripped separately — URLs legitimately contain `,` `.` `)` in query strings. */
export const AT_URL_PATTERN = /(?<=^|\s)@(https?:\/\/\S+)/g;

/** Default cap on inlined URL body (chars). */
export const DEFAULT_AT_URL_MAX_CHARS = 32_000;

export interface AtUrlExpansion {
  /** The raw `@url` token as it appeared in the text. */
  token: string;
  /** Absolute URL (after trailing-punctuation strip). */
  url: string;
  /** True if content was inlined. False = skipped (reason in `skip`). */
  ok: boolean;
  /** Page title when extractable from `<title>`. */
  title?: string;
  /** Char count of the (post-truncation) inlined body. */
  chars?: number;
  /** True iff the original page exceeded `maxChars` and was clipped. */
  truncated?: boolean;
  /** Why the mention was skipped — set when ok=false. */
  skip?: "fetch-error" | "non-text" | "timeout" | "blocked";
  /** Free-form error message attached to skip outcomes. */
  error?: string;
}

export interface AtUrlOptions {
  /** Max chars of inlined body per URL. */
  maxChars?: number;
  /** Per-URL fetch timeout in ms. */
  timeoutMs?: number;
  fetcher?: (
    url: string,
    opts: { maxChars?: number; timeoutMs?: number; signal?: AbortSignal },
  ) => Promise<{ url: string; title?: string; text: string; truncated: boolean }>;
  cache?: Map<string, AtUrlExpansion & { body?: string }>;
  /** Forward Esc/abort to the fetcher. */
  signal?: AbortSignal;
}

export async function expandAtUrls(
  text: string,
  opts: AtUrlOptions = {},
): Promise<{ text: string; expansions: AtUrlExpansion[] }> {
  const maxChars = opts.maxChars ?? DEFAULT_AT_URL_MAX_CHARS;
  const fetcher = opts.fetcher;
  if (!fetcher) {
    throw new Error("expandAtUrls: fetcher option is required (wire src/tools/web.ts:webFetch)");
  }

  const seen = new Map<string, AtUrlExpansion>();
  const bodies = new Map<string, string>();
  const order: string[] = [];

  for (const match of text.matchAll(AT_URL_PATTERN)) {
    const rawUrl = match[1] ?? "";
    const url = stripUrlTail(rawUrl);
    if (!url) continue;
    if (seen.has(url)) continue;

    const cached = opts.cache?.get(url);
    if (cached) {
      seen.set(url, cached);
      if (cached.body) bodies.set(url, cached.body);
      order.push(url);
      continue;
    }

    let expansion: AtUrlExpansion;
    let body = "";
    try {
      const page = await fetcher(url, {
        maxChars,
        timeoutMs: opts.timeoutMs,
        signal: opts.signal,
      });
      body = page.text;
      expansion = {
        token: `@${url}`,
        url,
        ok: true,
        title: page.title,
        chars: body.length,
        truncated: page.truncated,
      };
    } catch (err) {
      const message = (err as Error).message ?? String(err);
      let skip: AtUrlExpansion["skip"] = "fetch-error";
      if (/aborted|timeout/i.test(message)) skip = "timeout";
      else if (/40\d|forbidden|access denied|captcha/i.test(message)) skip = "blocked";
      expansion = {
        token: `@${url}`,
        url,
        ok: false,
        skip,
        error: message,
      };
    }
    seen.set(url, expansion);
    if (body) bodies.set(url, body);
    if (opts.cache) opts.cache.set(url, { ...expansion, body });
    order.push(url);
  }

  if (seen.size === 0) return { text, expansions: [] };

  const expansions = order.map((u) => seen.get(u)!).filter(Boolean);
  const blocks: string[] = [];
  for (const ex of expansions) {
    if (ex.ok) {
      const titleAttr = ex.title ? ` title="${escapeAttr(ex.title)}"` : "";
      const truncTag = ex.truncated ? ' truncated="true"' : "";
      const body = bodies.get(ex.url) ?? "";
      blocks.push(`<url href="${ex.url}"${titleAttr}${truncTag}>\n${body}\n</url>`);
    } else {
      const reasonAttr = ex.skip ?? "fetch-error";
      blocks.push(`<url href="${ex.url}" skipped="${reasonAttr}" />`);
    }
  }
  const augmented = `${text}\n\n[Referenced URLs]\n${blocks.join("\n\n")}`;
  return { text: augmented, expansions };
}

/** Only strips `.,;:!?` and unmatched close-brackets — internal path / query punctuation preserved. */
export function stripUrlTail(raw: string): string {
  let s = raw;
  while (s.length > 0) {
    const last = s[s.length - 1]!;
    if (".,;:!?".includes(last)) {
      s = s.slice(0, -1);
      continue;
    }
    if (")]}>".includes(last)) {
      const open = ({ ")": "(", "]": "[", "}": "{", ">": "<" } as const)[
        last as ")" | "]" | "}" | ">"
      ];
      if (!s.includes(open)) {
        s = s.slice(0, -1);
        continue;
      }
    }
    break;
  }
  return s;
}

function escapeAttr(s: string): string {
  return s
    .replace(/"/g, "&quot;")
    .replace(/[\r\n]+/g, " ")
    .trim();
}
