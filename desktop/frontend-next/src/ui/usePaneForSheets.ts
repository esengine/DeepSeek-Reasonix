import { useEffect, useRef } from "react";
import { t } from "../i18n";

interface Args {
  settings: boolean;
  feedback: boolean;
  panesRead: boolean;
  panes: number;
  root: string | undefined;
  openPane: (req: { root: string }) => Promise<unknown>;
  refuse: (message: string) => void;
  fail: (e: unknown) => void;
  clear: (message: string) => void;
}

// Settings and feedback are answered by a pane's kernel, so with none open
// there is nothing to ask. Opening either then starts a session in the current
// folder; with no folder to start in, the refusal is shown rather than dropped
// and withdrawn once a folder or a pane exists.
export function usePaneForSheets(a: Args) {
  const started = useRef(false);
  const refused = useRef("");
  const wants = a.settings || a.feedback;
  const { panes, root, panesRead } = a;
  const latest = useRef(a);
  latest.current = a;
  useEffect(() => {
    if (!wants || !panesRead || panes > 0) {
      started.current = false;
      if (refused.current && (panes > 0 || root)) {
        latest.current.clear(refused.current);
        refused.current = "";
      }
      return;
    }
    if (started.current) return;
    started.current = true;
    if (!root) {
      const message = latest.current.settings
        ? t("设置需要一个打开的会话。请先在左栏添加一个文件夹。")
        : t("反馈需要一个打开的会话。请先在左栏添加一个文件夹。");
      refused.current = message;
      latest.current.refuse(message);
      return;
    }
    void latest.current.openPane({ root }).catch(latest.current.fail);
  }, [wants, panesRead, panes, root]);
}
