type Selection = { current: string; pending?: string };

/** Mirrors the desktop's per-tab selection and task-turn application boundary. */
export class MockEffortSelections {
  private readonly selections = new Map<string, Selection>();

  get(tabId: string): Selection {
    return this.selections.get(tabId) ?? { current: "auto" };
  }

  select(tabId: string, level: string, running: boolean): void {
    const effort = this.get(tabId);
    const selected = level || "auto";
    if (running && selected !== effort.current) effort.pending = selected;
    else {
      delete effort.pending;
      effort.current = selected;
    }
    this.selections.set(tabId, effort);
  }

  beginTurn(tabId: string): void {
    const effort = this.get(tabId);
    if (effort.pending) {
      effort.current = effort.pending;
      delete effort.pending;
    }
  }

  clearPending(tabId: string): void {
    delete this.get(tabId).pending;
  }
}
