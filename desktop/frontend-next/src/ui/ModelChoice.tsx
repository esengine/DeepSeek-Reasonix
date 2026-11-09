import { StudioIcon } from "./StudioIcon";
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
          placeholder={t("composer.search_or_enter_an_exact")}
          aria-label={t("composer.search_or_add_a_model")}
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
            {t(fetching ? "common.reading_from_provider" : "composer.not_listed_read_available_models")}
          </button>
        )}
      </div>
      {fetchFail && (
        <div className="find" data-lvl="err" role="alert">
          <span className="t">{t("composer.could_not_read_the_model")}</span>
          <span className="why">{fetchFail}</span>
          <span className="why">{t("composer.you_can_also_type_the")}</span>
        </div>
      )}

      <div className="mrows">
        {shown.map((m) => (
          <div className="mline" key={m} data-off={on.has(m) ? undefined : ""}
            aria-busy={facts?.[m]?.checking || undefined}>
            <button className="tick" role="checkbox" aria-checked={on.has(m)}
              aria-label={t("composer.enable_name", { name: m })} onClick={() => onToggle(m)}>
              <i />
            </button>
            <span className="mid">
              <span className="nm" title={labels?.[m] ? m : undefined}>{labels?.[m] ?? m}</span>
              {onAlias && <input className="malias" value={aliasFor(m)} spellCheck={false}
                data-action="provider.draft" data-value="model-alias" data-target={m}
                aria-label={t("composer.display_alias_for_name", { name: m })}
                placeholder={t("composer.display_alias_empty_uses_model")}
                title={t("composer.display_only_the_model_id")}
                disabled={!on.has(m)} onChange={(e) => onAlias(m, e.target.value)} />}
              {facts?.[m] && <ModelEvidence fact={facts[m]} />}
            </span>
            <span className="mops">
              {onCheck && (
                <button className="mverify" data-action="provider.model-check" data-target={m}
                  disabled={!on.has(m) || checkDisabled || facts?.[m]?.checking}
                  aria-label={t("composer.verify_model_name", { name: m })} onClick={() => onCheck(m)}>
                  {t(facts?.[m]?.checking ? "common.verifying" : facts?.[m]?.status ? "composer.verify_again" : "composer.verify")}
                </button>
              )}
              {onVision ? (
                <button className="vtag" aria-pressed={vision.includes(m)}
                  aria-label={t("composer.image_input_for_name", { name: m })}
                  disabled={!on.has(m) || visionLocked?.(m)}
                  title={visionLocked?.(m) ? t("composer.this_connection_will_not_send") : t(vision.includes(m) ? "composer.image_input_enabled" : "composer.image_capability_is_unverified_click")}
                  onClick={() => onVision(m)}>
                  {t(vision.includes(m) ? "common.reads_images" : visionLocked?.(m) ? "composer.no_images" : "composer.images")}
                </button>
              ) : (
                vision.includes(m) && <span className="vtag" data-flat>{t("common.reads_images")}</span>
              )}
              {onDefault && (
                <button className="dtag" aria-pressed={def === m} disabled={!on.has(m)}
                  aria-label={t("composer.make_name_the_default_model", { name: m })}
                  onClick={() => onDefault(m)}>
                  {t("common.default")}
                </button>
              )}
            </span>
          </div>
        ))}
      </div>

      {hidden > 0 && <span className="mmore">{t("composer.n_more_not_listed_search", { n: hidden })}</span>}
      {shown.length === 0 && !naming && <div className="empty">{t("composer.no_models_match")}</div>}

      {/* Under the list, never over it: a "create" offer sitting above seven
          real matches is how you add a model when you meant to pick one. */}
      {naming && (
        <button className="mnew" onClick={add}>
          <span className="k"><StudioIcon name="plus" /></span>
          <span className="tx">
            <span className="lb">{t("composer.add_unlisted_model_name", { name: q.trim() })}</span>
            <span className="ds">{t("composer.saves_the_exact_model_id")}</span>
          </span>
        </button>
      )}
    </>
  );
}

function ModelEvidence({ fact }: { fact: ModelFact }) {
  const origin = {
    configured: "composer.configured",
    endpoint: "composer.endpoint_catalog",
    manual: "composer.added_manually",
    missing: "composer.not_returned_this_time",
  }[fact.origin];
  const state = fact.checking
    ? "common.verifying"
    : fact.status === "available"
      ? "composer.verified_available"
      : fact.status === "unavailable"
        ? unavailableReason(fact.reason)
        : fact.status === "unknown"
          ? checkReason(fact.reason)
          : "composer.not_verified";
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
    case "not_found": return "composer.the_model_does_not_exist";
    case "rejected": return "composer.the_endpoint_does_not_accept";
    case "tools": return "composer.answers_chat_but_refuses_tool";
    default: return "composer.currently_unavailable";
  }
}

function checkReason(reason?: ProviderModelCheckReason): string {
  switch (reason) {
    case "auth": return "composer.credentials_were_rejected_availability_is";
    case "rate_limited": return "composer.rate_limited_availability_is_still";
    case "network": return "composer.network_error_availability_is_still";
    case "timeout": return "composer.verification_timed_out_availability_is";
    case "local_timeout": return "composer.the_local_service_did_not";
    case "redirect_refused": return "composer.the_address_redirected_to_another";
    case "rejected": return "composer.request_rejected_availability_is_still";
    default: return "composer.availability_is_still_unknown";
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
    count.available && t("composer.n_available", { n: count.available }),
    count.unavailable && t("composer.n_unavailable", { n: count.unavailable }),
    count.unknown && t("composer.n_undetermined", { n: count.unknown }),
    count.checking && t("composer.n_verifying", { n: count.checking }),
    count.todo && t("composer.n_not_verified", { n: count.todo }),
  ].filter(Boolean).join(" · ");
}
