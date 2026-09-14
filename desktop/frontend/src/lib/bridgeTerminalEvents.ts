import { hostEvents } from "./bridge";

export interface TerminalOutputEvent {
  id: string;
  data: string;
}

export interface TerminalExitEvent {
  id: string;
  exitCode: number;
  removed?: boolean;
}

function terminalEventPayload<T>(payload: unknown): T | null {
  if (!payload || typeof payload !== "object") return null;
  return payload as T;
}

export function onTerminalOutput(cb: (event: TerminalOutputEvent) => void): () => void {
  const off = hostEvents("terminal:output", (payload) => {
    const event = terminalEventPayload<TerminalOutputEvent>(payload);
    if (event?.id && typeof event.data === "string") cb(event);
  });
  if (off) return off;
  mockTerminalOutputListeners.add(cb);
  return () => mockTerminalOutputListeners.delete(cb);
}

export function onTerminalExit(cb: (event: TerminalExitEvent) => void): () => void {
  const off = hostEvents("terminal:exit", (payload) => {
    const event = terminalEventPayload<TerminalExitEvent>(payload);
    if (event?.id && typeof event.exitCode === "number") cb(event);
  });
  if (off) return off;
  mockTerminalExitListeners.add(cb);
  return () => mockTerminalExitListeners.delete(cb);
}

const mockTerminalOutputListeners = new Set<(event: TerminalOutputEvent) => void>();
const mockTerminalExitListeners = new Set<(event: TerminalExitEvent) => void>();

export function __emitMockTerminalOutput(event: TerminalOutputEvent): void {
  mockTerminalOutputListeners.forEach((listener) => listener(event));
}

export function __emitMockTerminalExit(event: TerminalExitEvent): void {
  mockTerminalExitListeners.forEach((listener) => listener(event));
}
