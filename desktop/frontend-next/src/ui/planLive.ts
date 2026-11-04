import { createContext, useContext } from "react";
import { todoStep, type Item, type PlanStep } from "../state/session";
import type { Approval } from "../port/wire";

import { isPlanGate } from "../state/plan_card";

export { isPlanGate };

/** The list approving seeds, as the kernel sent it with the card. */
export function proposedSteps(a: Approval): PlanStep[] {
  const todos = a.input?.todos;
  return Array.isArray(todos) ? todos.map(todoStep) : [];
}

type Gate = Extract<Item, { t: "approval" }>;

/** The newest plan card in the transcript: the only one the live list belongs to. */
export function latestPlanGate(items: Item[]): Gate | undefined {
  for (let i = items.length - 1; i >= 0; i--) {
    const it = items[i];
    if (it.t === "approval" && isPlanGate(it.a)) return it;
  }
  return undefined;
}

/** What the transcript's plan card reads from its pane: the kernel's live list,
 *  whether the run is moving or waiting on a person, and where it says whether
 *  the current plan's card is on screen. */
export interface PlanLive {
  plan: PlanStep[];
  running: boolean;
  blocked: boolean;
  latest?: string;
  onSeen: (seen: boolean) => void;
}

export const PlanLiveContext = createContext<PlanLive | null>(null);
export const usePlanLive = () => useContext(PlanLiveContext);
