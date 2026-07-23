import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { EditorProps } from "../CodeViewer";
import { highlightToHtml, shouldHighlightSource } from "../../lib/highlight";
import { useT } from "../../lib/i18n";
import { CopyButton } from "../CopyButton";

// Line-numbered code viewer with virtual scroll and viewer-scoped search.
const VIRTUAL_THRESHOLD = 100;
const ROW_HEIGHT_ESTIMATE = 22;
const OVERSCAN = 15;
const SEARCH_DEBOUNCE_MS = 100;
const WORD_CHARACTER_RE = /[\p{L}\p{N}_]/u;
export const MAX_SEARCH_MATCHES = 10_000;

export interface CodeSearchMatch {
  lineIndex: number;
  start: number;
  end: number;
  absoluteStart: number;
  absoluteEnd: number;
}

export interface CodeSearchResult {
  matches: CodeSearchMatch[];
  truncated: boolean;
}

export function findCodeMatches(
  source: string | readonly string[],
  query: string,
  caseSensitive = false,
  wholeWord = false,
  maxMatches = MAX_SEARCH_MATCHES,
): CodeSearchResult {
  if (!query) return { matches: [], truncated: false };

  const matches: CodeSearchMatch[] = [];
  const lines = typeof source === "string" ? source.split("\n") : source;
  const pattern = new RegExp(escapeRegex(query), caseSensitive ? "gu" : "giu");
  let absoluteOffset = 0;

  for (let lineIndex = 0; lineIndex < lines.length; lineIndex += 1) {
    const line = lines[lineIndex];
    pattern.lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = pattern.exec(line)) !== null) {
      const start = match.index;
      const end = start + match[0].length;
      const startsInsideWord = start > 0 && isWordCharacter(codePointBefore(line, start));
      const endsInsideWord = end < line.length && isWordCharacter(codePointAt(line, end));
      if (!wholeWord || (!startsInsideWord && !endsInsideWord)) {
        if (matches.length >= maxMatches) {
          return { matches, truncated: true };
        }
        matches.push({
          lineIndex,
          start,
          end,
          absoluteStart: absoluteOffset + start,
          absoluteEnd: absoluteOffset + end,
        });
      }
      // The query is non-empty, but keep the loop safe if regex behavior ever
      // changes around an unusual Unicode sequence.
      if (match[0].length === 0) pattern.lastIndex += 1;
    }
    absoluteOffset += line.length + 1;
  }

  return { matches, truncated: false };
}

// Insert mark elements into one already-highlighted line. Search offsets stay
// relative to raw source, so escaped entities and token span boundaries remain
// intact without rebuilding the full file HTML on every keystroke.
export function highlightLineMatches(
  highlightedLineHtml: string,
  matches: CodeSearchMatch[],
  currentMatch?: CodeSearchMatch,
): string {
  if (matches.length === 0) return highlightedLineHtml;

  let htmlOffset = 0;
  let sourceOffset = 0;
  let matchIndex = 0;
  let markOpen = false;
  let result = "";

  const openMark = () => (
    matches[matchIndex]?.absoluteStart === currentMatch?.absoluteStart
      ? '<mark class="code-search-hl code-search-hl--current">'
      : '<mark class="code-search-hl">'
  );

  while (htmlOffset < highlightedLineHtml.length) {
    const char = highlightedLineHtml[htmlOffset];
    if (char === "<") {
      const tagEnd = highlightedLineHtml.indexOf(">", htmlOffset);
      if (tagEnd === -1) {
        result += highlightedLineHtml.slice(htmlOffset);
        break;
      }
      const tag = highlightedLineHtml.slice(htmlOffset, tagEnd + 1);
      if (markOpen) result += "</mark>";
      result += tag;
      if (markOpen) result += openMark();
      htmlOffset = tagEnd + 1;
      continue;
    }

    if (!markOpen && matches[matchIndex]?.start === sourceOffset) {
      markOpen = true;
      result += openMark();
    }

    let token: string;
    let sourceLength: number;
    if (char === "&") {
      const entityEnd = highlightedLineHtml.indexOf(";", htmlOffset);
      if (entityEnd !== -1) {
        token = highlightedLineHtml.slice(htmlOffset, entityEnd + 1);
        sourceLength = decodedEntityLength(token);
      } else {
        token = char;
        sourceLength = 1;
      }
    } else {
      const codePoint = highlightedLineHtml.codePointAt(htmlOffset) ?? 0;
      sourceLength = codePoint > 0xffff ? 2 : 1;
      token = highlightedLineHtml.slice(htmlOffset, htmlOffset + sourceLength);
    }

    result += token;
    htmlOffset += token.length;
    sourceOffset += sourceLength;

    if (markOpen && matches[matchIndex]?.end === sourceOffset) {
      result += "</mark>";
      markOpen = false;
      matchIndex += 1;
    }
  }

  if (markOpen) result += "</mark>";
  return result;
}

