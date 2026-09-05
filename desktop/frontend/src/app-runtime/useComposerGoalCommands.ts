import type { CollaborationMode } from "../lib/types";
import { useGoalActionHandler } from "../lib/goalAction";
import { useCommittedCommand } from "../lib/useCommittedCommand";

/** Void Composer events share one error boundary; awaited send paths still reject. */
export function useComposerGoalCommands(input: {
  applyGoal: (goal: string) => Promise<void>;
  applyCollaborationMode: (mode: CollaborationMode) => Promise<void>;
}) {
  const { runGoalAction } = useGoalActionHandler();
  const clearGoalFromUi = useCommittedCommand(() => runGoalAction(() => input.applyGoal("")));
  const setCollaborationModeFromUi = useCommittedCommand((mode: CollaborationMode) => runGoalAction(() => input.applyCollaborationMode(mode)));
  return { clearGoalFromUi, setCollaborationModeFromUi };
}
