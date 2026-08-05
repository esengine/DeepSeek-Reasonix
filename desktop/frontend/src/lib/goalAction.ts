import { useCallback } from "react";
import { useToast } from "./toast";

export type GoalAction = () => void | Promise<void>;

/**
 * Run a Goal-related UI/background action without leaking a rejected bridge
 * Promise to the global crash handler. Submission paths that must fail closed
 * should await the underlying action directly instead.
 */
export function runGoalAction(action: GoalAction, onError: (error: unknown) => void): void {
  try {
    void Promise.resolve(action()).catch(onError);
  } catch (error) {
    onError(error);
  }
}

export function useGoalActionHandler() {
  const { showToast } = useToast();
  const handleGoalActionError = useCallback((error: unknown) => {
    showToast(error instanceof Error ? error.message : String(error), "error");
  }, [showToast]);
  const runHandledGoalAction = useCallback((action: GoalAction) => {
    runGoalAction(action, handleGoalActionError);
  }, [handleGoalActionError]);
  return { runGoalAction: runHandledGoalAction, handleGoalActionError };
}
