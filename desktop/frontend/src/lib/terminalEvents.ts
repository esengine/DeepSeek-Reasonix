import { onTerminalExit, onTerminalOutput, type TerminalExitEvent, type TerminalOutputEvent } from "./bridge";

const MAX_HISTORY_BYTES = 1024 * 1024;

type SequencedTerminalSink = (data: Uint8Array, sequence: number) => void;

const sinks = new Map<string, SequencedTerminalSink>();
const exitListeners = new Set<(event: TerminalExitEvent) => void>();
const history = new Map<string, Uint8Array[]>();
const historyBytes = new Map<string, number>();
const nextSequence = new Map<string, number>();
let started = false;
let stopBridge: (() => void) | null = null;

function decodeBase64(value: string): Uint8Array {
  if (typeof atob !== "function") return new Uint8Array();
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
  return bytes;
}

function deliverOutput(event: TerminalOutputEvent): void {
  const bytes = decodeBase64(event.data);
  if (bytes.byteLength === 0) return;
  const sequence = nextSequence.get(event.id) ?? 0;
  nextSequence.set(event.id, sequence + 1);
  const queue = history.get(event.id) ?? [];
  queue.push(bytes);
  let total = (historyBytes.get(event.id) ?? 0) + bytes.byteLength;
  while (total > MAX_HISTORY_BYTES && queue.length > 0) {
    total -= queue.shift()?.byteLength ?? 0;
  }
  history.set(event.id, queue);
  historyBytes.set(event.id, total);
  sinks.get(event.id)?.(bytes, sequence);
}

function deliverExit(event: TerminalExitEvent): void {
  if (event.removed) forgetTerminalSession(event.id);
  exitListeners.forEach((listener) => listener(event));
}

export function startTerminalEventBridge(): () => void {
  if (!started) {
    started = true;
    const stopOutput = onTerminalOutput(deliverOutput);
    const stopExit = onTerminalExit(deliverExit);
    stopBridge = () => {
      stopOutput();
      stopExit();
      started = false;
      stopBridge = null;
    };
  }
  return () => stopBridge?.();
}

export function registerTerminalOutputSink(id: string, sink: SequencedTerminalSink): readonly [
  unregister: () => void,
  history: () => readonly [chunks: readonly Uint8Array[], nextSequence: number],
] {
  sinks.set(id, sink);
  return [
    () => {
      if (sinks.get(id) === sink) sinks.delete(id);
    },
    () => [history.get(id) ?? [], nextSequence.get(id) ?? 0],
  ];
}

export function forgetTerminalSession(id: string): void {
  history.delete(id);
  historyBytes.delete(id);
  nextSequence.delete(id);
}

export function registerTerminalExitListener(listener: (event: TerminalExitEvent) => void): () => void {
  exitListeners.add(listener);
  return () => exitListeners.delete(listener);
}

export function __resetTerminalEventBus(): void {
  sinks.clear();
  history.clear();
  historyBytes.clear();
  nextSequence.clear();
  stopBridge?.();
}

export const terminalEventBufferLimit = MAX_HISTORY_BYTES;
