import { useLayoutEffect, type RefObject } from "react";
import { useAppNavigationStore } from "../store/appNavigation";

/** Keep the workspace geometry and runtime mounted while isolating its input. */
export function useManagementWorkspace(workspaceRef: RefObject<HTMLDivElement | null>, active: boolean) {
  useLayoutEffect(() => {
    if (!active) return;
    const workspace = workspaceRef.current;
    const previous = useAppNavigationStore.getState().workspaceFocus;
    if (workspace) workspace.inert = true;
    return () => {
      if (workspace) workspace.inert = false;
      queueMicrotask(() => {
        if (useAppNavigationStore.getState().page.kind === "workspace" && previous?.isConnected && !previous.closest("[inert]")) previous.focus({ preventScroll: true });
      });
    };
  }, [workspaceRef, active]);
}
