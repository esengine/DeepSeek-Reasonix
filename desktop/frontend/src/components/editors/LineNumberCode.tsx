import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { EditorProps } from "../CodeViewer";
import { highlightToHtml, shouldHighlightSource } from "../../lib/highlight";
import { useT } from "../../lib/i18n";
import { CopyButton } from "../CopyButton";
import {
  findCodeMatches,
  MAX_REGEX_PATTERN_LENGTH,
  MAX_REGEX_SOURCE_LENGTH,
  MAX_SEARCH_MATCHES,
  type CodeSearchMatch,
  type CodeSearchResult,
  type RegexSearchErrorCode,
} from "./codeSearch";
import { startRegexSearch } from "./regexSearchClient";

export { findCodeMatches, MAX_SEARCH_MATCHES } from "./codeSearch";

// Line-numbered code viewer with virtual scroll and viewer-scoped search.
const VIRTUAL_THRESHOLD = 100;
const ROW_HEIGHT_ESTIMATE = 22;
const OVERSCAN = 15;
const SEARCH_DEBOUNCE_MS = 100;
const EMPTY_SEARCH_RESULT: CodeSearchResult = { matches: [], truncated: false };

interface RegexSearchState {
  source: string;
  query: string;
  caseSensitive: boolean;
  wholeWord: boolean;
  status: "idle" | "pending" | "ready" | "error";
  result: CodeSearchResult;
  error?: RegexSearchErrorCode;
  detail?: string;
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
  scrollMode,
  sourceSize,
  searchRequestPending,
  onSearchRequestConsumed,
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
  const [regexEnabled, setRegexEnabled] = useState(false);
  const [regexSearchState, setRegexSearchState] = useState<RegexSearchState>({
    source: value,
    query: "",
    caseSensitive: false,
    wholeWord: false,
    status: "idle",
    result: EMPTY_SEARCH_RESULT,
  });
  const [currentMatchIdx, setCurrentMatchIdx] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const searchTimerRef = useRef<number | null>(null);
  const regexRequestIdRef = useRef(0);

  const openSearch = useCallback(() => {
    setSearchOpen(true);
    window.setTimeout(() => {
      inputRef.current?.focus();
      inputRef.current?.select();
    }, 0);
  }, []);

  useEffect(() => {
    if (!searchRequestPending) return;
    openSearch();
    onSearchRequestConsumed?.();
  }, [onSearchRequestConsumed, openSearch, searchRequestPending]);

  const literalSearchResult = useMemo(
    () => regexEnabled
      ? EMPTY_SEARCH_RESULT
      : findCodeMatches(lines, searchQuery, caseSensitive, wholeWord),
    [caseSensitive, lines, regexEnabled, searchQuery, wholeWord],
  );
  const regexStateIsCurrent =
    regexSearchState.source === value
    && regexSearchState.query === searchQuery
    && regexSearchState.caseSensitive === caseSensitive
    && regexSearchState.wholeWord === wholeWord;
  const regexSearchPending = Boolean(
    regexEnabled
    && searchQuery
    && (!regexStateIsCurrent || regexSearchState.status === "pending"),
  );
  const regexSearchError = regexEnabled
    && regexStateIsCurrent
    && regexSearchState.status === "error"
    ? regexSearchState.error
    : undefined;
  const searchResult = regexEnabled
    ? regexStateIsCurrent && regexSearchState.status === "ready"
      ? regexSearchState.result
      : EMPTY_SEARCH_RESULT
    : literalSearchResult;
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
  const searchPending = query !== searchQuery || regexSearchPending;

