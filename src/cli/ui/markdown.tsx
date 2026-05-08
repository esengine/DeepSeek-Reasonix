/** Markdown → Ink. Parsing via marked; visual mapping mirrors dashboard/app.css `.md` rules. Code blocks pass through cli-highlight for ANSI syntax coloring. */

import { highlight, supportsLanguage } from "cli-highlight";
import { Box, Text, useStdout } from "ink";
import { type Token, type Tokens, marked } from "marked";
import React from "react";
import stringWidth from "string-width";
import { wrapToCells } from "../../frame/width.js";
import { FG, SURFACE, TONE } from "./theme/tokens.js";

/** Left margin consumed by card outer marginLeft + body paddingLeft + safety. */
const BODY_LEFT_CELLS = 7;

const MarkdownWidthCtx = React.createContext<number | undefined>(undefined);

function useWidth(): number {
  const ctx = React.useContext(MarkdownWidthCtx);
  if (ctx !== undefined) return ctx;
  return (useStdout()?.stdout?.columns ?? process.stdout.columns ?? 80) - BODY_LEFT_CELLS;
}

marked.setOptions({ gfm: true, breaks: false });

export function Markdown({ text, width }: { text: string; width?: number }): React.ReactElement {
  const tokens = React.useMemo(() => marked.lexer(text), [text]);
  const ctxWidth = width !== undefined ? Math.max(1, width) : undefined;
  return (
    <MarkdownWidthCtx.Provider value={ctxWidth}>
      <Box flexDirection="column" gap={1}>
        {tokens.map((token, i) => (
          <BlockToken key={`${i}-${token.type}`} token={token} />
        ))}
      </Box>
    </MarkdownWidthCtx.Provider>
  );
}

function BlockToken({ token }: { token: Token }): React.ReactElement | null {
  switch (token.type) {
    case "heading":
      return <Heading token={token as Tokens.Heading} />;
    case "paragraph":
      return <Paragraph token={token as Tokens.Paragraph} />;
    case "list":
      return <List token={token as Tokens.List} depth={0} />;
    case "code":
      return <CodeBlock token={token as Tokens.Code} />;
    case "blockquote":
      return <Blockquote token={token as Tokens.Blockquote} />;
    case "hr":
      return <HorizontalRule />;
    case "table":
      return <Table token={token as Tokens.Table} />;
    case "html":
      return <Text color={FG.body}>{(token as Tokens.HTML).text}</Text>;
    case "space":
      return null;
    default:
      return <Text color={FG.body}>{(token as { raw?: string }).raw ?? ""}</Text>;
  }
}

function Heading({ token }: { token: Tokens.Heading }): React.ReactElement {
  return (
    <Box>
      <Text bold color={FG.strong} backgroundColor={SURFACE.bgElev}>
        {` ${plainText(token.tokens)} `}
      </Text>
    </Box>
  );
}

function Paragraph({ token }: { token: Tokens.Paragraph }): React.ReactElement {
  return (
    <Text color={FG.body}>
      <Inline tokens={token.tokens ?? []} />
    </Text>
  );
}

function List({ token, depth }: { token: Tokens.List; depth: number }): React.ReactElement {
  return (
    <Box flexDirection="column">
      {token.items.map((item, i) => (
        <ListItem
          key={`${i}-${item.text.slice(0, 24)}`}
          item={item}
          ordered={token.ordered}
          index={i + (Number(token.start) || 1)}
          depth={depth}
        />
      ))}
    </Box>
  );
}

