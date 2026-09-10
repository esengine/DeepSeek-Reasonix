import type { EventFrame, ServiceState } from "./ipc.js";

export interface EventRecovery {
  generation: string;
  reason: "generation" | "gap" | "subscription";
  expectedSeq: number;
  actualSeq: number;
}

// Dynamic remote-tab event names must not accumulate for the renderer's
// lifetime. Once the cap is reached, retain no names and conservatively
// repair every new subscription until the next service generation.
export class MissedEventSubscriptions {
  private readonly names = new Set<string>();
  private overflow = false;
  get size(): number { return this.names.size; }
  add(name: string): void {
    if (this.overflow || this.names.has(name)) return;
    if (this.names.size === 64) { this.names.clear(); this.overflow = true; }
    else this.names.add(name);
  }
  consume(name: string): boolean { return this.names.delete(name) || this.overflow; }
  clear(): void { this.names.clear(); this.overflow = false; }
}

export function eventFrame(value: unknown, allowShellEvent = false): EventFrame | null {
  if (typeof value !== "object" || value === null) return null;
  const frame = value as Partial<EventFrame>;
  if (!Number.isSafeInteger(frame.seq) || (frame.seq ?? -1) < 0 || typeof frame.generation !== "string"
    || typeof frame.name !== "string" || !Array.isArray(frame.args)) return null;
  if (frame.seq === 0 && (!allowShellEvent || !frame.name.startsWith("app:"))) return null;
  return frame as EventFrame;
}

// The transport cursor is independent of individual event subscriptions. A
// gap requests authoritative read-side repair; it never replays an invocation.
export class DesktopEventStream {
  private generation = "";
  private seq = 0;
  private lastRecovery: EventRecovery | null = null;

  constructor(private readonly deliver: (frame: EventFrame) => void, private readonly recover: (event: EventRecovery) => void) {}

  get recovery(): EventRecovery | null { return this.lastRecovery; }

  observeState(state: ServiceState): void {
    const generation = state.phase === "ready" ? state.generation : "";
    if (generation === this.generation) return;
    this.generation = generation;
    this.seq = 0;
    this.lastRecovery = null;
    if (generation !== "") this.requestRecovery("generation");
  }

  requestRecovery(reason: EventRecovery["reason"], actualSeq = this.seq): void {
    if (this.generation === "") return;
    const event: EventRecovery = { generation: this.generation, reason, expectedSeq: this.seq + 1, actualSeq };
    this.lastRecovery = event;
    this.recover(event);
  }

  accept(value: unknown): boolean {
    const frame = eventFrame(value, true);
    if (!frame || this.generation === "" || frame.generation !== this.generation) return false;
    if (frame.seq !== 0) {
      if (frame.seq <= this.seq) return false;
      if (frame.seq > this.seq + 1) this.requestRecovery("gap", frame.seq);
      this.seq = frame.seq;
    }
    this.deliver(frame);
    return true;
  }
}
