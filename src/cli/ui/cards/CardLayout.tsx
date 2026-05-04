/** Shared layout for the chat card stack. Header row + indented body. */

import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { type ReactNode } from "react";
import { FG } from "../theme/tokens.js";

export interface CardLayoutProps {
  /** Leading glyph (◆ ▸ ✓ ⊞ ± ⌬ etc.). */
  readonly glyph: string;
  /** Color used for both glyph and bold title. */
  readonly tone: string;
  /** Bold word right of the glyph — names the card kind. */
  readonly title: string;
  /** Optional faint inline meta — string is rendered in FG.faint with a leading `·`; pass a node for custom styling. */
  readonly meta?: ReactNode;
  /** Trailing inline element (spinner, badge, etc). Sits at the right of the header row. */
  readonly trailing?: ReactNode;
  /** Body content. Rendered with paddingLeft=2 below the header. */
  readonly children?: ReactNode;
}

/** marginTop=1 separates cards in the stack; paddingLeft=2 keeps body indented from the glyph column. */
const BODY_PAD = 2;

export function CardLayout({
  glyph,
  tone,
  title,
  meta,
  trailing,
  children,
}: CardLayoutProps): React.ReactElement {
  return (
    <Box flexDirection="column" marginTop={1}>
      <Box flexDirection="row" gap={1}>
        <Text color={tone}>{glyph}</Text>
        <Text color={tone} bold>
          {title}
        </Text>
        {renderMeta(meta)}
        {trailing}
      </Box>
      {children ? (
        <Box flexDirection="column" paddingLeft={BODY_PAD}>
          {children}
        </Box>
      ) : null}
    </Box>
  );
}

function renderMeta(meta: ReactNode): ReactNode {
  if (meta == null) return null;
  if (typeof meta === "string") {
    if (meta.length === 0) return null;
    return <Text color={FG.faint}>{meta.startsWith("·") ? meta : `· ${meta}`}</Text>;
  }
  return meta;
}
