// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { inkCompat } from "../../renderer/index.js";
import { type InlineSpan, type MdLine, markdownToLines } from "./markdown-lines.js";

const FG_BODY = "#c9d1d9";
const FG_FAINT = "#6e7681";
const FG_STRONG = "#f0f6fc";
const FG_META = "#8b949e";
const TONE_BRAND = "#79c0ff";
const TONE_OK = "#7ee787";
const TONE_WARN = "#f0b07d";
const SURFACE_ELEV = "#161b22";

export function MarkdownView({ text }: { text: string }): React.ReactElement {
  const lines = React.useMemo(() => markdownToLines(text), [text]);
  return <MarkdownLines lines={lines} />;
}

export function MarkdownLines({
  lines,
}: {
  lines: ReadonlyArray<MdLine>;
}): React.ReactElement {
  return (
    <inkCompat.Box flexDirection="column">
      {lines.map((line, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: lines are positional + stable per render
        <LineRow key={`md-${i}-${line.kind}`} line={line} />
      ))}
    </inkCompat.Box>
  );
}

function LineRow({ line }: { line: MdLine }): React.ReactElement | null {
  switch (line.kind) {
    case "blank":
      return <inkCompat.Text> </inkCompat.Text>;
    case "hr":
      return <inkCompat.Text color={FG_FAINT}>──────</inkCompat.Text>;
    case "heading":
      return (
        <inkCompat.Box>
          <inkCompat.Text bold color={FG_STRONG}>
            {`${"#".repeat(line.level)} `}
          </inkCompat.Text>
          <Spans spans={line.spans} bold strongColor />
        </inkCompat.Box>
      );
    case "paragraph":
      return (
        <inkCompat.Box>
          <Spans spans={line.spans} />
        </inkCompat.Box>
      );
    case "list": {
      const indent = " ".repeat(line.depth * 2);
      const marker =
        line.task === "done"
          ? "✓"
          : line.task === "todo"
            ? "○"
            : line.ordered
              ? `${line.index}.`
              : "·";
      const markerColor =
        line.task === "done" ? TONE_OK : line.task === "todo" ? FG_FAINT : FG_META;
      return (
        <inkCompat.Box>
          <inkCompat.Text color={markerColor}>{`${indent}${marker} `}</inkCompat.Text>
          <Spans spans={line.spans} dim={line.task === "done"} strike={line.task === "done"} />
        </inkCompat.Box>
      );
    }
    case "code":
      return <CodeBlock lang={line.lang} text={line.text} />;
    case "blockquote":
      return (
        <inkCompat.Box>
          <inkCompat.Text color={TONE_BRAND}>{"▎ "}</inkCompat.Text>
          <Spans spans={line.spans} italic />
        </inkCompat.Box>
      );
  }
}

function spanKey(span: InlineSpan, i: number): string {
  return `${i}-${span.text.length}-${span.bold ? "b" : ""}${span.italic ? "i" : ""}${span.code ? "c" : ""}${span.strike ? "s" : ""}${span.link ? "l" : ""}`;
}

function CodeBlock({ lang, text }: { lang: string; text: string }): React.ReactElement {
  const lines = text.split("\n");
  return (
    <inkCompat.Box flexDirection="column">
      {lang.length > 0 ? <inkCompat.Text color={FG_META}>{` ${lang}`}</inkCompat.Text> : null}
      {lines.map((ln, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: code lines are positional + stable per render
        <inkCompat.Text key={`code-${i}`} backgroundColor={SURFACE_ELEV}>
          {` ${ln} `}
        </inkCompat.Text>
      ))}
    </inkCompat.Box>
  );
}

interface SpansProps {
  readonly spans: ReadonlyArray<InlineSpan>;
  readonly bold?: boolean;
  readonly italic?: boolean;
  readonly dim?: boolean;
  readonly strike?: boolean;
  readonly strongColor?: boolean;
}

function Spans({ spans, bold, italic, dim, strike, strongColor }: SpansProps): React.ReactElement {
  if (spans.length === 0) return <inkCompat.Text> </inkCompat.Text>;
  return (
    <>
      {spans.map((span, i) => (
        <SpanText
          key={spanKey(span, i)}
          span={span}
          ambientBold={bold}
          ambientItalic={italic}
          ambientDim={dim}
          ambientStrike={strike}
          strongColor={strongColor}
        />
      ))}
    </>
  );
}

function SpanText({
  span,
  ambientBold,
  ambientItalic,
  ambientDim,
  ambientStrike,
  strongColor,
}: {
  span: InlineSpan;
  ambientBold?: boolean;
  ambientItalic?: boolean;
  ambientDim?: boolean;
  ambientStrike?: boolean;
  strongColor?: boolean;
}): React.ReactElement {
  if (span.code) {
    return (
      <inkCompat.Text color={FG_STRONG} backgroundColor={SURFACE_ELEV}>
        {` ${span.text} `}
      </inkCompat.Text>
    );
  }
  const color = span.fileRef
    ? TONE_BRAND
    : span.link
      ? TONE_BRAND
      : strongColor
        ? FG_STRONG
        : FG_BODY;
  return (
    <inkCompat.Text
      color={color}
      bold={!!(span.bold || ambientBold)}
      italic={!!(span.italic || ambientItalic)}
      dimColor={!!ambientDim}
      strikethrough={!!(span.strike || ambientStrike)}
      underline={!!(span.link || span.fileRef)}
    >
      {span.text}
    </inkCompat.Text>
  );
}
