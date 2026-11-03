import { useMemo, useState } from "react";
import { t } from "../i18n";
import type { ProviderModelCheck, ProviderModelCheckReason, ProviderModelCheckStatus } from "../port/port";

// Which models a connection offers. A gateway answers with hundreds of names
// that differ by a date suffix, so the list is a search field first: one field
// that filters, and — when nothing on the list carries the name — adds it. Two
// boxes would be two controls for one intent, and the user would have to know
// which one their model is behind.

// 一行一个 DOM 节点的列表要有上限（perf/README 的判据）。上限只砍没勾的尾巴：
// 勾上的永远列出来，否则"我选了什么"会被截断，而那是这个面板要回答的问题。
const CAP = 60;

export type ModelOrigin = "configured" | "endpoint" | "manual" | "missing";

export interface ModelFact {
  origin: ModelOrigin;
  status?: ProviderModelCheckStatus;
  reason?: ProviderModelCheckReason;
  httpStatus?: number;
  detail?: string;
  checking?: boolean;
}

export function checkedFact(fact: ModelFact | undefined, origin: ModelOrigin, got: ProviderModelCheck): ModelFact {
  return { ...(fact ?? { origin }), status: got.status, reason: got.reason, httpStatus: got.httpStatus, detail: got.detail };
}

export function clearModelCheckFacts(facts: Record<string, ModelFact>): Record<string, ModelFact> {
  return Object.fromEntries(Object.entries(facts).map(([model, fact]) => [
    model,
    { origin: fact.origin },
  ]));
}

interface Props {
  // Everything on offer: what the endpoint reported, plus anything added here.
  models: string[];
  aliases?: Record<string, string>;
  onAlias?: (m: string, value: string) => void;
  picked: string[];
  vision: string[];
  // Absent where the flow has no default to set — adding a source takes the
  // first pick, so there is nothing to choose yet.
  onDefault?: (m: string) => void;
  def?: string;
  // Absent where the answer is the endpoint's and not the user's to change.
  onVision?: (m: string) => void;
  // Per row: the kernel refuses image input for this model, so the switch would
  // be a dead control. One endpoint can serve both kinds, so it is not a
  // property of the connection.
  visionLocked?: (m: string) => boolean;
  // How a model reads with its shared prefix left out; the id stays in the tooltip.
  labels?: Record<string, string>;
  facts?: Record<string, ModelFact>;
  onCheck?: (m: string) => void;
  checkDisabled?: boolean;
  onToggle: (m: string) => void;
  onAdd: (m: string) => void;
  // The same read the section header offers, placed where a missing model is noticed.
  onFetch?: () => void;
  fetching?: boolean;
  fetchDisabled?: boolean;
  fetchFail?: string;
}

