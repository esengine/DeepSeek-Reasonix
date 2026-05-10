import { useStdout } from "ink";
import { useEffect } from "react";

export function useTerminalSetup(mouse: boolean): void {
  const { stdout } = useStdout();
  useEffect(() => {
    if (!stdout || !stdout.isTTY) return;
    stdout.write("\u001b[?2004h");
    stdout.write("\u001b[>4;2m");
    // 1007 (alt-scroll) over full mouse tracking — keeps native drag-select intact.
    if (mouse) stdout.write("\u001b[?1007h");
    return () => {
      if (mouse) stdout.write("\u001b[?1007l");
      stdout.write("\u001b[?2004l");
      stdout.write("\u001b[>4m");
    };
  }, [stdout, mouse]);
}