// A multiline highlight.js span may cross a newline. Each virtual row needs
// valid standalone HTML, so close active tags at the boundary and reopen the
// same stack on the next line.
export function splitHighlightedCodeLines(html: string): string[] {
  const lines: string[] = [];
  const openTags: string[] = [];
  let current = "";
  let offset = 0;

  while (offset < html.length) {
    if (html[offset] === "\n") {
      current += closeTags(openTags);
      lines.push(current);
      current = openTags.join("");
      offset += 1;
      continue;
    }
    if (html[offset] === "<") {
      const tagEnd = html.indexOf(">", offset);
      if (tagEnd !== -1) {
        const tag = html.slice(offset, tagEnd + 1);
        current += tag;
        if (/^<(span|mark)\b/.test(tag)) {
          openTags.push(tag);
        } else if (/^<\/(span|mark)>$/.test(tag)) {
          openTags.pop();
        }
        offset = tagEnd + 1;
        continue;
      }
    }
    const codePoint = html.codePointAt(offset) ?? 0;
    const length = codePoint > 0xffff ? 2 : 1;
    current += html.slice(offset, offset + length);
    offset += length;
  }

  current += closeTags(openTags);
  lines.push(current);
  return lines;
}

export default function LineNumberCode({
  value,
  language,
  showLineNumbers,
  maxHeight,
  sourceSize,
}: EditorProps) {
  const t = useT();
  const lines = useMemo(() => value.split("\n"), [value]);
  const syntaxHighlight = shouldHighlightSource(value, sourceSize, lines.length);
  const baseLineHtmls = useMemo(
    () => syntaxHighlight
      ? splitHighlightedCodeLines(highlightToHtml(value, language))
      : lines.map(escapeHtml),
    [language, lines, syntaxHighlight, value],
  );

  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [wholeWord, setWholeWord] = useState(false);
  const [currentMatchIdx, setCurrentMatchIdx] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const searchTimerRef = useRef<number | null>(null);

  const searchResult = useMemo(
    () => findCodeMatches(lines, searchQuery, caseSensitive, wholeWord),
    [lines, searchQuery, caseSensitive, wholeWord],
  );
  const matches = searchResult.matches;
  const totalMatches = matches.length;
  const activeMatchIndex = totalMatches > 0 ? currentMatchIdx % totalMatches : 0;
  const activeMatch = matches[activeMatchIndex];
  const matchesByLine = useMemo(
    () => {
      const grouped = new Map<number, CodeSearchMatch[]>();
      for (const match of matches) {
        const lineMatches = grouped.get(match.lineIndex);
        if (lineMatches) lineMatches.push(match);
        else grouped.set(match.lineIndex, [match]);
      }
      return grouped;
    },
    [matches],
  );
  const searchPending = query !== searchQuery;

  useEffect(() => {
    return () => {
      if (searchTimerRef.current != null) window.clearTimeout(searchTimerRef.current);
    };
  }, []);

  const commitSearchQuery = useCallback((nextQuery: string) => {
    if (searchTimerRef.current != null) {
      window.clearTimeout(searchTimerRef.current);
      searchTimerRef.current = null;
    }
    setCurrentMatchIdx(0);
    setSearchQuery(nextQuery);
  }, []);

  const updateQuery = useCallback((nextQuery: string) => {
    setQuery(nextQuery);
    setCurrentMatchIdx(0);
    if (searchTimerRef.current != null) window.clearTimeout(searchTimerRef.current);
    searchTimerRef.current = window.setTimeout(() => {
      searchTimerRef.current = null;
      setSearchQuery(nextQuery);
    }, SEARCH_DEBOUNCE_MS);
  }, []);

  const closeSearch = useCallback(() => {
    setSearchOpen(false);
    setQuery("");
    commitSearchQuery("");
  }, [commitSearchQuery]);

  const scrollRef = useRef<HTMLDivElement>(null);
  const isVirtual = showLineNumbers !== false && lines.length > VIRTUAL_THRESHOLD;
  const virtualizer = useVirtualizer({
    count: isVirtual ? lines.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT_ESTIMATE,
    overscan: OVERSCAN,
  });

  const scrollToLine = useCallback(
    (index: number) => {
      if (!scrollRef.current) return;
      if (isVirtual) {
        virtualizer.scrollToIndex(index, { align: "center" });
      } else {
        const row = scrollRef.current.querySelector<HTMLElement>(`[data-line-index="${index}"]`);
        if (row && typeof row.scrollIntoView === "function") {
          row.scrollIntoView({ block: "center", inline: "nearest", behavior: "smooth" });
        } else {
          scrollRef.current.scrollTo({
            top: index * ROW_HEIGHT_ESTIMATE - scrollRef.current.clientHeight / 2,
            behavior: "smooth",
          });
        }
      }
    },
    [isVirtual, virtualizer],
  );
  const scrollToLineRef = useRef(scrollToLine);
  scrollToLineRef.current = scrollToLine;

  useEffect(() => {
    setCurrentMatchIdx(0);
    if (!searchQuery || !matches[0]) return;
    const timer = window.setTimeout(() => scrollToLineRef.current(matches[0].lineIndex), 0);
    return () => window.clearTimeout(timer);
  }, [matches, searchQuery]);

  const jumpToMatch = useCallback(
    (direction: 1 | -1) => {
      if (searchPending) {
        commitSearchQuery(query);
        return;
      }
      if (totalMatches === 0) return;
      const nextIndex = direction === 1
        ? (activeMatchIndex + 1) % totalMatches
        : (activeMatchIndex - 1 + totalMatches) % totalMatches;
      setCurrentMatchIdx(nextIndex);
      const lineIndex = matches[nextIndex]?.lineIndex;
      if (lineIndex != null) scrollToLine(lineIndex);
    },
    [activeMatchIndex, commitSearchQuery, matches, query, scrollToLine, searchPending, totalMatches],
  );

  const lineNoWidth = String(lines.length).length;
  const renderRow = (index: number) => {
    const lineNo = index + 1;
    const lineMatches = matchesByLine.get(index) ?? [];
    const lineHtml = lineMatches.length > 0
      ? highlightLineMatches(baseLineHtmls[index] ?? "", lineMatches, activeMatch)
      : baseLineHtmls[index] ?? "";
    const isCurrent = searchQuery && activeMatch?.lineIndex === index;
    const isDimmed = searchQuery && !matchesByLine.has(index);
    return (
      <div
        key={index}
        data-line-index={index}
        className={`code-line-row${isCurrent ? " code-line-row--current" : ""}${isDimmed ? " code-line-row--dim" : ""}`}
      >
        {showLineNumbers !== false && (
          <span
            className="code-line-ln"
            style={{ minWidth: `${lineNoWidth + 2}ch` }}
            aria-label={t("workspace.codeLine", { line: lineNo })}
          >
            {lineNo}
          </span>
        )}
        <code
          className="code-line-text"
          dangerouslySetInnerHTML={{ __html: lineHtml || " " }}
        />
      </div>
    );
  };

  const totalMatchLabel = searchResult.truncated ? `${totalMatches}+` : totalMatches;

  return (
    <div
      className={`code-block__wrap${searchOpen ? " code-block__wrap--search-open" : ""}`}
      onKeyDownCapture={(event) => {
        if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
          event.preventDefault();
          event.stopPropagation();
          setSearchOpen(true);
          window.setTimeout(() => {
            inputRef.current?.focus();
            inputRef.current?.select();
          }, 0);
        } else if (event.key === "Escape" && searchOpen) {
          event.preventDefault();
          event.stopPropagation();
          closeSearch();
          window.setTimeout(() => scrollRef.current?.focus(), 0);
        }
      }}
    >
      {searchOpen && (
        <div className="code-search">
          <input
            ref={inputRef}
            type="text"
            className="code-search__input"
            placeholder={t("workspace.searchPlaceholder")}
            value={query}
            onChange={(event) => updateQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                jumpToMatch(event.shiftKey ? -1 : 1);
              }
            }}
          />

          {query && (
            <span className="code-search__count">
              {searchPending
                ? t("common.loading")
                : t("workspace.searchCount", {
                  current: totalMatches > 0 ? activeMatchIndex + 1 : 0,
                  total: totalMatchLabel,
                })}
            </span>
          )}

          {query && !searchPending && totalMatches > 0 && (
            <>
              <button
                className="code-search__nav"
                onClick={() => jumpToMatch(-1)}
                aria-label={t("workspace.searchPrevious")}
                title={t("workspace.searchPrevious")}
                type="button"
              >
                <svg width="12" height="12" viewBox="0 0 12 12"><path d="M6 2L2 6l4 4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/></svg>
              </button>
              <button
                className="code-search__nav"
                onClick={() => jumpToMatch(1)}
                aria-label={t("workspace.searchNext")}
                title={t("workspace.searchNext")}
                type="button"
              >
                <svg width="12" height="12" viewBox="0 0 12 12"><path d="M2 2l4 4-4 4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/></svg>
              </button>
            </>
          )}

          <button
            className={`code-search__toggle${caseSensitive ? " code-search__toggle--on" : ""}`}
            onClick={() => {
              setCurrentMatchIdx(0);
              setCaseSensitive((enabled) => !enabled);
            }}
            aria-label={t("workspace.searchMatchCase")}
            aria-pressed={caseSensitive}
            title={t("workspace.searchMatchCase")}
            type="button"
          >
            Aa
          </button>
          <button
            className={`code-search__toggle${wholeWord ? " code-search__toggle--on" : ""}`}
            onClick={() => {
              setCurrentMatchIdx(0);
              setWholeWord((enabled) => !enabled);
            }}
            aria-label={t("workspace.searchWholeWord")}
            aria-pressed={wholeWord}
            title={t("workspace.searchWholeWord")}
            type="button"
          >
            ab
          </button>
          <button
            className="code-search__close"
            onClick={closeSearch}
            aria-label={t("workspace.searchClose")}
            title={t("workspace.searchClose")}
            type="button"
          >
            ✕
          </button>
        </div>
      )}

      <div
        ref={scrollRef}
        className="code hljs code--lines"
        data-lang={language}
        data-highlight-mode={syntaxHighlight ? "syntax" : "plain"}
        tabIndex={0}
        style={{
          maxHeight: maxHeight ?? undefined,
          overflow: maxHeight != null || isVirtual ? "auto" : undefined,
        }}
      >
        {isVirtual ? (
          <div
            className="code-lines-wrap"
            style={{ height: virtualizer.getTotalSize(), width: "100%", position: "relative" }}
          >
            {virtualizer.getVirtualItems().map((row) => (
              <div
                key={row.key}
                data-index={row.index}
                ref={virtualizer.measureElement}
                style={{
                  position: "absolute",
                  top: 0,
                  left: 0,
                  width: "100%",
                  transform: `translateY(${row.start}px)`,
                }}
              >
                {renderRow(row.index)}
              </div>
            ))}
          </div>
        ) : (
          <div className="code-lines-wrap">
            {lines.map((_, index) => renderRow(index))}
          </div>
        )}
      </div>
      <CopyButton text={value} className="code-block__copy" />
    </div>
  );
}

