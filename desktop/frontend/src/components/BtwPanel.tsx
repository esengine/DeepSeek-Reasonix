import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { AlertCircle, ArrowUp, MessageSquare, RefreshCw, ShieldCheck, Square, X } from "lucide-react";
import { app, onBtwEvent } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { initialState, reducer } from "../lib/useController";
import type { BtwStateView, WireEvent } from "../lib/types";
import { Transcript } from "./Transcript";
import { Tooltip } from "./Tooltip";

type ControllerState = typeof initialState;
type BtwEndReason = "idle" | "stopped";

interface BtwTabLocal {
  state: ControllerState;
  runtime: BtwStateView;
  draft: string;
  runtimeError: string;
  endedReason?: BtwEndReason;
}

interface BtwStartRequest {
  id: number;
  input: string;
}

const emptyRuntime: BtwStateView = {
  active: false,
  running: false,
  cancelRequested: false,
  cancellable: false,
};
const BTW_IDLE_TIMEOUT_NOTICE = "BTW side conversation closed after being idle.";
const BTW_RECONCILE_DELAY_MS = 900;

function freshControllerState(): ControllerState {
  return {
    ...initialState,
    items: [],
    context: { ...initialState.context },
    jobs: [],
    checkpoints: [],
  };
}

function freshTabLocal(): BtwTabLocal {
  return {
    state: freshControllerState(),
    runtime: { ...emptyRuntime },
    draft: "",
    runtimeError: "",
  };
}

function runtimeFromEvent(current: BtwStateView, event: WireEvent, state: ControllerState): BtwStateView {
  if (event.kind === "turn_started") {
    return { active: true, running: true, cancelRequested: false, cancellable: true };
  }
  if (event.kind === "turn_done") {
    return { ...current, running: false, cancelRequested: false, cancellable: false };
  }
  if (event.kind === "notice" && event.text === BTW_IDLE_TIMEOUT_NOTICE) {
    return { ...emptyRuntime };
  }
  return {
    ...current,
    active: true,
    running: state.running,
    cancelRequested: state.cancelRequested,
    cancellable: state.cancellable,
  };
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  return String(error);
}

function stagedDraft(current: string, incoming: string): string {
  const next = incoming.trim();
  if (!next) return current;
  if (!current.trim()) return next;
  if (current.trim() === next) return current;
  return `${current.trimEnd()}\n${next}`;
}

function restoredSubmittedDraft(current: string, submitted: string): string {
  return stagedDraft(submitted, current);
}

