import { Box } from "ink";
import React, { useContext } from "react";

/** Settled cards (in scrollback) drop border + padding + margin so history collapses to flat lines. */
export const ActiveCardContext = React.createContext(true);

export interface CardProps {
  tone: string;
  children: React.ReactNode;
}

const STRIPE_BORDER = {
  topLeft: " ",
  top: " ",
  topRight: " ",
  left: "▎",
  right: " ",
  bottomLeft: " ",
  bottom: " ",
  bottomRight: " ",
} as const;

export function Card({ tone, children }: CardProps): React.ReactElement {
  const active = useContext(ActiveCardContext);
  if (!active) {
    return <Box flexDirection="column">{children}</Box>;
  }
  return (
    <Box
      flexDirection="column"
      borderStyle={STRIPE_BORDER}
      borderColor={tone}
      borderTop={false}
      borderRight={false}
      borderBottom={false}
      paddingLeft={1}
      marginTop={1}
      width="100%"
    >
      {children}
    </Box>
  );
}
