/**
 * Pluggable host surface shared by Wails desktop and HTTP/SSE (serve) clients.
 * Default product path remains Wails; Electron PoC uses HttpSseHost.
 */

import type { POC_CAPABILITIES } from "./capabilities";

export type HostCapabilities = typeof POC_CAPABILITIES;

export interface ReasonixHost {
  getCapabilities(): HostCapabilities;
  dispose(): void;

  status(): Promise<unknown>;
  history(): Promise<unknown>;
  context(): Promise<unknown>;
  sessions(): Promise<unknown>;
  skills(): Promise<unknown>;
  todos(): Promise<unknown>;
  checkpoints(): Promise<unknown>;
  models(): Promise<unknown>;
  providerSetup(): Promise<unknown>;

  submit(input: string, format?: string): Promise<unknown>;
  cancel(): Promise<unknown>;
  approve(id: string, allow: boolean, session?: boolean, persist?: boolean): Promise<unknown>;
  answer(id: string, answers: unknown): Promise<unknown>;
  setPlanMode(on: boolean): Promise<unknown>;
  setToolApprovalMode(mode: string): Promise<unknown>;
  setAutoApproveTools(on: boolean): Promise<unknown>;
  compact(): Promise<unknown>;
  newSession(): Promise<unknown>;
  rewind(turn: number, scope: string): Promise<unknown>;
  fork(turn: number): Promise<unknown>;
  summarize(turn: number): Promise<unknown>;
  setGoal(goal: string): Promise<unknown>;
  clearGoal(): Promise<unknown>;
  resume(path: string): Promise<unknown>;
  deleteSession(path: string): Promise<unknown>;
  reloadExtensions(): Promise<unknown>;

  onEvent(handler: (e: Record<string, unknown>) => void): () => void;
}
