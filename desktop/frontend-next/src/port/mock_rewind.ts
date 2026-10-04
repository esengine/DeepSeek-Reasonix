import type { RewindPlan, RewindResult, RewindScope, RewindUndo } from "./session";

export class MockRewind {
  private last: { prompts: string[]; offer: RewindUndo } | null = null;

  // Later scripted turns include bash, so the menu can show partial coverage.
  prepare(turn: number, scope: RewindScope): RewindPlan {
    const partial = turn > 0;
    return {
      planId: `mock-plan-${turn}-${scope}`,
      turn,
      coverage: partial ? "partial" : "full",
      coverageGaps: partial
        ? [{ reason: "bash_side_effect", detail: "bash side effects are not path-tracked", tool: "bash" }]
        : undefined,
      canFiles: true,
      canConversation: true,
      files: ["note.txt"],
      fileCount: turn > 0 ? 3 : 0,
      requiresConfirmation: partial,
    };
  }

  commit(planId: string, prompts: string[]): { prompts: string[]; result: RewindResult } {
    const turn = Number(planId.split("-")[2] ?? 0);
    this.last = { prompts: prompts.slice(), offer: { transactionId: `mock-tx-${turn}`, turn, files: 1 } };
    return {
      prompts: prompts.slice(0, turn),
      result: { ok: true, transactionId: this.last.offer.transactionId, undoAvailable: true, deleted: ["note.txt"] },
    };
  }

  available(): RewindUndo | null { return this.last?.offer ?? null; }

  undo(): string[] | null {
    const prompts = this.last?.prompts ?? null;
    this.invalidate();
    return prompts;
  }

  invalidate() { this.last = null; }
}
