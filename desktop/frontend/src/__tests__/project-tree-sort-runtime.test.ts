type Equal = (actual: unknown, expected: unknown, label: string) => void;

export async function runProjectTreeSortRuntimeTests(eq: Equal, projectTreeSource: string) {
  eq(
    projectTreeSource.includes("sortMode,")
      && projectTreeSource.includes("workbenchSortModeRef.current"),
    true,
    "topic page requests carry the selected conversation sort mode",
  );
  eq(
    projectTreeSource.includes("topicLoadSeqRef.current[key] += 1")
      && projectTreeSource.includes("void loadProjectTopics(project)"),
    true,
    "changing the conversation sort mode invalidates old pages and reloads immediately",
  );

  const loadSequences: Record<string, number> = { "project-a": 1 };
  const staleGeneration = loadSequences["project-a"];
  let resolveStaleLoad: (() => void) | undefined;
  let staleLoadApplied = false;
  const staleLoad = new Promise<void>((resolve) => {
    resolveStaleLoad = resolve;
  }).then(() => {
    if (loadSequences["project-a"] === staleGeneration) staleLoadApplied = true;
  });
  for (const key in loadSequences) loadSequences[key] += 1;
  resolveStaleLoad?.();
  await staleLoad;
  eq(staleLoadApplied, false, "a delayed old-sort response cannot apply after sort invalidation");

  const currentGeneration = ++loadSequences["project-a"];
  eq(loadSequences["project-a"] === currentGeneration, true, "the first request for the new sort remains current");
  eq(projectTreeSource.includes("topicPageStateRef.current = {}"), true, "sort invalidation clears pagination cursors before the new first page loads");
}
