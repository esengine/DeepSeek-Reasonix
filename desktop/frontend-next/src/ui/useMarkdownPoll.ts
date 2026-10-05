import { useEffect, type RefObject } from "react";
import type { AgentPort, WorkspaceFile } from "../port/port";

/** Refresh the visible Markdown reader without replacing local edits. */
export function useMarkdownPoll({
  port, filePath, held, issued, setFailed, setFile, setDraft,
}: {
  port: AgentPort;
  filePath: string;
  held: RefObject<{ file: WorkspaceFile | null; draft: string }>;
  issued: RefObject<number>;
  setFailed: (error: string) => void;
  setFile: (file: WorkspaceFile) => void;
  setDraft: (content: string) => void;
}) {
  useEffect(() => {
    if (!filePath) return;
    let live = true;
    let reading = false;
    const timer = window.setInterval(() => {
      const before = held.current;
      if (reading || !before.file || before.file.path !== filePath || before.draft !== before.file.content) return;
      reading = true;
      const ticket = ++issued.current;
      void port.workspaceFile(filePath).then((next) => {
        const now = held.current;
        if (!live || ticket !== issued.current || now.file?.path !== filePath || now.draft !== now.file.content) return;
        setFailed("");
        if (next.content !== now.file.content || next.revision !== now.file.revision) {
          setFile(next);
          setDraft(next.content);
        }
      }, () => {
        // Atomic saves can briefly leave the file unreadable; retry next time.
      }).finally(() => { reading = false; });
    }, 3000);
    return () => { live = false; window.clearInterval(timer); };
  }, [port, filePath, held, issued, setFailed, setFile, setDraft]);
}
