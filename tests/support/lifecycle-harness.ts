import { EngineeringLifecycleRuntime } from "../../src/code/lifecycle.js";
import {
  type CheckpointVerdict,
  PauseGate,
  type PlanVerdict,
  type RevisionVerdict,
} from "../../src/core/pause-gate.js";
import { ToolRegistry } from "../../src/tools.js";
import { type PlanStep, type StepCompletion, registerPlanTool } from "../../src/tools/plan.js";

type GateVerdict = PlanVerdict | CheckpointVerdict | RevisionVerdict;

class QueueGate extends PauseGate {
  readonly requests: Array<{ kind: string; payload: unknown }> = [];
  private readonly lifecycle: EngineeringLifecycleRuntime;
  private readonly verdicts: GateVerdict[] = [];

  constructor(lifecycle: EngineeringLifecycleRuntime) {
    super();
    this.lifecycle = lifecycle;
  }

  push(verdict: GateVerdict): void {
    this.verdicts.push(verdict);
  }

  override ask(opts: { kind: string; payload?: unknown }): Promise<any> {
    this.requests.push({ kind: opts.kind, payload: opts.payload });
    const verdict = this.verdicts.shift();
    if (!verdict) throw new Error(`no queued verdict for ${opts.kind}`);

    if (opts.kind === "plan_proposed" && verdict.type === "approve") {
      const payload = opts.payload as { steps?: PlanStep[] } | undefined;
      this.lifecycle.recordPlanApproved(payload?.steps);
    }
    if (opts.kind === "plan_checkpoint") {
      this.lifecycle.recordCheckpointReached();
      if (verdict.type === "stop") this.lifecycle.cancel();
    }
    if (opts.kind === "plan_revision" && verdict.type === "accepted") {
      const payload = opts.payload as { remainingSteps?: PlanStep[] } | undefined;
      this.lifecycle.recordPlanRevised(payload?.remainingSteps ?? []);
    }

    return Promise.resolve(verdict);
  }
}

export interface StrictLifecycleHarness {
  lifecycle: EngineeringLifecycleRuntime;
  gate: QueueGate;
  completions: StepCompletion[];
  dispatch(name: string, args: Record<string, unknown>): Promise<string>;
  queue(verdict: GateVerdict): void;
}

export function createStrictLifecycleHarness(): StrictLifecycleHarness {
  const lifecycle = new EngineeringLifecycleRuntime({ mode: "strict" });
  const registry = new ToolRegistry();
  const gate = new QueueGate(lifecycle);
  const completions: StepCompletion[] = [];

  registry.addToolInterceptor("engineering-lifecycle", lifecycle.guardToolCall);
  registry.setResultAugmenter((name, args, result) => {
    lifecycle.recordToolResult(name, args, result);
    return result;
  });
  registerPlanTool(registry, {
    onPlanSubmitted: (_plan, steps) => lifecycle.recordPlanProposed(steps),
    onStepCompleted: (completion) => completions.push(completion),
  });
  registerMutationTools(registry);

  return {
    lifecycle,
    gate,
    completions,
    queue: (verdict) => gate.push(verdict),
    dispatch: async (name, args) => {
      const result = await registry.dispatch(name, JSON.stringify(args), {
        confirmationGate: gate,
      });
      try {
        const parsed = JSON.parse(result) as Partial<StepCompletion>;
        if (parsed.kind === "step_completed" && typeof parsed.stepId === "string") {
          lifecycle.recordStepCompleted(parsed.stepId);
        }
      } catch {
        // Non-JSON tool results are normal for plan approval/revision text.
      }
      return result;
    },
  };
}

function registerMutationTools(registry: ToolRegistry): void {
  registry.register({
    name: "delete_file",
    parameters: {
      type: "object",
      properties: { path: { type: "string" } },
      required: ["path"],
    },
    fn: (args: { path: string }) => `deleted ${args.path}`,
  });
  registry.register({
    name: "write_file",
    parameters: {
      type: "object",
      properties: { path: { type: "string" }, content: { type: "string" } },
      required: ["path", "content"],
    },
    fn: (args: { path: string }) => `▸ edit blocks: 1/1 applied\n  ✓ wrote       ${args.path}`,
  });
}
