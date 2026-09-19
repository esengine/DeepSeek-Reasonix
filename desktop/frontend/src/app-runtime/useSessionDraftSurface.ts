import { useCallback, useEffect, useRef, useState } from "react";

import type {
  SessionDraftSettings,
  SessionDraftSubmissionView,
  SessionDraftSummary,
  SessionDraftView,
  ServerView,
  SessionRef,
} from "../generated/desktopContract.generated";
import { app } from "../lib/bridge";
import type { StructuredInvocationSubmit } from "../lib/invocationDisplay";
import type { CommandInfo, CollaborationMode, ToolApprovalMode } from "../lib/types";
import type { PersistentComposerDraft } from "../components/Composer";
import { buildInitialGoalSubmission } from "./sessionSubmissionOwner";

declare global {
  interface Window {
    __reasonixFlushSessionDraft?: () => Promise<void>;
    __reasonixResumeSessionDraftEditing?: () => void;
  }
}

const EMPTY_CONTENT: PersistentComposerDraft = {
  text: "",
  invocations: [],
  attachments: [],
  workspaceRefs: [],
  pastedBlocks: [],
  openPastedLabels: [],
  sessionRefs: [],
  selectedTextRefs: [],
};

function parseContent(raw: string): PersistentComposerDraft {
  try {
    const value = JSON.parse(raw || "{}") as Partial<PersistentComposerDraft>;
    return {
      text: typeof value.text === "string" ? value.text : "",
      invocations: Array.isArray(value.invocations) ? value.invocations : [],
      attachments: Array.isArray(value.attachments) ? value.attachments.map(({ previewUrl: _previewUrl, ...attachment }) => attachment) : [],
      workspaceRefs: Array.isArray(value.workspaceRefs) ? value.workspaceRefs : [],
      pastedBlocks: Array.isArray(value.pastedBlocks) ? value.pastedBlocks : [],
      openPastedLabels: Array.isArray(value.openPastedLabels) ? value.openPastedLabels : [],
      sessionRefs: Array.isArray(value.sessionRefs) ? value.sessionRefs : [],
      selectedTextRefs: Array.isArray(value.selectedTextRefs) ? value.selectedTextRefs : [],
    };
  } catch {
    return { ...EMPTY_CONTENT };
  }
}

function contentJSON(content: PersistentComposerDraft): string {
  return JSON.stringify({
    ...content,
    attachments: content.attachments.map(({ previewUrl: _previewUrl, ...attachment }) => attachment),
  });
}

function sameSettings(left: SessionDraftSettings, right: SessionDraftSettings): boolean {
  return canonicalJSON(left) === canonicalJSON(right);
}

function canonicalJSON(value: unknown): string {
  return JSON.stringify(value, (_key, item) => item && typeof item === "object" && !Array.isArray(item)
    ? Object.fromEntries(Object.keys(item).sort().map(key => [key, item[key]])) : item);
}

export type DraftSaveState = "saved" | "dirty" | "saving" | "error" | "conflict";

export type SessionDraftSurface = {
  kind: "draft";
  draft: SessionDraftView;
  content: PersistentComposerDraft;
  settings: SessionDraftSettings;
  commands: CommandInfo[];
  servers: ServerView[];
  models?: import("../generated/desktopContract.generated").ModelInfo[];
  generation: number;
  editVersion: number;
  pendingTasks: number;
  preparingSubmission: boolean;
  saveState: DraftSaveState;
  error?: string;
  taskError?: string;
  conflict?: SessionDraftView;
  operation?: SessionDraftSubmissionView;
};

export type DraftHandle = Readonly<{ draftId: string; generation: number }>;

export type DraftSubmissionCapture = Readonly<{
  handle: DraftHandle;
  preparationId: string;
  draftId: string;
  workspaceId: string;
  generation: number;
  editVersion: number;
  navigationIntent: number;
  content: PersistentComposerDraft;
  settings: SessionDraftSettings;
}>;

type DraftEntry = {
  draft: SessionDraftView;
  content: PersistentComposerDraft;
  settings: SessionDraftSettings;
  commands: CommandInfo[];
  servers: ServerView[];
  models?: import("../generated/desktopContract.generated").ModelInfo[];
  generation: number;
  visibleIntent: number;
  editVersion: number;
  savedEditVersion: number;
  saving: boolean;
  error?: string;
  taskError?: string;
  conflict?: SessionDraftView;
  operation?: SessionDraftSubmissionView;
  lifecycle: "active" | "converted" | "discarded";
  timer?: number;
  savePromise?: Promise<SessionDraftView | null>;
  pendingTasks: Map<string, Promise<unknown>>;
  preparingSubmission: boolean;
  preparation?: DraftSubmissionCapture;
  operationCapture?: DraftSubmissionCapture;
  discarding?: boolean;
  deferredContent?: PersistentComposerDraft;
  lastAccess: number;
};

type DraftSurfaceOptions = {
  onAccepted(ref: SessionRef): Promise<void> | void;
  onChanged(): void;
  claimNavigationIntent?: () => number; currentNavigationIntent?: () => number;
  isNavigationIntentCurrent?: (intent: number) => boolean;
};

function saveState(entry: DraftEntry): DraftSaveState {
  if (entry.conflict) return "conflict";
  if (entry.error) return "error";
  if (entry.saving) return "saving";
  if (entry.savedEditVersion < entry.editVersion) return "dirty";
  return "saved";
}

export function draftSubmissionLocksEditing(operation?: SessionDraftSubmissionView): boolean {
  return Boolean(operation && !["cancelled", "terminal_failed"].includes(operation.phase));
}

const DRAFT_ATTENTION_PHASES = new Set([
  "accepted",
  "dispatch_unknown",
  "dispatching_shell",
  "resume_required",
  "runtime_failed",
  "terminal_failed",
]);

/** Healthy drafts use the ordinary new-session landing. Only states that need
 *  an explicit user decision interrupt that surface with recovery controls. */
export function draftSurfaceNeedsAttention(draft: SessionDraftSurface): boolean {
  return draft.saveState === "conflict" || draft.saveState === "error" || Boolean(draft.taskError)
    || Boolean(draft.operation && DRAFT_ATTENTION_PHASES.has(draft.operation.phase));
}

