import { useMemo, type Dispatch, type ReactNode, type SetStateAction } from "react";
import type { AgentPort } from "../port/port";
import type { TreeWorkspace } from "../port/hub";
import { AutoArchiveNotice } from "./AutoArchiveNotice";
import { ErrorBar } from "./ErrorBar";
import { LevelUpNotice } from "./LevelUpNotice";

/** What the window says about itself. A refused action's reason is the error bar
 *  and takes the place of the passing notes, which fill it when there is none. */
export function useWindowNotices({ error, setError, onReport, tree, port, feedbackOpen }: {
  error: string;
  setError: Dispatch<SetStateAction<string>>;
  onReport: () => void;
  tree: TreeWorkspace[];
  port: AgentPort | null;
  feedbackOpen: boolean;
}): { errorBar: ReactNode; note: ReactNode; bar: ReactNode } {
  const errorBar = useMemo(() => (error ? <ErrorBar text={error} onDismiss={() => setError("")} onReport={onReport} /> : null), [error, setError, onReport]);
  const note = useMemo(
    () => <LevelUpNotice port={port} tick={feedbackOpen ? 1 : 0} fallback={<AutoArchiveNotice tree={tree} />} />,
    [tree, port, feedbackOpen],
  );
  return { errorBar, note, bar: errorBar ?? note };
}