export function ModelChoice({
  models, aliases, onAlias, picked, vision, def, onDefault, onVision, visionLocked, labels, facts, onCheck, checkDisabled, onToggle, onAdd,
  onFetch, fetching, fetchDisabled, fetchFail,
}: Props) {
  const [q, setQ] = useState("");
  const query = q.trim().toLowerCase();
  const aliasFor = (m: string) => aliases && Object.hasOwn(aliases, m) ? aliases[m] : "";
  const on = useMemo(() => new Set(picked), [picked]);

  // Enabled first, and deliberately keyed on the list alone: re-sorting on every
  // tick throws the row out from under the pointer that just hit it. Menu.tsx
  // keeps group order over relevance for the same reason.
  const order = useMemo(
    () => [...models].sort((a, b) => Number(picked.includes(b)) - Number(picked.includes(a))),
    [models],
  );

  const hits = query ? order.filter((m) => `${m} ${aliasFor(m)}`.toLowerCase().includes(query)) : order;
  const [shown, hidden] = cap(hits, on);
  // Display aliases aid search but cannot reserve another model's wire ID.
  const naming = q.trim() !== "" && !models.some((m) => m.toLowerCase() === query);

  const add = () => {
    onAdd(q.trim());
    setQ("");
  };

  const tally = checkTally(picked, facts);
  return (
    <>
      {facts && <p className="msummary" role="status">{tally}</p>}
      <div className="msearch">
        <input
          type="search"
          value={q}
          spellCheck={false}
          placeholder={t("modelChoice.searchOrEnterAnExact")}
          aria-label={t("modelChoice.searchOrAddAModel")}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key !== "Enter" || !naming) return;
            e.preventDefault();
            add();
          }}
        />
        {query && <span className="cnt">{hits.length} / {models.length}</span>}
        {onFetch && (
          <button className="mfetch" data-action="provider.probe" onClick={onFetch} disabled={fetchDisabled}>
            {t(fetching ? "common.readingFromProvider" : "modelChoice.notListedReadAvailableModels")}
          </button>
        )}
      </div>
      {fetchFail && (
        <div className="find" data-lvl="err" role="alert">
          <span className="t">{t("modelChoice.couldNotReadTheModel")}</span>
          <span className="why">{fetchFail}</span>
          <span className="why">{t("modelChoice.youCanAlsoTypeThe")}</span>
        </div>
      )}

      <div className="mrows">
        {shown.map((m) => (
          <div className="mline" key={m} data-off={on.has(m) ? undefined : ""}
            aria-busy={facts?.[m]?.checking || undefined}>
            <button className="tick" role="checkbox" aria-checked={on.has(m)}
              aria-label={t("modelChoice.enable", { name: m })} onClick={() => onToggle(m)}>
              <i />
            </button>
            <span className="mid">
              <span className="nm" title={labels?.[m] ? m : undefined}>{labels?.[m] ?? m}</span>
              {onAlias && <input className="malias" value={aliasFor(m)} spellCheck={false}
                data-action="provider.draft" data-value="model-alias" data-target={m}
                aria-label={t("modelChoice.displayAliasFor", { name: m })}
                placeholder={t("modelChoice.displayAliasEmptyUsesModel")}
                title={t("modelChoice.displayOnlyTheModelID")}
                disabled={!on.has(m)} onChange={(e) => onAlias(m, e.target.value)} />}
              {facts?.[m] && <ModelEvidence fact={facts[m]} />}
            </span>
            <span className="mops">
              {onCheck && (
                <button className="mverify" data-action="provider.model-check" data-target={m}
                  disabled={!on.has(m) || checkDisabled || facts?.[m]?.checking}
                  aria-label={t("modelChoice.verifyModel", { name: m })} onClick={() => onCheck(m)}>
                  {t(facts?.[m]?.checking ? "common.verifying" : facts?.[m]?.status ? "modelChoice.verifyAgain" : "common.verify")}
                </button>
              )}
              {onVision ? (
                <button className="vtag" aria-pressed={vision.includes(m)}
                  aria-label={t("modelChoice.imageInputFor", { name: m })}
                  disabled={!on.has(m) || visionLocked?.(m)}
                  title={visionLocked?.(m) ? t("modelChoice.thisConnectionWillNotSend") : t(vision.includes(m) ? "modelChoice.imageInputEnabled" : "modelChoice.imageCapabilityIsUnverifiedClick")}
                  onClick={() => onVision(m)}>
                  {t(vision.includes(m) ? "common.readsImages" : visionLocked?.(m) ? "modelChoice.noImages" : "modelChoice.images")}
                </button>
              ) : (
                vision.includes(m) && <span className="vtag" data-flat>{t("common.readsImages")}</span>
              )}
              {onDefault && (
                <button className="dtag" aria-pressed={def === m} disabled={!on.has(m)}
                  aria-label={t("modelChoice.makeTheDefaultModel", { name: m })}
                  onClick={() => onDefault(m)}>
                  {t("common.default")}
                </button>
              )}
            </span>
          </div>
        ))}
      </div>

      {hidden > 0 && <span className="mmore">{t("modelChoice.moreNotListedSearchTo", { n: hidden })}</span>}
      {shown.length === 0 && !naming && <div className="empty">{t("modelChoice.noModelsMatch")}</div>}

      {/* Under the list, never over it: a "create" offer sitting above seven
          real matches is how you add a model when you meant to pick one. */}
      {naming && (
        <button className="mnew" onClick={add}>
          <span className="k">+</span>
          <span className="tx">
            <span className="lb">{t("modelChoice.addUnlistedModel", { name: q.trim() })}</span>
            <span className="ds">{t("modelChoice.savesTheExactModelID")}</span>
          </span>
        </button>
      )}
    </>
  );
}