function projectEntry(entry: DraftEntry): SessionDraftSurface {
  return {
    kind: "draft",
    draft: entry.draft,
    content: entry.content,
    settings: entry.settings,
    commands: entry.commands,
    servers: entry.servers,
    models: entry.models,
    generation: entry.generation,
    editVersion: entry.editVersion,
    pendingTasks: entry.pendingTasks.size,
    preparingSubmission: entry.preparingSubmission || Boolean(entry.discarding),
    saveState: saveState(entry),
    error: entry.error,
    taskError: entry.taskError,
    conflict: entry.conflict,
    operation: entry.operation,
  };
}

function cloneContent(content: PersistentComposerDraft): PersistentComposerDraft {
  return structuredClone(content);
}

function cloneSettings(settings: SessionDraftSettings): SessionDraftSettings {
  return structuredClone(settings);
}

function captureMatches(value: unknown, draftId: string, generation: number): value is DraftSubmissionCapture {
  if (!value || typeof value !== "object") return false;
  const capture = value as Partial<DraftSubmissionCapture>;
  return capture.draftId === draftId && capture.generation === generation;
}

function pruneCleanEntries(entries: Map<string, DraftEntry>, visibleDraftId: string) {
  const clean = [...entries.entries()]
    .filter(([id, entry]) => id !== visibleDraftId && entry.lifecycle === "active"
      && entry.savedEditVersion === entry.editVersion && !entry.saving && !entry.conflict && !entry.error && !entry.taskError
      && !entry.operation && !entry.preparingSubmission && !entry.discarding && entry.pendingTasks.size === 0)
    .sort((left, right) => right[1].lastAccess - left[1].lastAccess);
  for (const [id] of clean.slice(20)) entries.delete(id);
}

