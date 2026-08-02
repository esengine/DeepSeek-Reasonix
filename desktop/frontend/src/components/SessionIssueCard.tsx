import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, KeyRound, Loader2 } from "lucide-react";
import type { AppBindings } from "../lib/bridge";
import type { Translator } from "../lib/i18n";
import type { SessionRuntimeIssue } from "../lib/types";

const translateDynamic = (t: Translator, key: string): string =>
  t(key as Parameters<Translator>[0]);

interface Props {
  issue: SessionRuntimeIssue;
  tabID: string;
  t: Translator;
  api: Pick<AppBindings, "ResolveSessionRuntimeIssue">;
}

const actionKey: Record<string, string> = {
  focus: "session.actionFocus",
  retry: "session.actionRetry",
  read_only: "session.actionReadOnly",
  copy: "session.actionCopy",
};

const messageKey: Record<string, string> = {
  current_tab: "session.messageCurrentTab",
  current_detached: "session.messageCurrentDetached",
  same_instance_hidden: "session.messageSameHidden",
  external_process: "session.messageExternal",
  stale_reclaimed: "session.messageStale",
  unknown: "session.messageUnknown",
};

const automaticOwnerKinds = new Set(["current_tab", "current_detached", "same_instance_hidden", "stale_reclaimed"]);

function preferredAction(ownerKind: string | undefined, actions: string[]): string | undefined {
  if (ownerKind === "current_tab" || ownerKind === "current_detached" || ownerKind === "same_instance_hidden") {
    return actions.includes("focus") ? "focus" : actions[0];
  }
  if (ownerKind === "stale_reclaimed") return actions.includes("retry") ? "retry" : actions[0];
  if (actions.includes("copy")) return "copy";
  if (actions.includes("read_only")) return "read_only";
  return actions[0];
}

/**
 * Resolves safe same-app/stale ownership automatically. Only a verified
 * external or unknown owner asks the user, with one recommended action and
 * lower-frequency alternatives behind progressive disclosure.
 */
export function SessionIssueCard({ issue, tabID, t, api }: Props) {
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [failed, setFailed] = useState(false);
  const autoStarted = useRef(false);
  const actions = issue.actions ?? [];
  const primary = useMemo(() => preferredAction(issue.ownerKind, actions), [actions, issue.ownerKind]);
  const automatic = Boolean(primary && automaticOwnerKinds.has(issue.ownerKind ?? ""));

  const run = useCallback(async (action: string) => {
    setBusy(true);
    setFailed(false);
    try {
      await api.ResolveSessionRuntimeIssue(tabID, issue.issueId ?? "", action);
      setDone(true);
    } catch {
      setBusy(false);
      setFailed(true);
    }
  }, [api, issue.issueId, tabID]);

  useEffect(() => {
    if (automatic && primary && !autoStarted.current) {
      autoStarted.current = true;
      void run(primary);
    }
  }, [automatic, primary, run]);

  if (done || actions.length === 0) return null;
  const alternatives = actions.filter((action) => action !== primary);
  const message = translateDynamic(t, messageKey[issue.ownerKind ?? "unknown"] ?? "session.messageUnknown");

  return (
    <div className="banner banner--warning banner--actionable session-issue-card" role="status" aria-live="polite">
      {busy ? <Loader2 className="spin" size={14} aria-hidden /> : <KeyRound size={14} aria-hidden />}
      <span className="banner__msg">
        {automatic && busy ? translateDynamic(t, "session.resolvingAutomatically") : message}
        {failed && <span className="banner__sub">{translateDynamic(t, "session.actionFailed")}</span>}
      </span>
      <span className="banner__spacer" />
      {!automatic && primary && (
        <button type="button" className="btn btn--small btn--primary" disabled={busy} onClick={() => void run(primary)}>
          {translateDynamic(t, actionKey[primary] ?? "session.actionRetry")}
        </button>
      )}
      {automatic && failed && primary && (
        <button type="button" className="btn btn--small" disabled={busy} onClick={() => void run(primary)}>
          {translateDynamic(t, "session.actionRetry")}
        </button>
      )}
      {!automatic && alternatives.length > 0 && (
        <details className="banner__more">
          <summary>
            {translateDynamic(t, "session.moreActions")}
            <ChevronDown size={12} aria-hidden />
          </summary>
          <div className="banner__more-actions">
            {alternatives.map((action) => (
              <button key={action} type="button" className="btn btn--small" disabled={busy} onClick={() => void run(action)}>
                {translateDynamic(t, actionKey[action] ?? "session.actionRetry")}
              </button>
            ))}
            {issue.message && <small>{issue.message}</small>}
          </div>
        </details>
      )}
    </div>
  );
}
