import type { ReactNode } from "react";
import { t } from "../i18n";
import type { ProviderEntry } from "../port/port";

// How a turn's context reaches the next one. Auto is vendor detection, which is
// the only honest answer for an endpoint nobody has characterised; the other two
// are what a reader picks when the endpoint has already contradicted it.
const CONTINUATIONS: ReadonlyArray<readonly [string, string]> = [
  ["", "common.auto"],
  ["stateful", "endpointSwitches.byReference"],
  ["stateless", "endpointSwitches.replayEachTurn"],
];

const CONTINUATION_WHY: Record<string, string> = {
  "": "endpointSwitches.decidedByTheEndpointS",
  stateful: "endpointSwitches.sendsOnlyTheNewTurn",
  stateless: "endpointSwitches.replaysTheWholeHistoryEvery",
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
          <span className="lb">{t("common.webSearch")}</span>
          <div className="seg" role="group" aria-label={t("endpointSwitches.webSearchFor", { name: label })}>
            {[true, false].map((on) => (
              <button key={String(on)} type="button" data-action="provider.draft" data-value={`web-search:${on}`}
                aria-pressed={search === on} disabled={disabled} onClick={() => onSearch(on)}>
                {t(on ? "common.on" : "common.off")}
              </button>
            ))}
          </div>
          <span className="why">{t("endpointSwitches.theEndpointRunsTheSearch")}</span>
        </div>
      )}
      {entry.canSetThinking && (
        <div className="vway">
          <span className="lb">{t("common.thinkingParameter")}</span>
          <div className="seg" role="group" aria-label={t("endpointSwitches.thinkingParameterFor", { name: label })}>
            {[true, false].map((on) => (
              <button key={String(on)} type="button" data-action="provider.draft" data-value={`thinking:${on}`}
                aria-pressed={sendsThinking === on} disabled={disabled} onClick={() => onThinking(on)}>
                {t(on ? "common.auto" : "endpointSwitches.doNotSend")}
              </button>
            ))}
          </div>
          <span className="why">
            {t(sendsThinking ? "endpointSwitches.someRelaysDoNotAccept" : "endpointSwitches.sendsOnlyTheOrdinaryChat")}
          </span>
          {sendsThinking && shape}
        </div>
      )}
      {entry.canSetContinuation && (
        <div className="vway">
          <span className="lb">{t("endpointSwitches.carryingContext")}</span>
          <div className="seg" role="group" aria-label={t("endpointSwitches.howCarriesContextBetweenTurns", { name: label })}>
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