export function BtwPanel({
  tabId,
  sessionKey,
  tabSessionKeys,
  ready,
  sessionOpen = true,
  hasParentContext = true,
  modelLabel,
  visible = true,
  startRequest,
  onStartRequestHandled,
  sessionKeyMigrations,
  sessionKeyInvalidations,
  onHide,
  onEnd,
}: {
  tabId?: string;
  sessionKey?: string;
  tabSessionKeys?: Record<string, string>;
  ready: boolean;
  sessionOpen?: boolean;
  hasParentContext?: boolean;
  modelLabel: string;
  visible?: boolean;
  startRequest?: BtwStartRequest | null;
  onStartRequestHandled?: (id: number) => void;
  sessionKeyMigrations?: Array<{ from: string; to: string }>;
  sessionKeyInvalidations?: Array<{ from: string; to: string }>;
  onHide: () => void;
  onEnd: () => void;
}) {
  const t = useT();
  const [tabs, setTabs] = useState<Record<string, BtwTabLocal>>({});
  const tabsRef = useRef(tabs);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const composingRef = useRef(false);
  const handledStartRequestsRef = useRef<Set<number>>(new Set());
  const inFlightKeysRef = useRef<Set<string>>(new Set());
  const optimisticPromptsRef = useRef<Map<string, { epoch: number; text: string }>>(new Map());
  const operationEpochsRef = useRef<Map<string, number>>(new Map());
  const closedKeysRef = useRef<Set<string>>(new Set());
  const keyAliasesRef = useRef<Map<string, string>>(new Map());
  const localKey = sessionKey || tabId || "";
  const current = localKey ? tabs[localKey] ?? freshTabLocal() : freshTabLocal();
  const draft = current.draft;
  const running = current.runtime.running || current.state.running;
  const active = current.runtime.active;
  const inputUnavailable = !tabId || !localKey || !ready;
  const canSubmit = active || hasParentContext;
  const transcriptTabId = localKey ? `btw-${encodeURIComponent(localKey)}` : "btw";

  const resolveLocalKey = useCallback((id: string) => {
    let resolved = id;
    const visited = new Set<string>();
    while (keyAliasesRef.current.has(resolved) && !visited.has(resolved)) {
      visited.add(resolved);
      resolved = keyAliasesRef.current.get(resolved)!;
    }
    return resolved;
  }, []);

  const updateLocal = useCallback((id: string, update: (current: BtwTabLocal) => BtwTabLocal) => {
    const resolvedId = resolveLocalKey(id);
    setTabs((previous) => {
      const next = { ...previous, [resolvedId]: update(previous[resolvedId] ?? freshTabLocal()) };
      tabsRef.current = next;
      return next;
    });
  }, [resolveLocalKey]);

  const deleteLocal = useCallback((id: string) => {
    const resolvedId = resolveLocalKey(id);
    setTabs((previous) => {
      if (!(resolvedId in previous)) return previous;
      const next = { ...previous };
      delete next[resolvedId];
      tabsRef.current = next;
      return next;
    });
  }, [resolveLocalKey]);

  const bumpOperationEpoch = useCallback((id: string) => {
    const resolvedId = resolveLocalKey(id);
    const next = (operationEpochsRef.current.get(resolvedId) ?? 0) + 1;
    operationEpochsRef.current.set(resolvedId, next);
    return next;
  }, [resolveLocalKey]);

  const operationEpoch = useCallback((id: string) => operationEpochsRef.current.get(resolveLocalKey(id)) ?? 0, [resolveLocalKey]);

  const focusInput = useCallback(() => {
    window.requestAnimationFrame(() => inputRef.current?.focus());
  }, []);

  const setLocalDraft = useCallback((value: string) => {
    if (!localKey) return;
    updateLocal(localKey, (entry) => ({ ...entry, draft: value }));
  }, [localKey, updateLocal]);

  const stagePrompt = useCallback((value: string) => {
    if (!localKey) return;
    updateLocal(localKey, (entry) => ({ ...entry, draft: stagedDraft(entry.draft, value) }));
    focusInput();
  }, [focusInput, localKey, updateLocal]);

  useEffect(() => {
    for (const { from, to } of sessionKeyMigrations ?? []) {
      if (!from || !to || from === to) continue;
      keyAliasesRef.current.set(from, to);
      setTabs((previous) => {
        const source = previous[from];
        if (!source) return previous;
        const next = { ...previous, [to]: source };
        delete next[from];
        tabsRef.current = next;
        return next;
      });
      const sourceEpoch = operationEpochsRef.current.get(from);
      if (sourceEpoch !== undefined) {
        operationEpochsRef.current.set(to, Math.max(sourceEpoch, operationEpochsRef.current.get(to) ?? 0));
        operationEpochsRef.current.delete(from);
      }
      if (closedKeysRef.current.delete(from)) closedKeysRef.current.add(to);
      if (inFlightKeysRef.current.delete(from)) inFlightKeysRef.current.add(to);
      const optimisticPrompt = optimisticPromptsRef.current.get(from);
      if (optimisticPrompt) {
        optimisticPromptsRef.current.set(to, optimisticPrompt);
        optimisticPromptsRef.current.delete(from);
      }
    }
  }, [sessionKeyMigrations]);

  useEffect(() => {
    for (const { from } of sessionKeyInvalidations ?? []) {
      const id = resolveLocalKey(from);
      if (!id) continue;
      const optimisticPrompt = optimisticPromptsRef.current.get(id);
      if (!closedKeysRef.current.has(id)) bumpOperationEpoch(id);
      closedKeysRef.current.add(id);
      inFlightKeysRef.current.delete(id);
      optimisticPromptsRef.current.delete(id);
      setTabs((previous) => {
        const entry = previous[id];
        if (!entry) return previous;
        const next = {
          ...previous,
          [id]: {
            ...entry,
            state: reducer(entry.state, {
              type: "backend_status",
              running: false,
              pendingPrompt: false,
              backgroundJobs: 0,
              cancelRequested: false,
              cancellable: false,
            }),
            runtime: { ...emptyRuntime },
            draft: optimisticPrompt
              ? restoredSubmittedDraft(entry.draft, optimisticPrompt.text)
              : entry.draft,
            runtimeError: "",
            endedReason: "stopped" as const,
          },
        };
        tabsRef.current = next;
        return next;
      });
    }
  }, [bumpOperationEpoch, resolveLocalKey, sessionKeyInvalidations]);

  useEffect(() => {
    return onBtwEvent((event) => {
      const eventTabId = event.tabId || tabId;
      const rawId = (eventTabId && tabSessionKeys?.[eventTabId]) || (eventTabId === tabId ? localKey : eventTabId) || localKey;
      const id = resolveLocalKey(rawId);
      const knownSession = Boolean(tabsRef.current[id]) || (sessionOpen && id === localKey);
      if (!id || !knownSession || closedKeysRef.current.has(id)) return;
      const expired = event.kind === "notice" && event.text === BTW_IDLE_TIMEOUT_NOTICE;
      const optimisticPrompt = optimisticPromptsRef.current.get(id);
      if (expired) {
        bumpOperationEpoch(id);
        closedKeysRef.current.add(id);
        optimisticPromptsRef.current.delete(id);
      } else if (event.kind === "turn_started") {
        optimisticPromptsRef.current.delete(id);
      }
      updateLocal(id, (entry) => {
        const eventState = reducer(entry.state, { type: "event", e: event });
        const state = expired
          ? reducer(eventState, {
              type: "backend_status",
              running: false,
              pendingPrompt: false,
              backgroundJobs: 0,
              cancelRequested: false,
              cancellable: false,
            })
          : eventState;
        return {
          ...entry,
          state,
          runtime: runtimeFromEvent(entry.runtime, event, state),
          draft: expired && optimisticPrompt
            ? restoredSubmittedDraft(entry.draft, optimisticPrompt.text)
            : entry.draft,
          runtimeError: event.kind === "turn_started" ? "" : entry.runtimeError,
          endedReason: expired ? "idle" : event.kind === "turn_started" ? undefined : entry.endedReason,
        };
      });
    });
  }, [bumpOperationEpoch, localKey, resolveLocalKey, sessionOpen, tabId, tabSessionKeys, updateLocal]);

  useEffect(() => {
    if (!sessionOpen || !tabId || !localKey) return;
    let cancelled = false;
    const epoch = operationEpoch(localKey);
    void app.BtwStateForTab(tabId)
      .then((runtime) => {
        if (cancelled || operationEpoch(localKey) !== epoch) return;
        updateLocal(localKey, (entry) => {
          if (entry.runtime.running || entry.state.running) return entry;
          if (closedKeysRef.current.has(localKey) && runtime.active) return entry;
          return {
            ...entry,
            runtime,
            runtimeError: "",
            endedReason: runtime.active ? undefined : entry.endedReason,
          };
        });
      })
      .catch((error) => {
        if (cancelled || operationEpoch(localKey) !== epoch) return;
        updateLocal(localKey, (entry) => ({ ...entry, runtimeError: errorMessage(error) }));
      });
    return () => {
      cancelled = true;
    };
  }, [localKey, operationEpoch, sessionOpen, tabId, updateLocal]);

  useEffect(() => {
    if (!visible || !localKey) return;
    focusInput();
  }, [focusInput, localKey, visible]);

  useLayoutEffect(() => {
    const node = inputRef.current;
    if (!node) return;
    node.style.height = "0px";
    const nextHeight = Math.min(132, Math.max(44, node.scrollHeight));
    node.style.height = `${nextHeight}px`;
    node.style.overflowY = node.scrollHeight > 132 ? "auto" : "hidden";
  }, [draft]);

  const reconcileOptimisticStart = useCallback((id: string, targetTabId: string, epoch: number, text: string) => {
    window.setTimeout(() => {
      const resolvedId = resolveLocalKey(id);
      if (operationEpoch(id) !== epoch || closedKeysRef.current.has(resolvedId)) return;
      const latest = tabsRef.current[resolvedId];
      if (!latest || (!latest.runtime.running && !latest.state.running)) return;
      void app.BtwStateForTab(targetTabId)
        .then((runtime) => {
          const latestId = resolveLocalKey(id);
          if (operationEpoch(id) !== epoch || closedKeysRef.current.has(latestId)) return;
          const next = tabsRef.current[latestId];
          if (!next || (!next.runtime.running && !next.state.running)) return;
          if (runtime.running) {
            if (optimisticPromptsRef.current.get(latestId)?.epoch === epoch) {
              optimisticPromptsRef.current.delete(latestId);
            }
            updateLocal(latestId, (entry) => ({ ...entry, runtime }));
            return;
          }
          if (optimisticPromptsRef.current.get(latestId)?.epoch === epoch) {
            optimisticPromptsRef.current.delete(latestId);
          }
          updateLocal(latestId, (entry) => ({
            ...entry,
            state: reducer(entry.state, { type: "send_failed", error: t("btw.errorNotStarted") }),
            runtime: { ...runtime, running: false, cancelRequested: false, cancellable: false },
            draft: restoredSubmittedDraft(entry.draft, text),
            runtimeError: t("btw.errorNotStarted"),
          }));
        })
        .catch(() => undefined);
    }, BTW_RECONCILE_DELAY_MS);
  }, [operationEpoch, resolveLocalKey, t, updateLocal]);

  const submitPrompt = useCallback(async (rawText: string, clearDraft: boolean): Promise<boolean> => {
    if (!tabId || !localKey || !ready) return false;
    const text = rawText.trim();
    const operationKey = resolveLocalKey(localKey);
    if (!text || inFlightKeysRef.current.has(operationKey)) return false;
    const latest = tabsRef.current[operationKey] ?? freshTabLocal();
    const wasActive = latest.runtime.active;
    if (latest.runtime.running || latest.state.running || (!wasActive && !hasParentContext)) return false;

    closedKeysRef.current.delete(operationKey);
    inFlightKeysRef.current.add(operationKey);
    const epoch = bumpOperationEpoch(operationKey);
    optimisticPromptsRef.current.set(operationKey, { epoch, text });
    updateLocal(operationKey, (entry) => ({
      ...entry,
      draft: clearDraft ? "" : entry.draft,
      state: reducer(entry.state, { type: "user", text, seq: entry.state.seq }),
      runtime: { active: true, running: true, cancelRequested: false, cancellable: true },
      runtimeError: "",
      endedReason: undefined,
    }));

    try {
      if (wasActive) {
        await app.SubmitBtwForTab(tabId, text);
      } else {
        await app.StartBtwForTab(tabId, text);
      }
      if (operationEpoch(operationKey) !== epoch) {
        const resolvedKey = resolveLocalKey(operationKey);
        if (optimisticPromptsRef.current.get(resolvedKey)?.epoch === epoch) {
          optimisticPromptsRef.current.delete(resolvedKey);
        }
        return false;
      }
      reconcileOptimisticStart(operationKey, tabId, epoch, text);
      return true;
    } catch (error) {
      const resolvedKey = resolveLocalKey(operationKey);
      const promptStillOptimistic = optimisticPromptsRef.current.get(resolvedKey)?.epoch === epoch;
      if (operationEpoch(operationKey) === epoch && promptStillOptimistic) {
        const message = errorMessage(error);
        optimisticPromptsRef.current.delete(resolvedKey);
        updateLocal(operationKey, (entry) => ({
          ...entry,
          state: reducer(entry.state, { type: "send_failed", error: message }),
          runtime: { ...entry.runtime, active: wasActive, running: false, cancelRequested: false, cancellable: false },
          draft: restoredSubmittedDraft(entry.draft, text),
          runtimeError: message,
        }));
      }
      return false;
    } finally {
      inFlightKeysRef.current.delete(resolveLocalKey(operationKey));
    }
  }, [bumpOperationEpoch, hasParentContext, localKey, operationEpoch, ready, reconcileOptimisticStart, resolveLocalKey, tabId, updateLocal]);

  useEffect(() => {
    if (!startRequest || handledStartRequestsRef.current.has(startRequest.id)) return;
    handledStartRequestsRef.current.add(startRequest.id);
    const latest = localKey ? tabsRef.current[localKey] : undefined;
    const blocked = !tabId
      || !localKey
      || !ready
      || !startRequest.input.trim()
      || Boolean(latest?.runtime.running || latest?.state.running || (!latest?.runtime.active && !hasParentContext));
    if (blocked) {
      if (startRequest.input.trim()) stagePrompt(startRequest.input);
      onStartRequestHandled?.(startRequest.id);
      return;
    }
    void submitPrompt(startRequest.input, false).finally(() => onStartRequestHandled?.(startRequest.id));
  }, [hasParentContext, localKey, onStartRequestHandled, ready, stagePrompt, startRequest, submitPrompt, tabId]);

  const submit = useCallback(async () => {
    await submitPrompt(draft, true);
  }, [draft, submitPrompt]);

  const markEnded = useCallback((id: string, reason: BtwEndReason, clearTranscript: boolean) => {
    updateLocal(id, (entry) => {
      if (clearTranscript) {
        return { ...freshTabLocal(), draft: entry.draft };
      }
      return {
        ...entry,
        state: reducer(entry.state, {
          type: "backend_status",
          running: false,
          pendingPrompt: false,
          backgroundJobs: 0,
          cancelRequested: false,
          cancellable: false,
        }),
        runtime: { ...emptyRuntime },
        runtimeError: "",
        endedReason: reason,
      };
    });
  }, [updateLocal]);

  const stopCurrent = useCallback(async () => {
    if (!tabId || !localKey || (!active && !running)) return;
    bumpOperationEpoch(localKey);
    closedKeysRef.current.add(localKey);
    try {
      await app.ReturnFromBtwForTab(tabId);
      markEnded(localKey, "stopped", false);
      focusInput();
    } catch (error) {
      closedKeysRef.current.delete(localKey);
      updateLocal(localKey, (entry) => ({ ...entry, runtimeError: errorMessage(error) }));
    }
  }, [active, bumpOperationEpoch, focusInput, localKey, markEnded, running, tabId, updateLocal]);

  const restart = useCallback(async () => {
    if (!tabId || !localKey || running) return;
    bumpOperationEpoch(localKey);
    closedKeysRef.current.add(localKey);
    try {
      if (active) await app.ReturnFromBtwForTab(tabId);
      markEnded(localKey, "stopped", true);
      focusInput();
    } catch (error) {
      closedKeysRef.current.delete(localKey);
      updateLocal(localKey, (entry) => ({ ...entry, runtimeError: errorMessage(error) }));
    }
  }, [active, bumpOperationEpoch, focusInput, localKey, markEnded, running, tabId, updateLocal]);

  const endAndClose = useCallback(async () => {
    const id = tabId;
    const key = localKey;
    if (!id || !key) {
      onEnd();
      return;
    }
    bumpOperationEpoch(key);
    closedKeysRef.current.add(key);
    try {
      await app.ReturnFromBtwForTab(id);
      deleteLocal(key);
      onEnd();
    } catch (error) {
      closedKeysRef.current.delete(key);
      updateLocal(key, (entry) => ({ ...entry, runtimeError: errorMessage(error) }));
    }
  }, [bumpOperationEpoch, deleteLocal, localKey, onEnd, tabId, updateLocal]);

  const onSubmit = useCallback((event: FormEvent) => {
    event.preventDefault();
    void submit();
  }, [submit]);

  const onKeyDown = useCallback((event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (
      event.key !== "Enter"
      || event.shiftKey
      || event.metaKey
      || event.ctrlKey
      || event.altKey
      || event.nativeEvent.isComposing
      || composingRef.current
      || event.keyCode === 229
    ) return;
    event.preventDefault();
    void submit();
  }, [submit]);

  const status = useMemo(() => {
    if (!ready) return t("btw.status.starting");
    if (running) return t("btw.status.running");
    if (active) return t("btw.status.active");
    return t("btw.status.ready");
  }, [active, ready, running, t]);

  const contextHint = active ? t("btw.contextActive") : t("btw.contextReady");
  const suggestions = useMemo(() => [
    t("btw.suggestionExplain"),
    t("btw.suggestionCompare"),
    t("btw.suggestionRisks"),
  ], [t]);

  return (
    <aside id="btw-panel" className="btw-panel" aria-labelledby="btw-panel-title" hidden={!visible}>
      <header className="btw-panel__head">
        <div className="btw-panel__head-main">
          <span className="btw-panel__mark" aria-hidden="true"><MessageSquare size={16} /></span>
          <div className="btw-panel__heading">
            <div className="btw-panel__title-row">
              <h2 id="btw-panel-title">{t("btw.title")}</h2>
              <span>{t("btw.subtitle")}</span>
            </div>
            <div className="btw-panel__status" role="status" aria-live="polite">
              <span className={`btw-panel__status-dot${running ? " btw-panel__status-dot--running" : active ? " btw-panel__status-dot--active" : ""}`} aria-hidden="true" />
              <span>{status}</span>
            </div>
          </div>
        </div>
        <div className="btw-panel__head-actions">
          {active && !running && (
            <Tooltip label={t("btw.refreshHint")}>
              <button className="btw-panel__icon-btn" type="button" onClick={() => void restart()} aria-label={t("btw.refreshHint")}>
                <RefreshCw size={14} />
              </button>
            </Tooltip>
          )}
          <button className="btw-panel__end-btn" type="button" onClick={() => void endAndClose()}>
            <Square size={10} fill="currentColor" />
            <span>{t("btw.end")}</span>
          </button>
          <Tooltip label={t("btw.hide")}>
            <button className="btw-panel__icon-btn" type="button" onClick={onHide} aria-label={t("btw.hide")}>
              <X size={15} />
            </button>
          </Tooltip>
        </div>
      </header>

      <div className="btw-panel__context">
        <span className="btw-panel__context-copy"><ShieldCheck size={13} aria-hidden="true" />{contextHint}</span>
        <span className="btw-panel__model" title={modelLabel}>{modelLabel}</span>
      </div>

      <div className="btw-panel__body">
        {current.state.items.length > 0 ? (
          <Transcript
            items={current.state.items}
            live={current.state.live}
            tabId={transcriptTabId}
            footerHeight={0}
            onPrompt={(text) => {
              setLocalDraft(text);
              focusInput();
            }}
            rewindDisabled
            running={running}
            questionNavigator={false}
            hasOlderHistory={false}
          />
        ) : (
          <div className="btw-panel__empty">
            <span className="btw-panel__empty-icon" aria-hidden="true"><MessageSquare size={20} /></span>
            <div className="btw-panel__empty-copy">
              <h3>{hasParentContext ? t("btw.emptyTitle") : t("btw.noContextTitle")}</h3>
              <p>{hasParentContext ? t("btw.emptyBody") : t("btw.noContextBody")}</p>
            </div>
            {hasParentContext && (
              <div className="btw-panel__suggestions" aria-label={t("btw.suggestions")}>
                {suggestions.map((suggestion) => (
                  <button key={suggestion} type="button" onClick={() => {
                    setLocalDraft(suggestion);
                    focusInput();
                  }}>
                    {suggestion}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      {current.endedReason && (
        <div className="btw-panel__notice" role="status">
          <RefreshCw size={13} aria-hidden="true" />
          <span>{current.endedReason === "idle" ? t("btw.endedIdle") : t("btw.endedStopped")}</span>
        </div>
      )}
      {current.runtimeError && (
        <div className="btw-panel__alert" role="alert">
          <AlertCircle size={13} aria-hidden="true" />
          <span>{t("btw.error", { message: current.runtimeError })}</span>
        </div>
      )}

      <form className="btw-panel__composer" onSubmit={onSubmit}>
        {running && (
          <div className="btw-panel__run-strip" role="status">
            <span className="btw-panel__run-dot" aria-hidden="true" />
            <span>{t("btw.status.running")}</span>
            <em>{t("btw.runningDraftHint")}</em>
          </div>
        )}
        <div className="btw-panel__input-shell">
          <textarea
            ref={inputRef}
            value={draft}
            onChange={(event) => setLocalDraft(event.target.value)}
            onKeyDown={onKeyDown}
            onCompositionStart={() => { composingRef.current = true; }}
            onCompositionEnd={() => { composingRef.current = false; }}
            placeholder={running ? t("btw.placeholderBusy") : t("btw.placeholder")}
            aria-label={t("btw.placeholder")}
            disabled={inputUnavailable}
            rows={1}
          />
          <Tooltip label={running ? t("btw.status.running") : t("composer.send")}>
            <button
              className="btw-panel__send-btn"
              type="submit"
              disabled={inputUnavailable || running || !canSubmit || draft.trim() === ""}
              aria-label={t("composer.send")}
            >
              <ArrowUp size={15} />
            </button>
          </Tooltip>
        </div>
        <div className="btw-panel__composer-foot">
          <span>{t("btw.keyboardHint")}</span>
          {running && (
            <button className="btw-panel__stop-btn" type="button" onClick={() => void stopCurrent()}>
              <Square size={9} fill="currentColor" />
              <span>{t("btw.stop")}</span>
            </button>
          )}
        </div>
      </form>
    </aside>
  );
}
