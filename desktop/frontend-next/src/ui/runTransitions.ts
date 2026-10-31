// Completion is a transition between known states, not a first observation.
export function newlyDone(before: Record<string, string>, runs: Record<string, { run: string; live: boolean }>): string[] {
  const fresh: string[] = [];
  for (const [id, st] of Object.entries(runs)) {
    if (st.run === "done" && before[id] !== undefined && before[id] !== "done") fresh.push(id);
    before[id] = st.run;
  }
  return fresh;
}
