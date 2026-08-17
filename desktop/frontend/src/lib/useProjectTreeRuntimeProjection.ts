import { useCallback, useEffect, useRef, type Dispatch, type SetStateAction } from "react";
import { app } from "./bridge";
import type { ProjectNode } from "./types";

type RuntimeProjection = {
  apply(tree: ProjectNode[]): ProjectNode[];
  dispose(): void;
};

export function useProjectTreeRuntimeProjection(
  setTree: Dispatch<SetStateAction<ProjectNode[]>>,
  excludedTopicIds: () => ReadonlySet<string>,
) {
  const projectionRef = useRef<RuntimeProjection | null>(null);
  useEffect(() => {
    let active = true;
    void import("./projectTreeRuntime").then((runtime) => {
      if (active) projectionRef.current = runtime.bindProjectTreeRuntime(
        setTree,
        () => app.GetProjectTreeRuntimeSnapshot?.(),
        excludedTopicIds,
      );
    }).catch(() => {});
    return () => {
      active = false;
      projectionRef.current?.dispose();
    };
  }, [excludedTopicIds, setTree]);
  return useCallback((tree: ProjectNode[]) => projectionRef.current?.apply(tree) ?? tree, []);
}