function ModelEvidence({ fact }: { fact: ModelFact }) {
  const origin = {
    configured: "modelChoice.configured",
    endpoint: "modelChoice.endpointCatalog",
    manual: "modelChoice.addedManually",
    missing: "modelChoice.notReturnedThisTime",
  }[fact.origin];
  const state = fact.checking
    ? "common.verifying"
    : fact.status === "available"
      ? "modelChoice.verifiedAvailable"
      : fact.status === "unavailable"
        ? unavailableReason(fact.reason)
        : fact.status === "unknown"
          ? checkReason(fact.reason)
          : "modelChoice.notVerified";
  return (
    <span className="mevidence" role={fact.status === "unavailable" && !fact.checking ? "alert" : undefined}>
      <span>{t(origin)}</span>
      {state && <><i aria-hidden="true" data-state={fact.checking ? "checking" : fact.status ?? "unverified"} />{t(state)}</>}
      {!fact.checking && fact.httpStatus ? <code className="mhttp">{`HTTP ${fact.httpStatus}`}</code> : null}
      {!fact.checking && fact.detail ? <span className="mdetail">{fact.detail}</span> : null}
    </span>
  );
}

function unavailableReason(reason?: ProviderModelCheckReason): string {
  switch (reason) {
    case "not_found": return "modelChoice.theModelDoesNotExist";
    case "rejected": return "modelChoice.theEndpointDoesNotAccept";
    case "tools": return "modelChoice.answersChatButRefusesTool";
    default: return "modelChoice.currentlyUnavailable";
  }
}

function checkReason(reason?: ProviderModelCheckReason): string {
  switch (reason) {
    case "auth": return "modelChoice.credentialsWereRejectedAvailabilityI";
    case "rate_limited": return "modelChoice.rateLimitedAvailabilityIsStill";
    case "network": return "modelChoice.networkErrorAvailabilityIsStill";
    case "timeout": return "modelChoice.verificationTimedOutAvailabilityIs";
    case "local_timeout": return "modelChoice.theLocalServiceDidNot";
    case "redirect_refused": return "modelChoice.theAddressRedirectedToAnother";
    case "rejected": return "modelChoice.requestRejectedAvailabilityIsStill";
    default: return "modelChoice.availabilityIsStillUnknown";
  }
}

function cap(hits: string[], on: Set<string>): [string[], number] {
  if (hits.length <= CAP) return [hits, 0];
  let room = Math.max(CAP - hits.filter((m) => on.has(m)).length, 0);
  const out = hits.filter((m) => on.has(m) || room-- > 0);
  return [out, hits.length - out.length];
}

function checkTally(picked: string[], facts?: Record<string, ModelFact>): string {
  if (!facts || !picked.some((m) => facts[m]?.status || facts[m]?.checking)) return "";
  const count = { available: 0, unavailable: 0, unknown: 0, checking: 0, todo: 0 };
  for (const m of picked) count[facts[m]?.checking ? "checking" : facts[m]?.status ?? "todo"]++;
  return [
    count.available && t("modelChoice.available", { n: count.available }),
    count.unavailable && t("modelChoice.unavailable", { n: count.unavailable }),
    count.unknown && t("modelChoice.undetermined", { n: count.unknown }),
    count.checking && t("modelChoice.verifying", { n: count.checking }),
    count.todo && t("modelChoice.notVerified2", { n: count.todo }),
  ].filter(Boolean).join(" · ");
}