function ListItem({
  item,
  ordered,
  index,
  depth,
}: {
  item: Tokens.ListItem;
  ordered: boolean;
  index: number;
  depth: number;
}): React.ReactElement {
  const marker = item.task ? (item.checked ? "✓" : "○") : ordered ? `${index}.` : "·";
  const markerColor = item.task ? (item.checked ? TONE.ok : FG.faint) : FG.meta;
  const dim = item.task && item.checked === true;
  const indent = " ".repeat(depth + 1);
  return (
    <Box>
      <Text color={markerColor}>{`${indent}${marker} `}</Text>
      <Box flexDirection="column">
        {item.tokens.map((tok, i) => {
          if (tok.type === "text") {
            const inner = (tok as Tokens.Text).tokens;
            return (
              // biome-ignore lint/suspicious/noArrayIndexKey: list-item children are positional and stable per render
              <Text key={`t-${i}`} color={dim ? FG.faint : FG.body} strikethrough={dim}>
                {inner ? <Inline tokens={inner} /> : (tok as Tokens.Text).text}
              </Text>
            );
          }
          if (tok.type === "list") {
            // biome-ignore lint/suspicious/noArrayIndexKey: list-item children are positional and stable per render
            return <List key={`l-${i}`} token={tok as Tokens.List} depth={depth + 1} />;
          }
          return <BlockToken key={`b-${i}-${tok.type}`} token={tok} />;
        })}
      </Box>
    </Box>
  );
}

function CodeBlock({ token }: { token: Tokens.Code }): React.ReactElement {
  const lang = token.lang?.split(/\s+/)[0] ?? "";
  const colored = highlightCode(token.text, lang);
  const lines = colored.split("\n");
  return (
    <Box flexDirection="column">
      {lang ? (
        <Box>
          <Text color={FG.meta}>{` ${lang}`}</Text>
        </Box>
      ) : null}
      <Box flexDirection="column">
        {lines.map((line, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: code lines are positional and stable per render
          <Text key={`code-${i}`} backgroundColor={SURFACE.bgElev}>
            {` ${line} `}
          </Text>
        ))}
      </Box>
    </Box>
  );
}

function highlightCode(source: string, lang: string): string {
  if (!lang) return source;
  try {
    if (supportsLanguage(lang)) return highlight(source, { language: lang, ignoreIllegals: true });
    return highlight(source, { ignoreIllegals: true });
  } catch {
    return source;
  }
}

function Blockquote({ token }: { token: Tokens.Blockquote }): React.ReactElement {
  return (
    <Box flexDirection="column">
      {(token.tokens ?? []).map((child, i) => (
        <Box key={`${i}-${child.type}`} flexDirection="row">
          <Text color={TONE.brand}>{" ▎ "}</Text>
          <Box flexDirection="column" flexGrow={1}>
            {child.type === "paragraph" ? (
              <Text italic color={FG.sub}>
                <Inline tokens={(child as Tokens.Paragraph).tokens ?? []} />
              </Text>
            ) : (
              <BlockToken token={child} />
            )}
          </Box>
        </Box>
      ))}
    </Box>
  );
}

/** Right-pad to `cells` visual columns — wide chars (CJK, emoji) count as 2. */
function padToCells(text: string, cells: number): string {
  const w = stringWidth(text);
  if (w >= cells) return text;
  return text + " ".repeat(cells - w);
}

function HorizontalRule(): React.ReactElement {
  const width = useWidth();
  const rule = "─".repeat(Math.max(width, 1));
  return <Text color={FG.faint}>{` ${rule}`}</Text>;
}

/** Pure function — no React deps. */
export function tableLayout(
  headerCells: string[],
  bodyCells: string[][],
  availableWidth: number,
): ColumnarLayout | FallbackLayout {
  const colCount = headerCells.length;
  const GAP = " ";
  const GAP_W = stringWidth(GAP);
  const widths = new Array<number>(colCount).fill(0);
  for (let c = 0; c < colCount; c++) {
    widths[c] = Math.max(
      stringWidth(headerCells[c] ?? ""),
      ...bodyCells.map((r) => stringWidth(r[c] ?? "")),
    );
  }
  const totalWidth = widths.reduce((s, w) => s + w, 0) + GAP_W * (colCount - 1);
  if (totalWidth <= availableWidth) {
    return { fallback: false, widths, colCount, gap: GAP };
  }
  // Fallback: key/value pairs, label column = widest header, value gets the rest.
  const rawLabel = Math.max(...headerCells.map((h) => stringWidth(h))) + 2; // label + ": "
  const labelPad = Math.min(rawLabel, availableWidth - 1);
  const valueCells = availableWidth - labelPad;
  return { fallback: true, labelPad, valueCells };
}