  useEffect(() => {
    regexRequestIdRef.current += 1;
    const requestId = regexRequestIdRef.current;
    const baseState = {
      source: value,
      query: searchQuery,
      caseSensitive,
      wholeWord,
      result: EMPTY_SEARCH_RESULT,
    };

    if (!regexEnabled || !searchQuery) {
      setRegexSearchState({ ...baseState, status: "idle" });
      return;
    }
    if (searchQuery.length > MAX_REGEX_PATTERN_LENGTH) {
      setRegexSearchState({ ...baseState, status: "error", error: "pattern_too_long" });
      return;
    }
    if (value.length > MAX_REGEX_SOURCE_LENGTH) {
      setRegexSearchState({ ...baseState, status: "error", error: "source_too_large" });
      return;
    }

    setRegexSearchState({ ...baseState, status: "pending" });
    return startRegexSearch(
      {
        requestId,
        source: value,
        pattern: searchQuery,
        caseSensitive,
        wholeWord,
        maxMatches: MAX_SEARCH_MATCHES,
      },
      {
        onResponse: (response) => {
          if (response.requestId !== regexRequestIdRef.current) return;
          if (response.ok) {
            setRegexSearchState({ ...baseState, status: "ready", result: response.result });
          } else {
            setRegexSearchState({
              ...baseState,
              status: "error",
              error: response.error,
              detail: response.detail,
            });
          }
        },
      },
    );
  }, [caseSensitive, regexEnabled, searchQuery, value, wholeWord]);

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
  const bounded = scrollMode === "bounded" || (scrollMode !== "expand" && maxHeight != null) || isVirtual;
  const virtualizer = useVirtualizer({
    count: isVirtual ? lines.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT_ESTIMATE,
    overscan: OVERSCAN,
    // Syntax-highlighted rows can still require measurement when the user
    // changes typography, but measurement/scroll updates should not feed a
    // React render loop for a long code block.
    directDomUpdates: true,
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
    const hasSettledSearch = searchQuery && !searchPending && !regexSearchError;
    const isCurrent = hasSettledSearch && activeMatch?.lineIndex === index;
    const isDimmed = hasSettledSearch && !matchesByLine.has(index);
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
  const searchErrorLabel = (() => {
    switch (regexSearchError) {
      case "invalid_pattern":
        return t("workspace.searchRegexInvalid");
      case "pattern_too_long":
        return t("workspace.searchRegexTooLong");
      case "source_too_large":
        return t("workspace.searchRegexSourceTooLarge");
      case "zero_length_unsupported":
        return t("workspace.searchRegexZeroLength");
      case "multiline_unsupported":
        return t("workspace.searchRegexMultiline");
      case "timeout":
        return t("workspace.searchRegexTimeout");
      case "unavailable":
        return t("workspace.searchRegexUnavailable");
      default:
        return "";
    }
  })();

  return (
    <div
      className="code-block__wrap"
      onKeyDownCapture={(event) => {
        if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
          event.preventDefault();
          event.stopPropagation();
          openSearch();
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
          <span className="code-search__icon" aria-hidden="true">
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
              <circle cx="6.5" cy="6.5" r="5" />
              <path d="M10.5 10.5L14 14" />
            </svg>
          </span>
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
            <span
              className={`code-search__count${searchErrorLabel ? " code-search__count--error" : ""}`}
              aria-live="polite"
              title={searchErrorLabel ? regexSearchState.detail || searchErrorLabel : undefined}
            >
              {searchPending
                ? t("common.loading")
                : searchErrorLabel
                  ? searchErrorLabel
                : totalMatches > 0
                  ? t("workspace.searchCount", {
                    current: activeMatchIndex + 1,
                    total: totalMatchLabel,
                  })
                  : t("workspace.searchNoResults")}
            </span>
          )}

          <div className="code-search__actions">
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
              className={`code-search__toggle${regexEnabled ? " code-search__toggle--on" : ""}`}
              onClick={() => {
                setCurrentMatchIdx(0);
                setRegexEnabled((enabled) => !enabled);
              }}
              aria-label={t("workspace.searchRegex")}
              aria-pressed={regexEnabled}
              title={t("workspace.searchRegex")}
              type="button"
            >
              .*
            </button>

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

            <CopyButton
              text={value}
              className="code-search__copy"
              showInlineLabel={false}
            />
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
        </div>
      )}

      <div
        ref={scrollRef}
        className={`code hljs code--lines${bounded ? " code--scroll-y" : ""}`}
        data-nested-scroll={bounded ? "" : undefined}
        data-lang={language}
        data-highlight-mode={syntaxHighlight ? "syntax" : "plain"}
        tabIndex={0}
        style={{
          maxHeight: maxHeight ?? undefined,
          overflow: bounded ? "auto" : undefined,
        }}
      >
        {isVirtual ? (
          <div
            ref={virtualizer.containerRef}
            className="code-lines-wrap"
            style={{ width: "100%", position: "relative" }}
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
      {!searchOpen && <CopyButton text={value} className="code-block__copy" />}
    </div>
  );
}

function escapeHtml(value: string): string {
  return value.replace(/[&<>]/g, (character) => (
    character === "&" ? "&amp;" : character === "<" ? "&lt;" : "&gt;"
  ));
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