export function useSessionDraftSurface(options: DraftSurfaceOptions) {
  const { onAccepted, onChanged, claimNavigationIntent, currentNavigationIntent, isNavigationIntentCurrent } = options;
  const entriesRef = useRef(new Map<string, DraftEntry>());
  const visibleDraftIdRef = useRef<string | null>(null);
  const [surface, setSurface] = useState<SessionDraftSurface | null>(null);
  const [summaries, setSummaries] = useState<SessionDraftSummary[]>([]);
  const openSequence = useRef(0);
  const localIntent = useRef(0);
  const restoreChain = useRef<Promise<void>>(Promise.resolve());
  const submissionWaits = useRef(new Map<string, Promise<void>>());
  const submissionStarts = useRef(new Map<string, Promise<void>>());
  const convertedOperations = useRef(new Set<string>());
  const acceptingExit = useRef(false);
  const allTasks = useRef(new Map<string, Promise<unknown>>());
  const preparationBarriers = useRef(new Map<string, { promise: Promise<void>; resolve(): void }>());
  const disposed = useRef(false);
  const observeOperation = useRef<(operation: SessionDraftSubmissionView, capture: DraftSubmissionCapture) => Promise<void>>(async () => {});

  const intentCurrent = useCallback((intent: number) => (
    isNavigationIntentCurrent ? isNavigationIntentCurrent(intent) : localIntent.current === intent
  ), [isNavigationIntentCurrent]);

  const claimIntent = useCallback(() => {
    const intent = claimNavigationIntent?.() ?? ++localIntent.current;
    localIntent.current = intent;
    return intent;
  }, [claimNavigationIntent]);

  const publish = useCallback((draftId: string | null = visibleDraftIdRef.current) => {
    if (!draftId) {
      setSurface(null);
      return;
    }
    const projected = entriesRef.current.get(draftId);
    if (projected) {
      setSummaries((current) => current.map((summary) => summary.id === draftId
        ? { ...summary, state: saveState(projected) }
        : summary));
    }
    if (visibleDraftIdRef.current !== draftId) return;
    const entry = projected;
    setSurface(entry && entry.lifecycle !== "discarded" ? projectEntry(entry) : null);
  }, []);

  const refreshSummaries = useCallback(async () => {
    try {
      const records = await app.ListSessionDraftSummaries();
      setSummaries(records.map((summary) => {
        const entry = entriesRef.current.get(summary.id);
        return entry ? { ...summary, state: saveState(entry) } : summary;
      }));
    } catch {
      // Keep the last successful projection on transient list failures.
    }
  }, []);

  useEffect(() => { void refreshSummaries(); }, [refreshSummaries]);

  const queueRestoreTarget = useCallback((draftId: string, intent: number) => {
    const next = restoreChain.current.catch(() => undefined).then(async () => {
      if (!intentCurrent(intent)) return;
      await app.SetSessionDraftRestoreTarget(draftId);
    });
    restoreChain.current = next;
    return next;
  }, [intentCurrent]);

  const installDraft = useCallback(async (draft: SessionDraftView, sequence: number, intent: number) => {
    const context = await app.GetDraftContext(draft.id);
    if (sequence !== openSequence.current || !intentCurrent(intent)) return;
    const existing = entriesRef.current.get(draft.id);
    let entry: DraftEntry;
    if (existing && existing.lifecycle === "active") {
      existing.commands = (context.commands ?? []) as CommandInfo[];
      existing.servers = context.servers ?? [];
      existing.models = context.models ?? [];
      existing.visibleIntent = intent;
      existing.lastAccess = Date.now();
      if (existing.savedEditVersion === existing.editVersion && !existing.saving && !existing.conflict && !existing.error) {
        existing.draft = context.draft;
        existing.content = parseContent(context.draft.contentJson);
        existing.settings = context.draft.settings;
      }
      entry = existing;
    } else {
      entry = {
        draft: context.draft,
        content: parseContent(context.draft.contentJson),
        settings: context.draft.settings,
        commands: (context.commands ?? []) as CommandInfo[],
        servers: context.servers ?? [],
        models: context.models ?? [],
        generation: 1,
        visibleIntent: intent,
        editVersion: 0,
        savedEditVersion: 0,
        saving: false,
        lifecycle: "active",
        pendingTasks: new Map(),
        preparingSubmission: false,
        lastAccess: Date.now(),
      };
      entriesRef.current.set(draft.id, entry);
    }
    visibleDraftIdRef.current = draft.id;
    if (context.operation) {
      if (!entry.operation || entry.operation.operationId !== context.operation.operationId || entry.operation.revision <= context.operation.revision) entry.operation = context.operation;
      const capture: DraftSubmissionCapture = {
        handle: { draftId: draft.id, generation: entry.generation }, preparationId: "",
        draftId: draft.id, generation: entry.generation, workspaceId: draft.workspaceId,
        editVersion: entry.editVersion, navigationIntent: intent,
        content: cloneContent(entry.content), settings: cloneSettings(entry.settings),
      };
      entry.operationCapture ??= capture;
      void observeOperation.current(context.operation, entry.operationCapture).catch(() => undefined);
    }
    pruneCleanEntries(entriesRef.current, draft.id);
    publish(draft.id);
    await queueRestoreTarget(draft.id, intent);
    if (sequence !== openSequence.current || !intentCurrent(intent) || visibleDraftIdRef.current !== draft.id) return;
    window.requestAnimationFrame(() => {
      if (sequence === openSequence.current && intentCurrent(intent) && visibleDraftIdRef.current === draft.id) document.getElementById("composer-input")?.focus();
    });
    const previewGeneration = entry.generation;
    void Promise.all(entry.content.attachments.map(async (attachment) => {
      try {
		const previewUrl = await app.AttachmentDataURLForComposerTarget({ kind: "draft", draftId: draft.id }, attachment.path);
        const current = entriesRef.current.get(draft.id);
        if (!current || current.lifecycle !== "active" || current.generation !== previewGeneration) return;
        current.content = {
          ...current.content,
          attachments: current.content.attachments.map((item) => item.path === attachment.path ? { ...item, previewUrl } : item),
        };
        publish(draft.id);
      } catch {
        // Missing attachments remain visible as repairable references.
      }
    }));
  }, [intentCurrent, publish, queueRestoreTarget]);

  const startSaveLoop = useCallback((draftId: string): Promise<SessionDraftView | null> => {
    const entry = entriesRef.current.get(draftId);
    if (!entry || entry.lifecycle !== "active") return Promise.resolve(null);
    if (entry.timer != null) {
      window.clearTimeout(entry.timer);
      entry.timer = undefined;
    }
    if (entry.savePromise) return entry.savePromise;
    const generation = entry.generation;
    const run = (async () => {
      while (entry.lifecycle === "active" && entry.generation === generation && !entry.conflict && entry.savedEditVersion < entry.editVersion) {
        const capturedVersion = entry.editVersion;
        const capturedRevision = entry.draft.revision;
        const capturedContent = cloneContent(entry.content);
        const capturedSettings = cloneSettings(entry.settings);
        const capturedJSON = contentJSON(capturedContent);
        entry.saving = true;
        entry.error = undefined;
        publish(draftId);
        try {
          const result = await app.SaveSessionDraft({
            draftId,
            revision: capturedRevision,
            contentJson: capturedJSON,
            settings: capturedSettings,
            force: false,
          });
          if (entry.lifecycle !== "active" || entry.generation !== generation) return null;
          if (result.outcome === "converted" || result.outcome === "discarded") {
            entry.lifecycle = result.outcome;
            entry.generation++;
            return null;
          }
          if (result.conflict || result.outcome === "conflict") {
            entry.conflict = result.draft;
            return null;
          }
          if (result.outcome === "operation_locked") {
            entry.error = "This draft is locked by its submission operation.";
            return null;
          }
          entry.draft = result.draft;
          entry.savedEditVersion = Math.max(entry.savedEditVersion, capturedVersion);
          entry.conflict = undefined;
          entry.error = undefined;
          void refreshSummaries();
        } catch (error) {
          if (entry.lifecycle !== "active" || entry.generation !== generation) return null;
          try {
            const confirmed = await app.GetSessionDraft(draftId);
            if (confirmed.status !== "active") {
              entry.lifecycle = confirmed.status === "converted" ? "converted" : "discarded";
              entry.generation++;
              return null;
            }
            if (confirmed.contentJson === capturedJSON && sameSettings(confirmed.settings, capturedSettings)) {
              entry.draft = confirmed;
              entry.savedEditVersion = Math.max(entry.savedEditVersion, capturedVersion);
              entry.error = undefined;
              continue;
            }
            if (confirmed.revision !== capturedRevision) {
              entry.conflict = confirmed;
              return null;
            }
          } catch {
            // Preserve the original error when acknowledgement verification fails.
          }
          entry.error = error instanceof Error ? error.message : String(error);
          return null;
        } finally {
          entry.saving = false;
          publish(draftId);
        }
      }
      return entry.savedEditVersion >= entry.editVersion ? entry.draft : null;
    })();
    const tracked = run.finally(() => {
      if (entry.savePromise === tracked) entry.savePromise = undefined;
      publish(draftId);
    });
    entry.savePromise = tracked;
    return tracked;
  }, [publish, refreshSummaries]);

  const flushDraft = useCallback(async (draftId: string, targetVersion?: number): Promise<SessionDraftView | null> => {
    const entry = entriesRef.current.get(draftId);
    if (!entry || entry.lifecycle !== "active") return null;
    const requiredVersion = targetVersion ?? entry.editVersion;
    while (entry.lifecycle === "active" && entry.savedEditVersion < requiredVersion) {
      if (entry.conflict || entry.error) return null;
      await startSaveLoop(draftId);
      if (entry.savedEditVersion >= requiredVersion) break;
      if (entry.conflict || entry.error || entry.lifecycle !== "active") return null;
    }
    return entry.savedEditVersion >= requiredVersion ? entry.draft : null;
  }, [startSaveLoop]);

  const scheduleSave = useCallback((entry: DraftEntry) => {
    if (entry.timer != null) window.clearTimeout(entry.timer);
    entry.timer = window.setTimeout(() => {
      entry.timer = undefined;
      void startSaveLoop(entry.draft.id);
    }, 250);
  }, [startSaveLoop]);

  const updateContentFor = useCallback((draftId: string, generation: number, content: PersistentComposerDraft) => {
    const entry = entriesRef.current.get(draftId);
    if (!entry || entry.lifecycle !== "active" || entry.generation !== generation) return;
    if (entry.preparingSubmission || draftSubmissionLocksEditing(entry.operation)) return;
    if (entry.discarding) { entry.deferredContent = cloneContent(content); return; }
    // A task registered before the exit barrier may still publish its captured
    // attachment/reference. Ordinary edits remain frozen while quitting.
    if (acceptingExit.current && entry.pendingTasks.size === 0) return;
    if (contentJSON(content) === contentJSON(entry.content)) return;
    entry.content = cloneContent(content);
    entry.editVersion++;
    entry.error = undefined;
    entry.lastAccess = Date.now();
    scheduleSave(entry);
    publish(draftId);
  }, [publish, scheduleSave]);

  const updateSettingsFor = useCallback((draftId: string, generation: number, patch: Partial<SessionDraftSettings>) => {
    if (acceptingExit.current) return;
    const entry = entriesRef.current.get(draftId);
    if (!entry || entry.lifecycle !== "active" || entry.generation !== generation) return;
    if (entry.preparingSubmission || entry.discarding || draftSubmissionLocksEditing(entry.operation)) return;
    const next = { ...entry.settings, ...patch };
    if (sameSettings(next, entry.settings)) return;
    entry.settings = next;
    entry.editVersion++;
    entry.error = undefined;
    entry.lastAccess = Date.now();
    scheduleSave(entry);
    publish(draftId);
  }, [publish, scheduleSave]);

  const patchContentFor = useCallback((draftId: string, generation: number, patch: Partial<PersistentComposerDraft> | ((content: PersistentComposerDraft) => PersistentComposerDraft)) => {
    const entry = entriesRef.current.get(draftId);
    if (!entry || entry.generation !== generation) return;
    const base = entry.deferredContent ?? entry.content;
    updateContentFor(draftId, generation, typeof patch === "function" ? patch(cloneContent(base)) : { ...base, ...patch });
  }, [updateContentFor]);

  const isCurrentHandle = useCallback((draftId: string, generation: number) => {
    const entry = entriesRef.current.get(draftId);
    return Boolean(entry && entry.lifecycle === "active" && entry.generation === generation);
  }, []);

  const canEditHandle = useCallback((draftId: string, generation: number) => {
    const entry = entriesRef.current.get(draftId);
    return Boolean(!acceptingExit.current && entry && entry.lifecycle === "active" && entry.generation === generation && !entry.preparingSubmission && !entry.discarding && !draftSubmissionLocksEditing(entry.operation));
  }, []);

  const updateContent = useCallback((content: PersistentComposerDraft) => {
    const id = visibleDraftIdRef.current;
    const entry = id ? entriesRef.current.get(id) : undefined;
    if (id && entry) updateContentFor(id, entry.generation, content);
  }, [updateContentFor]);

  const updateSettings = useCallback((patch: Partial<SessionDraftSettings>) => {
    const id = visibleDraftIdRef.current;
    const entry = id ? entriesRef.current.get(id) : undefined;
    if (id && entry) updateSettingsFor(id, entry.generation, patch);
  }, [updateSettingsFor]);

  const open = useCallback(async (scope: string, workspaceRoot: string) => {
    if (acceptingExit.current) return;
    const intent = claimIntent();
    const sequence = ++openSequence.current;
    const sourceId = visibleDraftIdRef.current;
    if (sourceId) void flushDraft(sourceId);
    const cached = [...entriesRef.current.values()].find(entry => entry.lifecycle === "active"
      && entry.draft.scope === scope && (scope !== "project" || entry.draft.workspaceRoot === workspaceRoot));
    if (cached) {
      cached.visibleIntent = intent;
      cached.lastAccess = Date.now();
      visibleDraftIdRef.current = cached.draft.id;
      publish(cached.draft.id);
      void queueRestoreTarget(cached.draft.id, intent);
      window.requestAnimationFrame(() => {
        if (sequence === openSequence.current && intentCurrent(intent)) document.getElementById("composer-input")?.focus();
      });
    }
    const draft = await app.OpenSessionDraftForTarget(scope, scope === "project" ? workspaceRoot : "");
    if (sequence !== openSequence.current || !intentCurrent(intent)) return;
    await installDraft(draft, sequence, intent);
    if (sequence === openSequence.current && intentCurrent(intent)) await refreshSummaries();
  }, [claimIntent, flushDraft, installDraft, intentCurrent, publish, queueRestoreTarget, refreshSummaries]);

  const initializeEmptySurface = useCallback(async () => {
    const baselineIntent = currentNavigationIntent?.() ?? localIntent.current;
    const sequence = ++openSequence.current;
    const restored = await app.RestoreSessionDraft();
    if (sequence !== openSequence.current || !intentCurrent(baselineIntent)) return;
    if (restored) {
      await installDraft(restored, sequence, claimIntent());
      return;
    }
    const tabs = await app.ListTabs();
    if (sequence !== openSequence.current || !intentCurrent(baselineIntent) || tabs.length > 0) return;
    const intent = claimIntent(), draft = await app.OpenSessionDraftForTarget("global", "");
    if (sequence !== openSequence.current || !intentCurrent(intent)) return;
    await installDraft(draft, sequence, intent);
    await refreshSummaries();
  }, [claimIntent, currentNavigationIntent, installDraft, intentCurrent, refreshSummaries]);

  const dismiss = useCallback(() => {
    ++openSequence.current;
    const id = visibleDraftIdRef.current;
    visibleDraftIdRef.current = null;
    publish(null);
    if (id) void flushDraft(id);
    const next = restoreChain.current.catch(() => undefined).then(async () => {
      if (id) await app.DismissSessionDraft(id);
      else await app.SetSessionDraftRestoreTarget("");
    });
    restoreChain.current = next;
    return next;
  }, [flushDraft, publish]);

  const useSavedConflict = useCallback(() => {
    const id = visibleDraftIdRef.current;
    const entry = id ? entriesRef.current.get(id) : undefined;
    if (!id || !entry?.conflict) return;
    const saved = entry.conflict;
    entry.generation++;
    entry.pendingTasks.clear();
    entry.draft = saved;
    entry.content = parseContent(saved.contentJson);
    entry.settings = saved.settings;
    entry.editVersion++;
    entry.savedEditVersion = entry.editVersion;
    entry.conflict = undefined;
    entry.error = undefined;
    publish(id);
  }, [publish]);

  const keepLocalConflict = useCallback(async () => {
    const id = visibleDraftIdRef.current;
    const entry = id ? entriesRef.current.get(id) : undefined;
    if (!id || !entry?.conflict) return;
    entry.draft = entry.conflict;
    entry.conflict = undefined;
    entry.error = undefined;
    publish(id);
    await flushDraft(id, entry.editVersion);
  }, [flushDraft, publish]);

  const retrySave = useCallback(async () => {
    const id = visibleDraftIdRef.current;
    const entry = id ? entriesRef.current.get(id) : undefined;
    if (!id || !entry || entry.lifecycle !== "active" || entry.conflict) return;
    entry.error = undefined;
    publish(id);
    await flushDraft(id, entry.editVersion);
  }, [flushDraft, publish]);

  const setMCPEnabled = useCallback((server: ServerView, enabled: boolean) => {
    const id = visibleDraftIdRef.current;
    const entry = id ? entriesRef.current.get(id) : undefined;
    if (!id || !entry) return;
    const disabledMcp = { ...entry.settings.disabledMcp };
    if (enabled) delete disabledMcp[server.name];
    else disabledMcp[server.name] = server;
    const mcpOrder = entry.settings.mcpOrder.includes(server.name)
      ? entry.settings.mcpOrder
      : [...entry.settings.mcpOrder, server.name];
    updateSettingsFor(id, entry.generation, { disabledMcp, mcpOrder });
  }, [updateSettingsFor]);

  const trackTask = useCallback(<T,>(draftId: string, generation: number, promise: Promise<T>): Promise<T> => {
    const entry = entriesRef.current.get(draftId);
    const taskId = crypto.randomUUID();
    const tracked = promise.finally(() => {
      allTasks.current.delete(taskId);
      const current = entriesRef.current.get(draftId);
      if (!current) return;
      current.pendingTasks.delete(taskId);
      publish(draftId);
    });
    allTasks.current.set(taskId, tracked);
    if (entry?.lifecycle === "active" && entry.generation === generation) entry.pendingTasks.set(taskId, tracked);
    publish(draftId);
    return tracked;
  }, [publish]);

  const reportTaskError = useCallback((draftId: string, generation: number, message: string) => {
    const entry = entriesRef.current.get(draftId);
    if (!entry || entry.lifecycle !== "active" || entry.generation !== generation) return;
    entry.taskError = message || undefined;
    publish(draftId);
  }, [publish]);

  const captureSubmission = useCallback((draftId: string, generation: number, content?: PersistentComposerDraft): DraftSubmissionCapture | null => {
    if (acceptingExit.current) return null;
    const entry = entriesRef.current.get(draftId);
    if (!entry || entry.lifecycle !== "active" || entry.generation !== generation) return null;
    if (entry.preparingSubmission || entry.discarding || entry.conflict || entry.pendingTasks.size || draftSubmissionLocksEditing(entry.operation)) return null;
    if (content) updateContentFor(draftId, generation, content);
    const capture = Object.freeze({
      handle: Object.freeze({ draftId, generation }),
      preparationId: crypto.randomUUID(),
      draftId,
      workspaceId: entry.draft.workspaceId,
      generation,
      editVersion: entry.editVersion,
      navigationIntent: entry.visibleIntent,
      content: cloneContent(entry.content),
      settings: cloneSettings(entry.settings),
    });
    const commandName = /^\/([^\s]+)/.exec(entry.content.text.trim())?.[1] ?? "";
    if (["model", "effort", "theme"].includes(commandName) || entry.commands.some((command) => command.name === commandName && command.draftBehavior && command.draftBehavior !== "submit")) return capture;
    entry.preparation = capture;
    entry.preparingSubmission = true;
    let resolve!: () => void;
    const promise = new Promise<void>((done) => { resolve = done; });
    preparationBarriers.current.set(capture.preparationId, { promise, resolve });
    publish(draftId);
    return capture;
  }, [publish, updateContentFor]);

  const releasePreparation = useCallback((value: unknown) => {
    const capture = value as DraftSubmissionCapture | undefined;
    if (!capture?.preparationId) return;
    const entry = entriesRef.current.get(capture.draftId);
    if (entry?.generation === capture.generation && entry.preparation?.preparationId === capture.preparationId) {
      entry.preparation = undefined;
      entry.preparingSubmission = false;
      publish(capture.draftId);
    }
    preparationBarriers.current.get(capture.preparationId)?.resolve();
    preparationBarriers.current.delete(capture.preparationId);
  }, [publish]);

  const flushPreparation = useCallback(async (value: unknown) => {
    const capture = value as DraftSubmissionCapture | undefined;
    if (!capture) return;
    const entry = entriesRef.current.get(capture.draftId);
    if (entry?.preparation?.preparationId !== capture.preparationId) return;
    const saved = await flushDraft(capture.draftId, capture.editVersion);
    if (!saved || canonicalJSON(parseContent(saved.contentJson)) !== canonicalJSON(parseContent(contentJSON(capture.content))) || !sameSettings(saved.settings, capture.settings)) throw new Error("Save the captured draft before submitting.");
  }, [flushDraft]);

  const waitForSubmission = useCallback(async (initial: SessionDraftSubmissionView, capture: DraftSubmissionCapture) => {
    let operation = initial;
    const started = Date.now();
    let failures = 0;
    for (;;) {
      if (disposed.current) return;
      if (convertedOperations.current.has(initial.operationId)) return;
      const entry = entriesRef.current.get(capture.draftId);
      if (entry?.operation && entry.operation.operationId !== initial.operationId) return;
      if (entry?.lifecycle === "converted" && operation.phase === "accepted") return;
      if (entry && entry.generation === capture.generation) {
        if (entry.operation?.operationId === operation.operationId && (entry.operation.revision ?? 0) > (operation.revision ?? 0)) operation = entry.operation;
        entry.operation = operation;
        publish(capture.draftId);
      }
      if (operation.phase === "accepted") {
        convertedOperations.current.add(operation.operationId);
        await refreshSummaries();
        const current = entriesRef.current.get(capture.draftId);
        const ownsPage = Boolean(operation.session && current && current.generation === capture.generation
          && visibleDraftIdRef.current === capture.draftId && intentCurrent(capture.navigationIntent));
        if (current && current.generation === capture.generation) {
          current.lifecycle = "converted";
          current.pendingTasks.clear();
          if (visibleDraftIdRef.current !== capture.draftId || ownsPage) entriesRef.current.delete(capture.draftId);
        }
        if (ownsPage) {
          visibleDraftIdRef.current = null;
          publish(null);
        } else publish(capture.draftId);
        if (ownsPage && operation.session) await onAccepted(operation.session);
        onChanged();
        return;
      }
      if (["terminal_failed", "cancelled", "resume_required", "runtime_failed"].includes(operation.phase)) {
        if (operation.phase === "terminal_failed") throw new Error(operation.error || "Unable to start this session.");
        return;
      }
      const delay = failures || operation.phase === "dispatch_unknown" ? [1000, 2000, 5000][Math.min(failures, 2)] : Date.now() - started < 5000 ? 250 : 1000;
      await new Promise((resolve) => window.setTimeout(resolve, delay));
      if (disposed.current) return;
      try { operation = await app.GetDraftSubmission(initial.operationId); failures = 0; }
      catch { failures++; }
    }
  }, [intentCurrent, onAccepted, onChanged, publish, refreshSummaries]);

  const waitForSubmissionOnce = useCallback((operation: SessionDraftSubmissionView, capture: DraftSubmissionCapture) => {
    const existing = submissionWaits.current.get(operation.operationId);
    if (existing) return existing;
    const pending = waitForSubmission(operation, capture).finally(() => {
      if (submissionWaits.current.get(operation.operationId) === pending) submissionWaits.current.delete(operation.operationId);
    });
    submissionWaits.current.set(operation.operationId, pending);
    return pending;
  }, [waitForSubmission]);
  observeOperation.current = waitForSubmissionOnce;

  const submitFrom = useCallback(async (
    draftId: string,
    generation: number,
    display: string,
    input = display,
    _tabId?: string,
    structured?: StructuredInvocationSubmit,
    capturedValue?: unknown,
  ) => {
    const capture = captureMatches(capturedValue, draftId, generation) ? capturedValue : captureSubmission(draftId, generation);
    if (!capture) return;
    const inFlight = submissionStarts.current.get(draftId);
    if (inFlight) return inFlight;
    const pending = (async () => {
      const source = entriesRef.current.get(draftId);
      if (!source || source.generation !== generation || source.lifecycle !== "active") return;
      const trimmedDisplay = display.trim();
      const commandName = /^\/([^\s]+)/.exec(trimmedDisplay)?.[1] ?? "";
      const command = source.commands.find((item) => item.name === commandName);
      if (["model", "effort", "theme"].includes(commandName) || (command?.draftBehavior && command.draftBehavior !== "submit")) releasePreparation(capture);
      if (command?.draftBehavior === "unavailable") throw new Error("This command needs an existing session.");
      const model = /^\/model\s+(\S+)$/.exec(trimmedDisplay);
      if (model) { updateSettingsFor(draftId, generation, { model: model[1] }); return; }
      const effort = /^\/effort\s+(\S+)$/.exec(trimmedDisplay);
      if (effort) { updateSettingsFor(draftId, generation, { effort: effort[1] }); return; }
      const theme = /^\/theme\s+(\S+)$/.exec(trimmedDisplay);
      if (theme) {
        const value = theme[1].toLowerCase();
        const experience = await import("../lib/themeExperience");
        if (value === "auto" || value === "light" || value === "dark") {
          await experience.setThemeMode(value);
          return;
        }
        const themeModule = await import("../lib/theme");
        const known = new Set(["graphite", "aurora", "slate", "carbon", "nocturne", "amber", "ember", "midnight", "sandstone", "porcelain", "linen", "glacier"]);
        if (!known.has(value)) throw new Error(`Unknown theme: ${value}`);
        await experience.activateBaseStyle(themeModule.normalizeThemeStyleForTheme(value));
        return;
      }
      if (command?.draftBehavior === "direct") throw new Error("This command needs an argument and does not create a session.");
      if (source.pendingTasks.size > 0) throw new Error("Wait for attachments to finish before sending.");
      source.preparingSubmission = true;
      publish(draftId);
      const saved = await flushDraft(draftId, capture.editVersion);
      if (!saved || saved.id !== draftId) throw new Error("Resolve the draft save conflict before sending.");
      if (contentJSON(parseContent(saved.contentJson)) !== contentJSON(capture.content) || !sameSettings(saved.settings, capture.settings)) throw new Error("The draft changed after submission was captured. Review it before sending.");
      const shell = trimmedDisplay.startsWith("!");
      let requestDisplay = structured?.display ?? display;
      let requestInput = shell ? trimmedDisplay.slice(1).trim() : structured?.input ?? input;
      let goal = capture.settings.goal;
      let collaborationMode = capture.settings.collaborationMode;
      let toolApprovalMode = capture.settings.toolApprovalMode;
      if (!shell && collaborationMode === "goal" && !goal) {
        const initial = buildInitialGoalSubmission(
          { display: requestDisplay, submit: requestInput, structured },
          collaborationMode as CollaborationMode,
          toolApprovalMode as ToolApprovalMode,
        );
        requestDisplay = initial.display;
        requestInput = initial.submit ?? initial.display;
        goal = initial.initialGoal?.goal ?? "";
        collaborationMode = initial.initialGoal?.collaborationMode ?? collaborationMode;
        toolApprovalMode = initial.initialGoal?.toolApprovalMode ?? toolApprovalMode;
      }
      const request = {
        snapshotVersion: 4,
        requestId: capture.preparationId,
        sourceDigest: saved.snapshotDigest ?? "",
        draftId,
        revision: saved.revision,
        kind: shell ? "shell" : "turn",
        display: requestDisplay,
        input: requestInput,
        invocations: structured?.invocations ?? [],
        goal,
        collaborationMode,
        toolApprovalMode,
        workspaceRefs: capture.content.workspaceRefs,
        settings: capture.settings,
      };
      let operation: SessionDraftSubmissionView;
      try { operation = await app.BeginDraftSubmission(request); }
      catch (error) {
        if (String(error).includes("draft submission not admitted:") || String(error).includes("reasonix_error:")) throw error;
        // A transport error does not prove that Begin failed. Keep the source
        // frozen while read-only reconciliation is unavailable.
        for (;;) {
          if (disposed.current) throw error;
          let state;
          try { state = await app.GetSessionDraftState(draftId); }
          catch {
            source.error = "Verifying whether the submission was received. Reconnecting…";
            publish(draftId);
            await new Promise(resolve => window.setTimeout(resolve, 2000));
            continue;
          }
          if (state.operation && (!state.operation.requestId || state.operation.requestId === capture.preparationId)) {
            operation = state.operation;
          } else {
            // A read may race the original Begin before its transaction. Retry
            // the exact request ID, whose backend lock serializes the decision.
            try { operation = await app.BeginDraftSubmission(request); }
            catch (retryError) {
              if (String(retryError).includes("draft submission not admitted:") || String(retryError).includes("reasonix_error:")) throw retryError;
              source.error = "Verifying whether the submission was received. Reconnecting…";
              publish(draftId);
              await new Promise(resolve => window.setTimeout(resolve, 2000));
              continue;
            }
          }
          source.error = undefined;
          break;
        }
      }
      const current = entriesRef.current.get(draftId);
      if (current && current.generation === generation) {
        current.operation = operation;
        current.operationCapture = capture;
        publish(draftId);
      }
      releasePreparation(capture);
      await waitForSubmissionOnce(operation, capture);
    })().finally(() => {
      releasePreparation(capture);
      if (submissionStarts.current.get(draftId) === pending) submissionStarts.current.delete(draftId);
    });
    submissionStarts.current.set(draftId, pending);
    return pending;
  }, [captureSubmission, flushDraft, publish, releasePreparation, updateSettingsFor, waitForSubmissionOnce]);

  const submit = useCallback((display: string, input = display, tabId?: string, structured?: StructuredInvocationSubmit, captured?: unknown) => {
    const id = visibleDraftIdRef.current;
    const entry = id ? entriesRef.current.get(id) : undefined;
    if (!id || !entry) return Promise.resolve();
    return submitFrom(id, entry.generation, display, input, tabId, structured, captured);
  }, [submitFrom]);

  const cancelSubmission = useCallback(async () => {
    const id = visibleDraftIdRef.current;
    const entry = id ? entriesRef.current.get(id) : undefined;
    const operation = entry?.operation;
    if (!id || !entry || !operation) return;
    const generation = entry.generation;
    const next = await app.CancelDraftSubmission(operation.operationId);
    const current = entriesRef.current.get(id);
    if (current && current.generation === generation && current.operation?.operationId === operation.operationId) {
      if ((current.operation.revision ?? 0) > (next.revision ?? 0)) return;
      current.operation = next;
      publish(id);
      if (current.operationCapture) void (next.phase === "accepted" ? waitForSubmission(next, current.operationCapture) : waitForSubmissionOnce(next, current.operationCapture)).catch(() => undefined);
    }
  }, [publish, waitForSubmission, waitForSubmissionOnce]);

  const resumeSubmission = useCallback(async () => {
    const id = visibleDraftIdRef.current;
    const entry = id ? entriesRef.current.get(id) : undefined;
    if (!id || !entry?.operation) return;
    const capture = { handle: { draftId: id, generation: entry.generation }, preparationId: "", draftId: id, generation: entry.generation, workspaceId: entry.draft.workspaceId, editVersion: entry.editVersion, navigationIntent: entry.visibleIntent, content: cloneContent(entry.content), settings: cloneSettings(entry.settings) };
    try {
      const next = await app.ResumeDraftSubmission(entry.operation.operationId, entry.operation.revision);
      entry.operationCapture = capture;
      await waitForSubmissionOnce(next, capture);
    } catch (error) {
      if (entriesRef.current.get(id) === entry) { entry.error = String(error); publish(id); }
    }
  }, [publish, waitForSubmissionOnce]);

  const openAcceptedSession = useCallback(async () => {
    const entry = entriesRef.current.get(visibleDraftIdRef.current ?? "");
    if (entry?.operation?.phase === "accepted" && entry.operation.session) await onAccepted(entry.operation.session);
  }, [onAccepted]);

  const refreshSubmission = useCallback(async () => {
    const entry = entriesRef.current.get(visibleDraftIdRef.current ?? "");
    if (!entry?.operation || !entry.operationCapture) return;
    const operationId = entry.operation.operationId;
    const generation = entry.generation;
    try {
      const next = await app.GetDraftSubmission(operationId);
      if (entriesRef.current.get(entry.draft.id) !== entry || entry.generation !== generation || entry.operation?.operationId !== operationId) return;
      if ((entry.operation.revision ?? 0) <= (next.revision ?? 0)) entry.operation = next;
      publish(entry.draft.id);
      void waitForSubmissionOnce(entry.operation, entry.operationCapture).catch(() => undefined);
    } catch { /* Connection state does not replace the durable operation. */ }
  }, [publish, waitForSubmissionOnce]);

  useEffect(() => {
    const online = () => {
      for (const entry of entriesRef.current.values()) {
        if (entry.lifecycle !== "active" || !entry.operation || !entry.operationCapture || !draftSubmissionLocksEditing(entry.operation)) continue;
        const id = entry.operation.operationId;
        void app.GetDraftSubmission(id).then(next => {
          if (entry.operation?.operationId === id && entry.operation.revision <= next.revision) {
            entry.operation = next;
            publish(entry.draft.id);
            void observeOperation.current(next, entry.operationCapture!).catch(() => undefined);
          }
        }).catch(() => undefined);
      }
    };
    window.addEventListener("online", online);
    return () => window.removeEventListener("online", online);
  }, [publish]);

  const discard = useCallback(async (handle?: DraftHandle, expectedVersion?: number, expectedRevision?: number, expectedIntent?: number) => {
    const id = handle?.draftId ?? visibleDraftIdRef.current;
    const entry = id ? entriesRef.current.get(id) : undefined;
    if (!id || !entry) return;
    const generation = handle?.generation ?? entry.generation;
    const intent = expectedIntent ?? entry.visibleIntent;
    if (entry.generation !== generation || (expectedVersion != null && entry.editVersion !== expectedVersion)
      || (expectedRevision != null && (entry.conflict?.revision ?? entry.draft.revision) !== expectedRevision)) throw new Error("The draft changed. Confirm discarding its current contents again.");
    if (entry.preparingSubmission || draftSubmissionLocksEditing(entry.operation)) throw new Error("Cancel the submission before discarding this draft.");
    entry.discarding = true;
    publish(id);
    try { await app.DiscardSessionDraft(id, expectedRevision ?? entry.conflict?.revision ?? entry.draft.revision); }
    catch (error) {
      entry.discarding = false;
      if (entry.deferredContent) { const content = entry.deferredContent; entry.deferredContent = undefined; updateContentFor(id, generation, content); }
      publish(id);
      throw error;
    }
    entry.lifecycle = "discarded";
    entry.generation++;
    if (entry.timer != null) window.clearTimeout(entry.timer);
    entriesRef.current.delete(id);
    if (visibleDraftIdRef.current === id && intentCurrent(intent)) {
      visibleDraftIdRef.current = null;
      publish(null);
    }
    await refreshSummaries();
    onChanged();
  }, [intentCurrent, onChanged, publish, refreshSummaries, updateContentFor]);

  const confirmDiscard = useCallback(async (labels: { title: string; message: string; detail: string; confirmLabel: string; cancelLabel: string }) => {
    const id = visibleDraftIdRef.current;
    const entry = id ? entriesRef.current.get(id) : undefined;
    if (!entry) return;
    if (entry.savePromise) await entry.savePromise;
    const handle = { draftId: entry.draft.id, generation: entry.generation };
    const version = entry.editVersion;
    const revision = entry.conflict?.revision ?? entry.draft.revision;
    const intent = entry.visibleIntent;
    const content = entry.content;
    const hasContent = Boolean(content.text.trim() || content.attachments.length || content.workspaceRefs.length || content.invocations.length || content.pastedBlocks.length || content.sessionRefs.length || content.selectedTextRefs.length);
    if (hasContent) {
      const confirmed = await app.ConfirmAction({ ...labels, detail: `${entry.draft.workspaceRoot || "Global workspace"}\n${labels.detail}`, destructive: true });
      if (!confirmed) return;
    }
    await discard(handle, version, revision, intent);
  }, [discard]);

  const flush = useCallback(() => {
    const id = visibleDraftIdRef.current;
    return id ? flushDraft(id) : Promise.resolve(null);
  }, [flushDraft]);

  useEffect(() => { disposed.current = false; const entries = entriesRef.current; return () => {
    disposed.current = true;
    for (const entry of entries.values()) {
      if (entry.timer != null) window.clearTimeout(entry.timer);
      void startSaveLoop(entry.draft.id);
    }
  }; }, [startSaveLoop]);

  useEffect(() => {
    const flushBeforeUnload = () => {
      for (const entry of entriesRef.current.values()) void startSaveLoop(entry.draft.id);
    };
    window.addEventListener("beforeunload", flushBeforeUnload);
    return () => window.removeEventListener("beforeunload", flushBeforeUnload);
  }, [startSaveLoop]);

  useEffect(() => {
    const flushAll = async () => {
      acceptingExit.current = true;
      try {
        while (allTasks.current.size) await Promise.allSettled([...allTasks.current.values()]);
        await Promise.all([...preparationBarriers.current.values()].map((barrier) => barrier.promise));
        const entries = [...entriesRef.current.values()].filter((entry) => entry.lifecycle === "active");
        await Promise.all(entries.map(async (entry) => {
          for (;;) {
            await Promise.allSettled([...entry.pendingTasks.values()]);
            const requiredVersion = entry.editVersion;
            const saved = await flushDraft(entry.draft.id, requiredVersion);
            if (entry.conflict || entry.error) {
              throw new Error(`Draft ${entry.draft.id} could not be saved before exit.`);
            }
            if (entry.pendingTasks.size === 0 && saved && entry.savedEditVersion >= entry.editVersion) break;
            if (entry.lifecycle !== "active") break;
          }
        }));
        await restoreChain.current;
      } catch (error) {
        acceptingExit.current = false;
        throw error;
      }
    };
    const resumeEditing = () => {
      acceptingExit.current = false;
    };
    window.__reasonixFlushSessionDraft = flushAll;
    window.__reasonixResumeSessionDraftEditing = resumeEditing;
    return () => {
      if (window.__reasonixFlushSessionDraft === flushAll) delete window.__reasonixFlushSessionDraft;
      if (window.__reasonixResumeSessionDraftEditing === resumeEditing) delete window.__reasonixResumeSessionDraftEditing;
    };
  }, [flushDraft]);

  return {
    surface,
    summaries,
    open,
    initializeEmptySurface,
    dismiss,
    flush,
    flushDraft,
    updateContent,
    updateContentFor,
    patchContentFor,
    isCurrentHandle,
    canEditHandle,
    updateSettings,
    updateSettingsFor,
    useSavedConflict,
    keepLocalConflict,
    retrySave,
    setMCPEnabled,
    trackTask,
    reportTaskError,
    captureSubmission,
    beginSubmissionPreparation: captureSubmission,
    releasePreparation,
    flushPreparation,
    submit,
    submitFrom,
    cancelSubmission,
    resumeSubmission,
    openAcceptedSession,
    refreshSubmission,
    discard,
    confirmDiscard,
    refreshSummaries,
  };
}