interface ColumnarLayout {
  fallback: false;
  widths: number[];
  colCount: number;
  gap: string;
}
interface FallbackLayout {
  fallback: true;
  labelPad: number;
  valueCells: number;
}

function Table({ token }: { token: Tokens.Table }): React.ReactElement {
  const width = useWidth();
  const headerCells = token.header.map((c) => plainText(c.tokens));
  const bodyCells = token.rows.map((row) => row.map((c) => plainText(c.tokens)));
  const layout = tableLayout(headerCells, bodyCells, width);
  if (!layout.fallback)
    return (
      <ColumnarTable
        headerCells={headerCells}
        bodyCells={bodyCells}
        widths={layout.widths}
        colCount={headerCells.length}
        gap={layout.gap}
      />
    );
  return (
    <FallbackTable
      headerCells={headerCells}
      bodyCells={bodyCells}
      labelPad={layout.labelPad}
      valueCells={layout.valueCells}
    />
  );
}

function ColumnarTable({
  headerCells,
  bodyCells,
  widths,
  colCount,
  gap,
}: {
  headerCells: string[];
  bodyCells: string[][];
  widths: number[];
  colCount: number;
  gap: string;
}): React.ReactElement {
  const ruleRow = widths.map((w) => "─".repeat(w)).join(gap);
  return (
    <Box flexDirection="column">
      <Box>
        <Text> </Text>
        {headerCells.map((cell, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: header cells positional
          <React.Fragment key={`h-${i}`}>
            <Text bold color={FG.sub}>
              {padToCells(cell, widths[i]!)}
            </Text>
            {i < colCount - 1 ? <Text>{gap}</Text> : null}
          </React.Fragment>
        ))}
      </Box>
      <Box>
        <Text> </Text>
        <Text color={FG.faint}>{ruleRow}</Text>
      </Box>
      {bodyCells.map((row, ri) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: body rows positional
        <Box key={`tr-${ri}`}>
          <Text> </Text>
          {row.map((cell, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: cells positional
            <React.Fragment key={`c-${ri}-${i}`}>
              <Text color={FG.body}>{padToCells(cell ?? "", widths[i]!)}</Text>
              {i < colCount - 1 ? <Text>{gap}</Text> : null}
            </React.Fragment>
          ))}
        </Box>
      ))}
    </Box>
  );
}

function FallbackTable({
  headerCells,
  bodyCells,
  labelPad,
  valueCells,
}: {
  headerCells: string[];
  bodyCells: string[][];
  labelPad: number;
  valueCells: number;
}): React.ReactElement {
  return (
    <Box flexDirection="column">
      {bodyCells.map((row, ri) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: body rows positional
        <Box key={`fr-${ri}`} flexDirection="column">
          {ri > 0 ? <Text> </Text> : null}
          {headerCells.map((h, ci) => {
            const label = `${padToCells(h, labelPad - 2)}: `;
            const lines = wrapToCells(row[ci] ?? "", valueCells);
            return lines.map((line, li) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: fallback table lines are positional
              <Box key={`fc-${ri}-${ci}-${li}`}>
                {li === 0 ? (
                  <Text bold color={FG.sub}>
                    {label}
                  </Text>
                ) : (
                  <Text>{padToCells("", labelPad)}</Text>
                )}
                <Text color={FG.body}>{line}</Text>
              </Box>
            ));
          })}
        </Box>
      ))}
    </Box>
  );
}

function Inline({ tokens }: { tokens: Token[] }): React.ReactElement {
  return (
    <>
      {tokens.map((tok, i) => (
        <InlineToken key={`${i}-${tok.type}`} token={tok} />
      ))}
    </>
  );
}

const FILE_REF_RE = /\b([A-Za-z0-9_./@\-]+\.[A-Za-z0-9]{1,6})(?::(\d+)(?:-(\d+))?)?\b/g;
const MENTION_RE = /(?<![A-Za-z0-9_])@([A-Za-z0-9_./\-]+\.[A-Za-z0-9]{1,6})/g;

