import React, { type PropsWithChildren, useContext, useInsertionEffect } from 'react';
import instances from '../instances.js';
import {
  DISABLE_MOUSE_TRACKING,
  ENABLE_MOUSE_TRACKING,
  ENTER_ALT_SCREEN,
  EXIT_ALT_SCREEN,
} from '../termio/dec.js';
import { TerminalWriteContext } from '../useTerminalNotification.js';
import Box from './Box.js';
import { TerminalSizeContext } from './TerminalSizeContext.js';

/** CSI sequence that clears the alt-screen and parks the cursor at home. */
const CLEAR_AND_HOME = '\x1B[2J\x1B[H';

const FALLBACK_ROWS = 24;

type Props = PropsWithChildren<{
  mouseTracking?: boolean;
}>;

export function AlternateScreen({ children, mouseTracking = true }: Props) {
  const size = useContext(TerminalSizeContext);
  const writeRaw = useContext(TerminalWriteContext);

  useInsertionEffect(() => {
    const ink = instances.get(process.stdout);
    if (!writeRaw) return;

    writeRaw(
      ENTER_ALT_SCREEN + CLEAR_AND_HOME + (mouseTracking ? ENABLE_MOUSE_TRACKING : ''),
    );
    ink?.setAltScreenActive(true, mouseTracking);

    return () => {
      ink?.setAltScreenActive(false);
      ink?.clearTextSelection();
      writeRaw((mouseTracking ? DISABLE_MOUSE_TRACKING : '') + EXIT_ALT_SCREEN);
    };
  }, [writeRaw, mouseTracking]);

  const rows = size?.rows ?? FALLBACK_ROWS;

  return (
    <Box flexDirection="column" height={rows} width="100%" flexShrink={0}>
      {children}
    </Box>
  );
}
