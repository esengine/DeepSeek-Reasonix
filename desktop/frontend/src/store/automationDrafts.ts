import { create } from "zustand";
import type { HeartbeatTask } from "../custom/features/heartbeat/heartbeat.types";
import type { HeartbeatFrequencyType } from "../custom/features/heartbeat/heartbeat.presentation";
export const automationEditableFields = ["title", "prompt", "interval", "enabled", "scope", "workspaceRoot", "approvalMode", "newConversationEachRun", "notifyChannels", "timeWindowStart", "timeWindowEnd"] as const;
type Field = typeof automationEditableFields[number];
export type AutomationDraft = {
  baseline: HeartbeatTask | null; draft: HeartbeatTask; conflicts: Field[];
  missing: boolean; busy: boolean; error: boolean; version: number;
  frequency: HeartbeatFrequencyType; tab: "configuration" | "history";
};
export function frequencyOf(interval: string): HeartbeatFrequencyType {
  const cycle = interval.match(/\|(daily|weekly|biweekly|monthly|yearly)/)?.[1];
  return cycle ? cycle as HeartbeatFrequencyType : interval.trim().split(/\s+/).length >= 5 ? "cron" : "interval";
}
export function automationDraftDirty(entry: AutomationDraft): boolean {
  return !entry.baseline || automationEditableFields.some((key) => entry.draft[key] !== entry.baseline?.[key]);
}
function createEntry(task: HeartbeatTask, isNew: boolean): AutomationDraft {
  return { baseline: isNew ? null : { ...task }, draft: { ...task }, conflicts: [], missing: false,
    busy: false, error: false, version: 0, frequency: frequencyOf(task.interval), tab: "configuration" };
}
export function reconcileAutomationDraft(entry: AutomationDraft, current?: HeartbeatTask): AutomationDraft {
  if (!entry.baseline) return current ? { ...entry, conflicts: [...automationEditableFields] } : entry;
  if (!current) return { ...entry, missing: true };
  const draft = { ...entry.draft };
  const conflicts = new Set(entry.conflicts);
  for (const key of automationEditableFields) {
    const localChanged = draft[key] !== entry.baseline[key];
    const remoteChanged = current[key] !== entry.baseline[key];
    if (localChanged && remoteChanged && draft[key] !== current[key]) conflicts.add(key);
    if (!localChanged) Object.assign(draft, { [key]: current[key] });
    if (draft[key] === current[key]) conflicts.delete(key);
  }
  draft.topicId = current.topicId; draft.lastRunAt = current.lastRunAt; draft.runHistory = current.runHistory;
  return { ...entry, baseline: { ...current }, draft, missing: false, conflicts: [...conflicts],
    frequency: draft.interval === entry.draft.interval ? entry.frequency : frequencyOf(draft.interval) };
}
type Store = {
  entries: Record<string, AutomationDraft>;
  ensure: (task: HeartbeatTask, isNew?: boolean) => void;
  edit: (id: string, update: (task: HeartbeatTask) => HeartbeatTask) => void;
  ui: (id: string, patch: Partial<Pick<AutomationDraft, "frequency" | "tab">>) => void;
  reconcile: (tasks: HeartbeatTask[]) => void;
  begin: (id: string) => number | null;
  finish: (id: string, version: number, saved?: HeartbeatTask) => void;
  settle: (id: string, version: number, success: boolean) => void;
  discard: (id: string) => void;
  remove: (id: string) => void;
};
export const useAutomationDraftStore = create<Store>((set, get) => ({
  entries: {},
  ensure: (task, isNew = false) => { if (!get().entries[task.id]) set((s) => ({ entries: { ...s.entries, [task.id]: createEntry(task, isNew) } })); },
  edit: (id, update) => set((s) => {
    const entry = s.entries[id]; if (!entry || entry.busy) return s;
    return { entries: { ...s.entries, [id]: { ...entry, draft: update(entry.draft), version: entry.version + 1 } } };
  }),
  ui: (id, patch) => set((s) => { const entry = s.entries[id]; return !entry || entry.busy ? s : { entries: { ...s.entries, [id]: { ...entry, ...patch } } }; }),
  reconcile: (tasks) => set((s) => ({ entries: Object.fromEntries(Object.entries(s.entries).map(([id, entry]) => [id, reconcileAutomationDraft(entry, tasks.find((task) => task.id === id))])) })),
  begin: (id) => {
    const entry = get().entries[id]; if (!entry || entry.busy) return null;
    const version = entry.version + 1;
    set((s) => ({ entries: { ...s.entries, [id]: { ...entry, version, busy: true, error: false } } })); return version;
  },
  finish: (id, version, saved) => set((s) => {
    const entry = s.entries[id]; if (!entry || entry.version !== version) return s;
    return { entries: { ...s.entries, [id]: saved ? { ...createEntry(saved, false), version, tab: entry.tab } : { ...entry, busy: false, error: true } } };
  }),
  settle: (id, version, success) => set((s) => { const entry = s.entries[id]; return !entry || entry.version !== version ? s : { entries: { ...s.entries, [id]: { ...entry, busy: false, error: !success } } }; }),
  discard: (id) => set((s) => {
    const entry = s.entries[id]; if (!entry || entry.busy) return s;
    const entries = { ...s.entries };
    if (entry.baseline && !entry.missing) entries[id] = createEntry(entry.baseline, false); else delete entries[id];
    return { entries };
  }),
  remove: (id) => set((s) => { const entries = { ...s.entries }; delete entries[id]; return { entries }; }),
}));
