import { useEffect, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { SessionRefSettings } from "../port/boundary";
import { Group } from "./Group";

// A referenced past conversation is quoted into the message as read-only data,
// up to this many tokens; past it only the newest messages stay, and the quote
// says how many. The limit is the user's, not a constant of the app.
export function SessionRef({ port }: { port: AgentPort }) {
  const [state, setState] = useState<SessionRefSettings | null>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const adopt = (s: SessionRefSettings) => {
    setState(s);
    setDraft(String(s.tokens));
  };

  useEffect(() => {
    port.sessionRefSettings().then(adopt).catch(() => setState(null));
  }, [port]);

  const save = async (tokens: number) => {
    setBusy(true);
    setError("");
    try {
      adopt(await port.saveSessionRefSettings(tokens));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const commit = () => {
    if (!state) return;
    const n = Number(draft);
    if (!Number.isInteger(n) || n < state.minTokens || n > state.maxTokens) {
      return setError(t("common.enter_a_whole_number_from", { min: state.minTokens, max: state.maxTokens }));
    }
    if (n !== state.tokens) void save(n);
  };

  const body = !state ? (
    <div className="empty">{t("composer.the_reference_settings_could_not")}</div>
  ) : (
    <div className="box">
      <div className="lrow threshold-row">
        <span className="tx">
          <span className="lb">{t("composer.largest_quote")}</span>
          <span className="ds">{t("composer.a_referenced_conversation_goes_into", { n: state.defaultTokens })}</span>
        </span>
        <div className="threshold-control">
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={t("composer.largest_quote")} data-action-keydown="session-ref.tokens" data-action-change="session-ref.tokens"
              value={draft} disabled={busy}
              onChange={(e) => setDraft(e.target.value.replace(/\D/g, ""))}
              onBlur={commit}
              onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => { if (e.key === "Enter") e.currentTarget.blur(); }} />
            <i>token</i>
          </span>
          <button type="button" className="btn" data-action="session-ref.reset" disabled={busy || state.tokens === state.defaultTokens} onClick={() => void save(0)}>{t("composer.restore_default")}</button>
        </div>
      </div>
      {error && <div className="why">{error}</div>}
    </div>
  );

  return <Group id="session-ref" title={t("composer.size_of_a_referenced_conversation")} hint={t("composer.how_much_a_referenced_past")}>{body}</Group>;
}
