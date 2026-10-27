import { useEffect, useRef } from "react";

interface Args {
  settings: boolean;
  feedback: boolean;
  panesRead: boolean;
  panes: number;
  root: string | undefined;
  openPane: (req: { root?: string }) => Promise<unknown>;
  fail: (e: unknown) => void;
}

// Settings and feedback are answered by a pane's kernel, so with none open
// there is nothing to ask. Opening either then starts a session in the current
// folder, or in the app's own workspace when there is no folder.
export function usePaneForSheets(a: Args) {
  const started = useRef(false);
  const wants = a.settings || a.feedback;
  const { panes, root, panesRead } = a;
  const latest = useRef(a);
  latest.current = a;
  useEffect(() => {
    if (!wants || !panesRead || panes > 0) {
      started.current = false;
      return;
    }
    if (started.current) return;
    started.current = true;
    void latest.current.openPane({ root }).catch(latest.current.fail);
  }, [wants, panesRead, panes, root]);
}
