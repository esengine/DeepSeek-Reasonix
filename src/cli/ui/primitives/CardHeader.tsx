import { Box, Text } from "ink";
import React from "react";
import { FG } from "../theme/tokens.js";

export type MetaItem = string | { text: string; color: string };

export interface CardHeaderProps {
  glyph: string;
  tone: string;
  title: string;
  /** Override the default tone-colored bold title (e.g. demoted cards use FG.sub). */
  titleColor?: string;
  /** When set, render the title as a backgrounded pill (e.g. `▎ ◆  reasoning  ` with a tinted block). */
  titleBg?: string;
  /** Body-tone text after the title, separated by a space (no `·`). */
  subtitle?: string;
  /** Faint trailing fields, prefixed with ` · ` and joined by ` · `. */
  meta?: ReadonlyArray<MetaItem>;
  /** Inline ad-hoc element after meta — for spinners, badges, anything outside the meta vocabulary. */
  right?: React.ReactNode;
}

export function CardHeader({
  glyph,
  tone,
  title,
  titleColor,
  titleBg,
  subtitle,
  meta,
  right,
}: CardHeaderProps): React.ReactElement {
  return (
    <Box flexDirection="row" gap={1}>
      <Text color={tone}>{glyph}</Text>
      {titleBg ? (
        <Text backgroundColor={titleBg} color={titleColor ?? tone} bold>
          {` ${title} `}
        </Text>
      ) : (
        <Text bold color={titleColor ?? tone}>
          {title}
        </Text>
      )}
      {subtitle ? <Text color={FG.body}>{subtitle}</Text> : null}
      {meta?.map((item, i) => {
        const isStr = typeof item === "string";
        const text = isStr ? item : item.text;
        const color = isStr ? FG.faint : item.color;
        return (
          // biome-ignore lint/suspicious/noArrayIndexKey: meta items are positional
          <React.Fragment key={`m-${i}`}>
            <Text color={FG.faint}>·</Text>
            <Text color={color}>{text}</Text>
          </React.Fragment>
        );
      })}
      {right}
    </Box>
  );
}
