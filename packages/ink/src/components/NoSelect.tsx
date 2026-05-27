import React, { type PropsWithChildren } from 'react';
import Box, { type Props as BoxProps } from './Box.js';

type Props = Omit<BoxProps, 'noSelect'> & {
  fromLeftEdge?: boolean;
};

/** Marks a sub-tree as non-selectable in fullscreen text selection. */
export function NoSelect({ children, fromLeftEdge, ...boxProps }: PropsWithChildren<Props>) {
  const noSelect = fromLeftEdge ? 'from-left-edge' : true;
  return (
    <Box {...boxProps} noSelect={noSelect}>
      {children}
    </Box>
  );
}
