import type { ReactNode } from "react";
import { t } from "../i18n";
import type { ProviderEntry } from "../port/port";

// How a turn's context reaches the next one. Auto is vendor detection, which is
// the only honest answer for an endpoint nobody has characterised; the other two
// are what a reader picks when the endpoint has already contradicted it.
const CONTINUATIONS: ReadonlyArray<readonly [string, string]> = [
  ["", "common.auto"],
  ["stateful", "models.by_reference"],
  ["stateless", "models.replay_each_turn"],
];

const CONTINUATION_WHY: Record<string, string> = {
  "": "models.decided_by_the_endpoints_vendor",
  stateful: "models.sends_only_the_new_turn",
  stateless: "models.replays_the_whole_history_every",
};

export function hasEndpointSwitches(entry: ProviderEntry): boolean {
  return !!(entry.canWebSearch || entry.canSetThinking || entry.canSetContinuation);
}

// The declarations that live on the stored wire, held in the same draft as the
// rest of the form: nothing here is written until Save.
export function EndpointSwitches({
  entry, label, shape, search, sendsThinking, continuation, onSearch, onThinking, onContinuation, disabled,
}: {
  entry: ProviderEntry; label: string; shape: ReactNode; search: boolean; sendsThinking: boolean; continuation: string;
  onSearch: (on: boolean) => void; onThinking: (send: boolean) => void; onContinuation: (mode: string) => void;
  disabled: boolean;
}) {
  return (
    <div className="pdetail-options">
      {entry.canWebSearch && (
        <div className="vway">
          <span className="lb">{t("models.web_search")}</span>
          <div className="seg" role="group" aria-label={t("models.web_search_for_name", { name: label })}>
            {[true, false].map((on) => (
              <button key={String(on)} type="button" data-action="provider.draft" data-value={`web-search:${on}`}
                aria-pressed={search === on} disabled={disabled} onClick={() => onSearch(on)}>
                {t(on ? "models.on" : "common.off")}
              </button>
            ))}
          </div>
          <span className="why">{t("models.the_endpoint_runs_the_search")}</span>
        </div>
      )}
      {entry.canSetThinking && (
        <div className="vway">
          <span className="lb">{t("models.thinking_parameter")}</span>
          <div className="seg" role="group" aria-label={t("models.thinking_parameter_for_name", { name: label })}>
            {[true, false].map((on) => (
              <button key={String(on)} type="button" data-action="provider.draft" data-value={`thinking:${on}`}
                aria-pressed={sendsThinking === on} disabled={disabled} onClick={() => onThinking(on)}>
                {t(on ? "common.auto" : "models.do_not_send")}
              </button>
            ))}
          </div>
          <span className="why">
            {t(sendsThinking ? "models.some_relays_do_not_accept" : "models.sends_only_the_ordinary_chat")}
          </span>
          {sendsThinking && shape}
        </div>
      )}
      {entry.canSetContinuation && (
        <div className="vway">
          <span className="lb">{t("models.carrying_context")}</span>
          <div className="seg" role="group" aria-label={t("models.how_name_carries_context_between", { name: label })}>
            {CONTINUATIONS.map(([mode, text]) => (
              <button key={mode} type="button" data-action="provider.draft" data-value={`continuation:${mode || "auto"}`}
                aria-pressed={continuation === mode} disabled={disabled} onClick={() => onContinuation(mode)}>
                {t(text)}
              </button>
            ))}
          </div>
          <span className="why">{t(CONTINUATION_WHY[continuation] ?? CONTINUATION_WHY[""])}</span>
        </div>
      )}
    </div>
  );
}
