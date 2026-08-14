import { lazy, Suspense, useState, type ReactNode } from "react";
import type { ToastContextValue } from "../lib/toast";

const BlankProjectFlow = lazy(() => import("./BlankProjectFlow").then((module) => ({ default: module.BlankProjectFlow })));

export function useProjectCreation({
  onAddProject,
  onRefresh,
  showToast,
}: {
  onAddProject: (path?: string) => Promise<void>;
  onRefresh: () => Promise<void>;
  showToast: ToastContextValue["showToast"];
}): {
  addingProject: boolean;
  handleAddProject: () => Promise<void>;
  openBlankProjectFlow: () => void;
  blankProjectFlow: ReactNode;
} {
  const [addingProject, setAddingProject] = useState(false);
  const [blankProjectFlowOpen, setBlankProjectFlowOpen] = useState(false);
  const handleAddProject = async () => {
    if (addingProject) return;
    setAddingProject(true);
    try {
      await onAddProject();
      await onRefresh();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setAddingProject(false);
    }
  };
  return {
    addingProject,
    handleAddProject,
    openBlankProjectFlow: () => setBlankProjectFlowOpen(true),
    blankProjectFlow: blankProjectFlowOpen ? (
      <Suspense fallback={null}>
        <BlankProjectFlow
          onOpenProject={onAddProject}
          onRefresh={onRefresh}
          onClose={() => setBlankProjectFlowOpen(false)}
        />
      </Suspense>
    ) : null,
  };
}
