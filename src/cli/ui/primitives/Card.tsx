import { Box } from "ink";
import React from "react";

export const ActiveCardContext = React.createContext(true);

export interface CardProps {
  /** Kept for API compatibility with CardHeader's tone arg; no longer drives a left stripe. */
  tone?: string;
  children: React.ReactNode;
}

export function Card({ children }: CardProps): React.ReactElement {
  return (
    <Box flexDirection="column" marginTop={1} width="100%">
      {children}
    </Box>
  );
}
