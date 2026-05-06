// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { type ReactNode } from "react";
import type { AnsiCode } from "../pools/style-pool.js";
import { Text as RsxText } from "../react/components.js";
import { bgCode, fgCode } from "./colors.js";

export interface InkTextProps {
  readonly children?: ReactNode;
  readonly color?: string;
  readonly backgroundColor?: string;
  readonly bold?: boolean;
  readonly dimColor?: boolean;
  readonly italic?: boolean;
  readonly underline?: boolean;
  readonly strikethrough?: boolean;
  readonly inverse?: boolean;
  readonly wrap?: "wrap" | "truncate" | "truncate-start" | "truncate-middle" | "truncate-end";
  /** OSC 8 hyperlink target — emitted as proper escape bytes, never inlined into cell content. */
  readonly hyperlink?: string;
}

const SGR = {
  bold: { apply: "\x1b[1m", revert: "\x1b[22m" },
  dim: { apply: "\x1b[2m", revert: "\x1b[22m" },
  italic: { apply: "\x1b[3m", revert: "\x1b[23m" },
  underline: { apply: "\x1b[4m", revert: "\x1b[24m" },
  inverse: { apply: "\x1b[7m", revert: "\x1b[27m" },
  strikethrough: { apply: "\x1b[9m", revert: "\x1b[29m" },
} as const;

export function Text(props: InkTextProps): React.ReactElement {
  const codes: AnsiCode[] = [];
  const fg = fgCode(props.color);
  if (fg) codes.push(fg);
  const bg = bgCode(props.backgroundColor);
  if (bg) codes.push(bg);
  if (props.bold) codes.push(SGR.bold);
  if (props.dimColor) codes.push(SGR.dim);
  if (props.italic) codes.push(SGR.italic);
  if (props.underline) codes.push(SGR.underline);
  if (props.inverse) codes.push(SGR.inverse);
  if (props.strikethrough) codes.push(SGR.strikethrough);
  return (
    <RsxText style={codes.length > 0 ? codes : undefined} hyperlink={props.hyperlink}>
      {props.children}
    </RsxText>
  );
}
