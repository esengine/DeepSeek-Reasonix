/** Generic pause gate — bridges tool functions and the App's modals via Promises. */
// Tools call gate.ask(kind, payload) and await the result; the App subscribes
// with gate.on() to show the right modal, then calls gate.resolve() on user pick.

export type ConfirmationChoice =
  | { type: "deny"; denyContext?: string }
  | { type: "run_once" }
  | { type: "always_allow"; prefix: string };

export type PlanVerdict = { type: "approve" } | { type: "refine" } | { type: "cancel" };

export type CheckpointVerdict =
  | { type: "continue" }
  | { type: "revise"; feedback?: string }
  | { type: "stop" };

export type RevisionVerdict = { type: "accepted" } | { type: "rejected" } | { type: "cancelled" };

export type ChoiceVerdict =
  | { type: "pick"; optionId: string }
  | { type: "text"; text: string }
  | { type: "cancel" };

export type ToolConfirmationAuditEvent =
  | {
      type: "tool.confirm.allow";
      kind: "run_command" | "run_background";
      payload: { command: string };
    }
  | {
      type: "tool.confirm.deny";
      kind: "run_command" | "run_background";
      payload: { command: string };
      denyContext?: string;
    }
  | {
      type: "tool.confirm.always_allow";
      kind: "run_command" | "run_background";
      payload: { command: string };
      prefix: string;
    };

interface PauseResponseMap {
  run_command: ConfirmationChoice;
  run_background: ConfirmationChoice;
  plan_proposed: PlanVerdict;
  plan_checkpoint: CheckpointVerdict;
  plan_revision: RevisionVerdict;
  choice: ChoiceVerdict;
}

type PauseKind = keyof PauseResponseMap;

interface PausePayloadMap {
  run_command: { command: string };
  run_background: { command: string };
  plan_proposed: { plan: string; steps?: unknown[]; summary?: string };
  plan_checkpoint: { stepId: string; title?: string; result: string; notes?: string };
  plan_revision: { reason: string; remainingSteps: unknown[]; summary?: string };
  choice: { question: string; options: unknown[]; allowCustom: boolean };
}

export type PauseRequest = {
  id: number;
  kind: PauseKind;
  payload: unknown;
};

type GateListener = (request: PauseRequest) => void;
type AuditListener = (event: ToolConfirmationAuditEvent) => void;

/** Named options for PauseGate.ask() — makes it obvious which field is kind vs payload. */
export interface PauseAskOpts<K extends PauseKind = PauseKind> {
  kind: K;
  payload: PausePayloadMap[K];
}

export class PauseGate {
  private _nextId = 0;
  private _pending = new Map<number, { resolve: (data: unknown) => void; request: PauseRequest }>();
  private _listeners: Set<GateListener> = new Set();
  private _auditListener: AuditListener | null = null;

  /** Block until the user responds. Takes a named options object so the
   *  kind and payload fields don't get confused at the call site. */
  ask<K extends PauseKind>(opts: PauseAskOpts<K>): Promise<PauseResponseMap[K]> {
    const { kind, payload } = opts;
    if (this._listeners.size === 0) {
      throw new Error(
        `${kind}: no confirmation listener registered — cannot prompt the user. This tool can only be used inside an interactive Reasonix session.`,
      );
    }
    return new Promise((resolve) => {
      const id = this._nextId++;
      const request: PauseRequest = { id, kind, payload };
      this._pending.set(id, { resolve: resolve as (d: unknown) => void, request });
      for (const fn of this._listeners) {
        try {
          fn(request);
        } catch {
          /* listener error shouldn't break the gate */
        }
      }
    });
  }

  /** Resolve a pending request. Called by the App's modal callback. */
  resolve(id: number, data: unknown): void {
    const p = this._pending.get(id);
    if (!p) return;
    this._pending.delete(id);
    this.emitAuditEvent(p.request, data);
    p.resolve(data);
  }

  setAuditListener(fn: AuditListener | null): void {
    this._auditListener = fn;
  }

  /** Subscribe to new pause requests. Returns an unsubscribe function. */
  on(fn: GateListener): () => void {
    this._listeners.add(fn);
    return () => {
      this._listeners.delete(fn);
    };
  }

  /** Current pending request, if any (polling fallback). */
  get current(): PauseRequest | null {
    for (const [, p] of this._pending) return p.request;
    return null;
  }

  private emitAuditEvent(request: PauseRequest, data: unknown): void {
    if (!this._auditListener) return;
    if (request.kind !== "run_command" && request.kind !== "run_background") return;
    if (!data || typeof data !== "object") return;
    const choice = data as Partial<ConfirmationChoice>;
    try {
      switch (choice.type) {
        case "run_once":
          this._auditListener({
            type: "tool.confirm.allow",
            kind: request.kind,
            payload: request.payload as { command: string },
          });
          break;
        case "deny":
          this._auditListener({
            type: "tool.confirm.deny",
            kind: request.kind,
            payload: request.payload as { command: string },
            denyContext: choice.denyContext,
          });
          break;
        case "always_allow":
          if (typeof choice.prefix !== "string") return;
          this._auditListener({
            type: "tool.confirm.always_allow",
            kind: request.kind,
            payload: request.payload as { command: string },
            prefix: choice.prefix,
          });
          break;
        default:
          break;
      }
    } catch {
      /* audit path must never break the gate */
    }
  }
}

/** Singleton shared between tools and the App. */
export const pauseGate = new PauseGate();