function looksLikeFileRef(path: string, hasLine: boolean): boolean {
  if (hasLine) return true;
  if (path.includes("/")) return true;
  const ext = path.split(".").pop() ?? "";
  return ext.length >= 2;
}

function osc8(label: string, _target: string, color: string): React.ReactElement {
  return (
    <Text color={color} underline>
      {label}
    </Text>
  );
}

function renderInlineText(raw: string): React.ReactElement {
  if (!raw) return <Text>{raw}</Text>;
  const out: React.ReactNode[] = [];
  let cursor = 0;
  type Hit = { start: number; end: number; node: React.ReactElement };
  const hits: Hit[] = [];
  for (const m of raw.matchAll(MENTION_RE)) {
    const start = m.index ?? 0;
    const end = start + m[0].length;
    const path = m[1]!;
    hits.push({
      start,
      end,
      node: <Text color={TONE.warn} underline>{`@${path}`}</Text>,
    });
  }
  for (const m of raw.matchAll(FILE_REF_RE)) {
    const start = m.index ?? 0;
    const end = start + m[0].length;
    if (hits.some((h) => start < h.end && end > h.start)) continue;
    const path = m[1]!;
    const line = m[2];
    if (!looksLikeFileRef(path, line !== undefined)) continue;
    const target = line ? `file://${path}:${line}` : `file://${path}`;
    hits.push({ start, end, node: osc8(m[0], target, TONE.brand) });
  }
  hits.sort((a, b) => a.start - b.start);
  let key = 0;
  for (const h of hits) {
    if (h.start > cursor) {
      out.push(<Text key={`t-${key++}`}>{raw.slice(cursor, h.start)}</Text>);
    }
    out.push(<React.Fragment key={`r-${key++}`}>{h.node}</React.Fragment>);
    cursor = h.end;
  }
  if (cursor < raw.length) out.push(<Text key={`t-${key++}`}>{raw.slice(cursor)}</Text>);
  return <>{out}</>;
}

function InlineToken({ token }: { token: Token }): React.ReactElement {
  switch (token.type) {
    case "text": {
      const t = token as Tokens.Text;
      return t.tokens ? <Inline tokens={t.tokens} /> : renderInlineText(t.text);
    }
    case "strong":
      return (
        <Text bold color={FG.strong}>
          <Inline tokens={(token as Tokens.Strong).tokens} />
        </Text>
      );
    case "em":
      return (
        <Text italic>
          <Inline tokens={(token as Tokens.Em).tokens} />
        </Text>
      );
    case "codespan":
      return (
        <Text color={FG.strong} backgroundColor={SURFACE.bgElev}>
          {` ${(token as Tokens.Codespan).text} `}
        </Text>
      );
    case "del":
      return (
        <Text color={TONE.err} strikethrough>
          <Inline tokens={(token as Tokens.Del).tokens} />
        </Text>
      );
    case "link": {
      const l = token as Tokens.Link;
      return (
        <Text color={TONE.brand} underline>
          <Inline tokens={l.tokens} />
        </Text>
      );
    }
    case "image": {
      const im = token as Tokens.Image;
      return <Text color={TONE.brand}>{`[image: ${im.text || im.href}]`}</Text>;
    }
    case "br":
      return <Text>{"\n"}</Text>;
    case "escape":
      return <Text>{(token as Tokens.Escape).text}</Text>;
    case "html":
      return <Text>{(token as Tokens.HTML).text}</Text>;
    default:
      return <Text>{(token as { raw?: string }).raw ?? ""}</Text>;
  }
}

export function plainText(tokens: Token[] | undefined): string {
  if (!tokens) return "";
  let out = "";
  for (const t of tokens) {
    switch (t.type) {
      case "text":
        out += (t as Tokens.Text).text;
        break;
      case "strong":
      case "em":
      case "del":
      case "link":
        out += plainText((t as { tokens?: Token[] }).tokens ?? []);
        break;
      case "codespan":
        out += (t as Tokens.Codespan).text;
        break;
      case "br":
        out += "\n";
        break;
      case "escape":
        out += (t as Tokens.Escape).text;
        break;
      default:
        out += (t as { raw?: string }).raw ?? "";
    }
  }
  return out;
}