function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function escapeHtml(value: string): string {
  return value.replace(/[&<>]/g, (character) => (
    character === "&" ? "&amp;" : character === "<" ? "&lt;" : "&gt;"
  ));
}

function isWordCharacter(value: string): boolean {
  return value !== "" && WORD_CHARACTER_RE.test(value);
}

function codePointBefore(value: string, offset: number): string {
  const codePoints = Array.from(value.slice(0, offset));
  return codePoints[codePoints.length - 1] ?? "";
}

function codePointAt(value: string, offset: number): string {
  return Array.from(value.slice(offset))[0] ?? "";
}

function decodedEntityLength(entity: string): number {
  const body = entity.slice(1, -1).toLowerCase();
  if (["amp", "lt", "gt", "quot", "apos", "#39", "#x27"].includes(body)) return 1;
  const numeric = body.startsWith("#x")
    ? Number.parseInt(body.slice(2), 16)
    : body.startsWith("#")
      ? Number.parseInt(body.slice(1), 10)
      : Number.NaN;
  return Number.isFinite(numeric) && numeric >= 0 && numeric <= 0x10ffff
    ? String.fromCodePoint(numeric).length
    : entity.length;
}

function closeTags(openTags: string[]): string {
  return [...openTags]
    .reverse()
    .map((tag) => tag.startsWith("<mark") ? "</mark>" : "</span>")
    .join("");
}
