import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
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
  return <MarkdownLines lines={markdownToLines(text)} />;
}

export function MarkdownLines({
  lines,
}: {
  lines: ReadonlyArray<MdLine>;
}): React.ReactElement {
  return (
    <Box flexDirection="column">
      {lines.map((line, i) => (
        <LineRow key={`md-${i}-${line.kind}`} line={line} />
      ))}
    </Box>
  );
}

function LineRow({ line }: { line: MdLine }): React.ReactElement | null {
  switch (line.kind) {
    case "blank":
      return <Text> </Text>;
    case "hr":
      return <Text color={FG_FAINT}>──────</Text>;
    case "heading":
      return (
        <Box>
          <Text bold color={FG_STRONG}>
            {`${"#".repeat(line.level)} `}
          </Text>
          <Spans spans={line.spans} bold strongColor />
        </Box>
      );
    case "paragraph":
      return (
        <Box>
          <Spans spans={line.spans} />
        </Box>
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
        <Box>
          <Text color={markerColor}>{`${indent}${marker} `}</Text>
          <Spans spans={line.spans} dim={line.task === "done"} strike={line.task === "done"} />
        </Box>
      );
    }
    case "code":
      return <CodeBlock lang={line.lang} text={line.text} />;
    case "blockquote":
      return (
        <Box>
          <Text color={TONE_BRAND}>{"▎ "}</Text>
          <Spans spans={line.spans} italic />
        </Box>
      );
  }
}

function spanKey(span: InlineSpan, i: number): string {
  return `${i}-${span.text.length}-${span.bold ? "b" : ""}${span.italic ? "i" : ""}${span.code ? "c" : ""}${span.strike ? "s" : ""}${span.link ? "l" : ""}`;
}

function CodeBlock({ lang, text }: { lang: string; text: string }): React.ReactElement {
  const lines = text.split("\n");
  return (
    <Box flexDirection="column">
      {lang.length > 0 ? <Text color={FG_META}>{` ${lang}`}</Text> : null}
      {lines.map((ln, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: code lines are positional + stable per render
        <Text key={`code-${i}`} backgroundColor={SURFACE_ELEV}>
          {` ${ln} `}
        </Text>
      ))}
    </Box>
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
  if (spans.length === 0) return <Text> </Text>;
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
      <Text color={FG_STRONG} backgroundColor={SURFACE_ELEV}>
        {` ${span.text} `}
      </Text>
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
    <Text
      color={color}
      bold={!!(span.bold || ambientBold)}
      italic={!!(span.italic || ambientItalic)}
      dimColor={!!ambientDim}
      strikethrough={!!(span.strike || ambientStrike)}
      underline={!!(span.link || span.fileRef)}
    >
      {span.text}
    </Text>
  );
}
