import { createContext, useCallback, useContext, useMemo } from 'react';
import { isProgressReportingAvailable, type Progress } from './terminal.js';
import { BEL } from './termio/ansi.js';
import { ITERM2, OSC, osc, PROGRESS, wrapForMultiplexer } from './termio/osc.js';

type WriteRaw = (data: string) => void;

export const TerminalWriteContext = createContext<WriteRaw | null>(null);

export const TerminalWriteProvider = TerminalWriteContext.Provider;

export type TerminalNotification = {
  notifyITerm2: (opts: { message: string; title?: string }) => void;
  notifyKitty: (opts: { message: string; title: string; id: number }) => void;
  notifyGhostty: (opts: { message: string; title: string }) => void;
  notifyBell: () => void;
  /** Report progress to the terminal via OSC 9;4. */
  progress: (state: Progress['state'] | null, percentage?: number) => void;
};

export function useTerminalNotification(): TerminalNotification {
  const writeRaw = useContext(TerminalWriteContext);
  if (!writeRaw) {
    throw new Error(
      'useTerminalNotification must be used within TerminalWriteProvider',
    );
  }

  const notifyITerm2 = useCallback(
    ({ message, title }: { message: string; title?: string }) => {
      const displayString = title ? `${title}:\n${message}` : message;
      writeRaw(wrapForMultiplexer(osc(OSC.ITERM2, `\n\n${displayString}`)));
    },
    [writeRaw],
  );

  const notifyKitty = useCallback(
    ({
      message,
      title,
      id,
    }: {
      message: string;
      title: string;
      id: number;
    }) => {
      // Kitty's notification protocol is three-part: set the title, set the
      // body, then commit with the focus action. The `i=` id ties the parts
      // together; `d=0` means "more parts coming", `d=1` means commit.
      writeRaw(wrapForMultiplexer(osc(OSC.KITTY, `i=${id}:d=0:p=title`, title)));
      writeRaw(wrapForMultiplexer(osc(OSC.KITTY, `i=${id}:p=body`, message)));
      writeRaw(wrapForMultiplexer(osc(OSC.KITTY, `i=${id}:d=1:a=focus`, '')));
    },
    [writeRaw],
  );

  const notifyGhostty = useCallback(
    ({ message, title }: { message: string; title: string }) => {
      writeRaw(wrapForMultiplexer(osc(OSC.GHOSTTY, 'notify', title, message)));
    },
    [writeRaw],
  );

  const notifyBell = useCallback(() => {
    // Emit raw BEL with no multiplexer wrap. Inside tmux this triggers the
    // bell-action (window flag), which is useful as a fallback for callers
    // that can't enumerate the host terminal's notification protocol.
    // Wrapping would turn BEL into an opaque DCS payload and lose that.
    writeRaw(BEL);
  }, [writeRaw]);

  const progress = useCallback(
    (state: Progress['state'] | null, percentage?: number) => {
      if (!isProgressReportingAvailable()) {
        return;
      }
      if (!state) {
        writeRaw(
          wrapForMultiplexer(
            osc(OSC.ITERM2, ITERM2.PROGRESS, PROGRESS.CLEAR, ''),
          ),
        );
        return;
      }
      const pct = Math.max(0, Math.min(100, Math.round(percentage ?? 0)));
      switch (state) {
        case 'completed':
          writeRaw(
            wrapForMultiplexer(
              osc(OSC.ITERM2, ITERM2.PROGRESS, PROGRESS.CLEAR, ''),
            ),
          );
          break;
        case 'error':
          writeRaw(
            wrapForMultiplexer(
              osc(OSC.ITERM2, ITERM2.PROGRESS, PROGRESS.ERROR, pct),
            ),
          );
          break;
        case 'indeterminate':
          writeRaw(
            wrapForMultiplexer(
              osc(OSC.ITERM2, ITERM2.PROGRESS, PROGRESS.INDETERMINATE, ''),
            ),
          );
          break;
        case 'running':
          writeRaw(
            wrapForMultiplexer(
              osc(OSC.ITERM2, ITERM2.PROGRESS, PROGRESS.SET, pct),
            ),
          );
          break;
      }
    },
    [writeRaw],
  );

  return useMemo(
    () => ({ notifyITerm2, notifyKitty, notifyGhostty, notifyBell, progress }),
    [notifyITerm2, notifyKitty, notifyGhostty, notifyBell, progress],
  );
}
