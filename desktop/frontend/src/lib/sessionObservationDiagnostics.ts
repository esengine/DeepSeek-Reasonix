// Content-free, bounded traces survive component unmounts. Keys are canonical
// session routes; observations explicitly identify their renderer lifetime.
const startedAt = new Date().toISOString();
type Observation = { at: string; action: string; tabId: string; generation: number; sequence: number; status?: string;
  prompt?: { id?: string; turnId?: string; runtimeEpoch?: string; kind?: string; bindingGeneration: number | null } };
const traces = new Map<string, { events: Observation[]; dropped: number }>();
export function noteSessionObservation(path: string, value: Omit<Observation, "at">): void {
  const key = JSON.stringify([value.tabId, path]);
  const trace = traces.get(key) ?? { events: [], dropped: 0 };
  if (trace.events.length === 256) { trace.events.shift(); trace.dropped++; }
  trace.events.push({ ...value, at: new Date().toISOString() });
  traces.delete(key); traces.set(key, trace);
  while (traces.size > 64) traces.delete(traces.keys().next().value!);
}
export function sessionObservationDiagnostics(path: string, tabId: string) {
  const trace = traces.get(JSON.stringify([tabId, path]));
  return { processStartedAt: startedAt, capturedAt: new Date().toISOString(), events: trace?.events ?? [], dropped: trace?.dropped ?? 0,
    unavailable: ["Observations before this renderer started or after trace eviction are unavailable."] };
}
