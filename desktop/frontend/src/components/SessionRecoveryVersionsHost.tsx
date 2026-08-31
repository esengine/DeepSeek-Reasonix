import { lazy, Suspense, useCallback, useEffect, useRef, useState } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { ProjectTopicKey } from "../lib/sessionCatalogTypes";
import { bindSessionVersionInspector } from "../lib/sessionRecoveryVersionHostBridge";
import { normalizeRecoveryLineageView, userVisibleRecoveryVersions } from "../lib/sessionRecoveryVersions";
import { useToast } from "../lib/toast";
import type { RecoveryLineageMember, RecoveryLineageView, SessionMeta } from "../lib/types";

const RecoveryLineageDialog = lazy(() => import("./RecoveryLineageDialog").then((module) => ({ default: module.RecoveryLineageDialog })));

type SessionVersionsState = { topic: ProjectTopicKey; view: RecoveryLineageView };

interface SessionRecoveryVersionsHostProps {
  sessions?: SessionMeta[];
  onResumeSession: (session: SessionMeta) => Promise<void>;
  onRecoveryCreated: () => void;
  onLineageChanged: () => void;
}

export function SessionRecoveryVersionsHost({ sessions, onResumeSession, onRecoveryCreated, onLineageChanged }: SessionRecoveryVersionsHostProps) {
  const t = useT();
  const { showToast } = useToast();
  const [state, setState] = useState<SessionVersionsState | null>(null);

  const showVersions = useCallback(async (topic: ProjectTopicKey, initial?: RecoveryLineageView) => {
    try {
      const view = normalizeRecoveryLineageView(initial ?? await app.GetRecoveryLineage(topic));
      if (userVisibleRecoveryVersions(view).length > 1) setState({ topic, view });
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error");
    }
  }, [showToast]);

  const runtimeCallbacks = useRef({ onRecoveryCreated, showToast, showVersions, t });
  useEffect(() => {
    runtimeCallbacks.current = { onRecoveryCreated, showToast, showVersions, t };
  }, [onRecoveryCreated, showToast, showVersions, t]);

  useEffect(() => {
    let stop: (() => void) | undefined;
    let cancelled = false;
    void import("../lib/sessionRecoveryRuntime").then(({ startSessionRecoveryRuntime }) => {
      if (cancelled) return;
      stop = startSessionRecoveryRuntime({
        onRecovered: () => runtimeCallbacks.current.onRecoveryCreated(),
        onDiverged: (topic, view) => {
          const current = runtimeCallbacks.current;
          current.showToast(current.t("recovery.divergedToast"), "warn", {
            actionLabel: current.t("recovery.inspectLineage"),
            onAction: () => { void current.showVersions(topic, view); },
          });
        },
      });
    });
    return () => {
      cancelled = true;
      stop?.();
    };
  }, []);

  const inspectVersions = useCallback((session: SessionMeta, view: RecoveryLineageView) => {
    if (!session.topicId) return;
    void showVersions({
      scope: session.scope || (session.workspaceRoot ? "project" : "global"),
      workspaceRoot: session.workspaceRoot || undefined,
      topicId: session.topicId,
      path: session.path,
    }, view);
  }, [showVersions]);
  useEffect(() => bindSessionVersionInspector(inspectVersions), [inspectVersions]);

  const openVersion = useCallback(async (member: RecoveryLineageMember) => {
    const topic = state?.topic;
    if (!topic) return;
    const known = sessions?.find((session) => session.path === member.path);
    await onResumeSession(known ?? sessionFromVersion(topic, member));
  }, [onResumeSession, sessions, state?.topic]);

  if (!state) return null;
  return (
    <Suspense fallback={null}>
      <RecoveryLineageDialog
        topic={state.topic}
        initial={state.view}
        onClose={() => setState(null)}
        onChanged={(view) => {
          setState((current) => current ? { ...current, view } : current);
          onLineageChanged();
        }}
        onOpenVersion={openVersion}
      />
    </Suspense>
  );
}

function sessionFromVersion(topic: ProjectTopicKey, member: RecoveryLineageMember): SessionMeta {
  const activityAt = member.lastActivityAt || member.createdAt || 0;
  return {
    path: member.path, preview: member.preview || "", title: member.versionNote,
    turns: member.turns, turnsState: "valid", createdAt: member.createdAt || 0,
    lastActivityAt: activityAt, modTime: activityAt, current: false, open: member.open,
    scope: topic.scope, workspaceRoot: topic.workspaceRoot, topicId: topic.topicId,
    recovered: true, recoveryRole: member.role, recoveryCanonical: member.canonical,
  };
}
