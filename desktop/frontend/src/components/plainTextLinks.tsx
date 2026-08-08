import type { ReactNode } from "react";
import { openExternal } from "../lib/bridge";

// PlainTextLinks turns bare http(s) URLs inside plain-text user messages into
// clickable links, mirroring the TUI behaviour. Unlike the markdown renderer
// (which runs full GFM autolinks via RichMarkdownLink), user bubbles stay
// plain text — only http(s) URLs are recognised, and trailing sentence
// punctuation is trimmed from the destination while the visible text keeps
// its original spelling.
const PLAIN_URL_RE = /https?:\/\/[^\s<>"'`]+/g;

// Trailing punctuation trimmed from a matched URL, ASCII plus CJK full-width
// forms, so "https://x.com." does not swallow the sentence period. Built via
// new RegExp with an escaped "]": a literal /.../ containing "/>" would
// confuse the TSX parser, and a bare "]" as the first class member trips up
// V8's class parsing when "?" and ")" follow.
const TRAILING_URL_PUNCT = new RegExp("[\\].,;:!?)}>”’」』】】，。；：！？）］｝]+$");

function trimTrailingPunct(url: string): string {
  return url.replace(TRAILING_URL_PUNCT, "");
}

/**
 * linkifyPlainText splits plain text into React nodes, wrapping every bare
 * http(s) URL in an anchor that opens the system browser via openExternal
 * (never navigating the app itself). Non-URL text passes through untouched.
 */
export function linkifyPlainText(text: string): ReactNode[] {
  const parts: ReactNode[] = [];
  let last = 0;
  let id = 0;
  for (const match of text.matchAll(PLAIN_URL_RE)) {
    const idx = match.index ?? 0;
    const raw = match[0];
    const url = trimTrailingPunct(raw);
    if (url === "") continue;
    if (idx > last) parts.push(text.slice(last, idx));
    parts.push(
      <a
        key={`url:${id++}`}
        className="msg__text-link"
        href={url}
        title={url}
        onClick={(event) => {
          event.preventDefault();
          openExternal(url);
        }}
      >
        {raw}
      </a>,
    );
    last = idx + raw.length;
  }
  if (last < text.length) parts.push(text.slice(last));
  return parts;
}
