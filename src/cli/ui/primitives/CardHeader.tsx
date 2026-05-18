import { Box, Text } from "ink";
import React, { useContext } from "react";
import { FG } from "../theme/tokens.js";
import { ActiveCardContext } from "./Card.js";

export type MetaItem = string | { text: string; color: string };

export interface CardHeaderProps {
  glyph: string;
  tone: string;
  title: string;
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
  subtitle,
  meta,
  right,
}: CardHeaderProps): React.ReactElement {
  const active = useContext(ActiveCardContext);
  const visibleMeta = active ? meta : meta?.filter((item) => typeof item !== "string");
  return (
    <Box flexDirection="row" gap={1}>
      <Text color={tone}>{glyph}</Text>
      <Text bold color={tone}>
        {title}
      </Text>
      {subtitle ? <Text color={FG.body}>{subtitle}</Text> : null}
      {visibleMeta?.map((item, i) => {
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
      {active ? right : null}
    </Box>
  );
}
