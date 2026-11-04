import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { Protocol, ProviderCheck, ProviderEntry } from "../port/port";
import { checkedFact, clearModelCheckFacts, ModelChoice, type ModelFact } from "./ModelChoice";
import type { Port } from "./Providers";
import { savedNotApplied, reason, say } from "../i18n/kernel";
import { checkFailure } from "./provider_check";
import { IDLE_TIMEOUT_MAX, IDLE_TIMEOUT_MIN, THINKING, headerLines, parseEffortLevels, parseExtraBody, parseHeaders, parseIdleTimeout } from "./provider_compat";
import { ModelEfforts } from "./ModelEfforts";
import { EffortShape } from "./EffortShape";
import type { ModelEffort, ModelLimit, ProviderEdit } from "../port/port";
import { KIND_LABEL } from "./vendors";
import { ModelLimits, limitTextOf, limitsToSend, type LimitText } from "./ModelLimits";
import { ModelPrices, incomplete, priceTextOf, pricesToSend, sourceBad, type PriceText } from "./ModelPrices";
import { BillingModeField, PAYG } from "./BillingModeField";
import { useModelBatch } from "./useModelBatch";
import { EndpointSwitches, hasEndpointSwitches } from "./EndpointSwitches";
import { BalanceFields } from "./BalanceFields";

// Only what this form owns is sent: the entry keeps its prices, effort
// vocabularies and everything else the panel cannot show.
// declare opens the form on the reasoning fields: the composer sends a user
// here when the endpoint reported no effort levels.
export function EditConn({
  entry, label = entry.name, wireFixed = false, initialCheck, port, busy, setBusy, onDone, onRevert, onSaved, onDirty, declare = false, justSaved = false,
}: {
  entry: ProviderEntry; label?: string; wireFixed?: boolean; initialCheck?: ProviderCheck; port: Port;
  busy: string; setBusy: (b: string) => void; onDone: () => void | Promise<void>; onRevert: () => void; onSaved?: () => void; onDirty?: (dirty: boolean) => void;
  declare?: boolean; justSaved?: boolean;
}) {
  const seededModels = [...new Set([...entry.models, ...(initialCheck?.models ?? [])])];
  const seededVision = [...new Set([...(entry.visionModels ?? []), ...(initialCheck?.vision ?? [])])];
  const [baseUrl, setBaseUrl] = useState(entry.baseUrl);
  const [kind, setKind] = useState(entry.kind);
  const [wires, setWires] = useState<Protocol[]>([]);
  useEffect(() => {
    let live = true;
    Promise.resolve().then(() => port.protocols()).then((items) => live && setWires(items)).catch(() => live && setWires([]));
    return () => { live = false; };
  }, [port]);
  const sameRole = (p: Protocol) => (p.answers ?? "chat") === (wires.find((w) => w.kind === entry.kind)?.answers ?? "chat");
  // An account with a sibling door already switches between wires above; retyping one entry would make two doors of one kind.
  const kinds = wireFixed ? [] : wires.some((w) => w.kind === entry.kind) ? wires.filter(sameRole).map((w) => w.kind) : [];
  const [completed, setCompleted] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [models, setModels] = useState<string[]>(seededModels);
  const [picked, setPicked] = useState<string[]>(entry.models);
  const [aliases, setAliases] = useState<Record<string, string>>(entry.modelAliases ?? {});
  const [hidePrefix, setHidePrefix] = useState(entry.hideModelPrefix === true);
  const [vision, setVision] = useState<string[]>(seededVision);
  const [visionSettable, setVisionSettable] = useState<string[] | undefined>(
    entry.visionSettable ? [...new Set([...entry.visionSettable, ...(initialCheck?.vision ?? [])])] : undefined,
  );
  const [facts, setFacts] = useState<Record<string, ModelFact>>(() => modelFacts(entry.models, initialCheck));
  const [diff, setDiff] = useState(() => initialCheck?.ok ? catalogDiff(entry.models, initialCheck.models ?? [], false) : null);
  const [checkingModel, setCheckingModel] = useState("");
  const [def, setDef] = useState(entry.default || entry.models[0] || "");
  const [err, setErr] = useState<{ text: string; kind: "save" | "unapplied" } | null>(null);
  const [refreshFail, setRefreshFail] = useState("");
  const [more, setMore] = useState(declare);
  const [win, setWin] = useState(entry.contextWindow ? String(entry.contextWindow) : "");
  const [maxOut, setMaxOut] = useState(entry.maxOutputTokens ? String(entry.maxOutputTokens) : "");
  const [idleText, setIdleText] = useState(entry.idleTimeoutSeconds ? String(entry.idleTimeoutSeconds) : "");
  const idle = parseIdleTimeout(idleText);
  const [limitText, setLimitText] = useState<Record<string, LimitText>>(
    () => Object.fromEntries(Object.entries(entry.modelLimits ?? {}).map(([m, l]) => [m, limitTextOf(l)])),
  );
  const [priceText, setPriceText] = useState<Record<string, PriceText>>(
    () => Object.fromEntries(Object.entries(entry.modelPrices ?? {}).map(([m, r]) => [m, priceTextOf(r)])),
  );
  const [billing, setBilling] = useState(entry.billingMode || PAYG);
  const [balUrl, setBalUrl] = useState(entry.balanceUrl ?? "");
  const [balFormat, setBalFormat] = useState(entry.balanceFormat ?? "");
  const [think, setThink] = useState(entry.reasoningProtocol ?? "");
  const [search, setSearch] = useState(entry.webSearch ?? false);
  const [carry, setCarry] = useState(entry.continuation ?? "");
  // These belong to the stored wire; a draft that retypes it carries none of them.
  const wireSwitches = kind === entry.kind;
  // Until saved, the thinking field follows the wire being picked: the stored
  // entry's field belongs to the stored wire.
  const draftWire = wires.find((w) => w.kind === kind);
  const effortField = kind === entry.kind || !draftWire
    ? entry.effortField
    : !draftWire.effortUnder?.length || think === "" || draftWire.effortUnder.includes(think) ? draftWire.effortField : undefined;
  const [levelText, setLevelText] = useState((entry.supportedEfforts ?? []).join(", "));
  const [defEffort, setDefEffort] = useState(entry.defaultEffort ?? "");
  const levels = parseEffortLevels(levelText);
  const [ownEfforts, setOwnEfforts] = useState<Record<string, ModelEffort>>(entry.modelEfforts ?? {});
  // Kimi K3 carries a fixed vocabulary and "none" sends no reasoning field, so
  // a declared list is kept on file but has nothing to act on under either.
  const levelsDormant = think === "kimi-k3" || think === "none";
  const reasoning = useRef<HTMLSelectElement>(null);
  useEffect(() => {
    if (!declare) return;
    reasoning.current?.scrollIntoView({ block: "center" });
    reasoning.current?.focus({ preventScroll: true });
  }, [declare]);
  const [heads, setHeads] = useState(headerLines(entry.headers));
  const [extra, setExtra] = useState(entry.extraBody ? JSON.stringify(entry.extraBody, null, 2) : "");
  const saving = busy === `edit:${entry.name}`;
  const refreshing = busy === `refresh:${entry.name}`;
  const listable = entry.canListModels !== false;
  const extraBad = extra.trim() !== "" && parseExtraBody(extra) === null;

  const toggle = (list: string[], set: (v: string[]) => void, m: string) =>
    set(list.includes(m) ? list.filter((x) => x !== m) : [...list, m]);

  // Which rows may take images. The kernel answers per model now — one endpoint
  // serves an image-taking model beside text-only ones — so an older kernel that
  // only sends the connection-wide boolean still gets the old answer.
  const visionLocked = (m: string) =>
    visionSettable ? !visionSettable.includes(m) : entry.canSetVision === false;

  // A name the endpoint never reported. It lands ticked because typing it out
  // is already the answer to "do you want this one", and at the head of the
  // list because a new row three hundred names down reads as nothing happening.
  const addModel = (m: string) => {
    setModels((cur) => (cur.includes(m) ? cur : [m, ...cur]));
    setPicked((cur) => (cur.includes(m) ? cur : [...cur, m]));
    setFacts((cur) => ({ ...cur, [m]: { origin: "manual" } }));
  };

  // Re-asking the endpoint is how a source that gained models catches up; the
  // ticks the user already made survive it.
  // A blank key field means "keep the stored one", so the kernel is asked
  // through the saved source with the form's address, key and protocol laid
  // over it: what is being edited is what gets tested.
  const refetch = async () => {
    setBusy(`refresh:${entry.name}`);
    setErr(null);
    setRefreshFail("");
    setDiff(null);
    try {
      const refreshed = await port.checkProvider(entry.name, { baseUrl: baseUrl.trim(), apiKey: apiKey.trim(), kind });
      if ("ok" in refreshed && !refreshed.ok) {
        setRefreshFail(checkFailure(refreshed));
        return;
      }
      const changed = !!refreshed.baseUrl && refreshed.baseUrl !== baseUrl.trim();
      if (changed) setBaseUrl(refreshed.baseUrl!);
      setCompleted(changed ? refreshed.baseUrl! : "");
      const found = refreshed.models ?? [];
      if (found.length === 0) {
        setRefreshFail(say({ code: "provider.probe.no_chat_models", params: { count: 0 } }));
        return;
      }
      const readers = refreshed.vision ?? [];
      setModels((current) => {
        setDiff(catalogDiff(current, found, true));
        setFacts((factsNow) => refreshedFacts(current, found, factsNow));
        return [...new Set([...found, ...current])];
      });
      setVision((current) => [...new Set([...current, ...readers])]);
      setVisionSettable((current) => current ? [...new Set([...current, ...readers])] : current);
    } catch (e) {
      setRefreshFail(reason(e));
    } finally {
      setBusy("");
    }
  };

  const checkModel = async (model: string) => {
    if (checkingModel || busy) return;
    setCheckingModel(model);
    setFacts((current) => ({
      ...current,
      [model]: { ...(current[model] ?? { origin: "configured" }), checking: true },
    }));
    try {
      const got = await port.checkProviderModel({
        name: entry.name,
        model,
        baseUrl: baseUrl.trim(),
        apiKey: apiKey.trim(),
        kind,
      });
      setFacts((current) => ({
        ...current,
        [model]: checkedFact(current[model], "configured", got),
      }));
    } catch {
      setFacts((current) => ({
        ...current,
        [model]: { ...(current[model] ?? { origin: "configured" }), status: "unknown", reason: "network" },
      }));
    } finally {
      setCheckingModel("");
      setFacts((current) => ({
        ...current,
        [model]: { ...(current[model] ?? { origin: "configured" }), checking: false },
      }));
    }
  };

  // A connection list typed here is what every inheriting model gets; with
  // none typed, the kernel's answer holds only while the form still matches
  // what was saved.
  const savedLevels = (entry.supportedEfforts ?? []).join(",");
  const inheritedFor = (model: string): ModelEffort | null | undefined => {
    if (levels.length > 0 && !levelsDormant) {
      return { supportedEfforts: levels, defaultEffort: levels.includes(defEffort) ? defEffort : "" };
    }
    if (think !== (entry.reasoningProtocol ?? "") || levels.join(",") !== savedLevels) return null;
    return entry.inheritedEfforts?.[model];
  };

  // A provider-wide value typed here is what every model without its own
  // inherits; until it is saved, the kernel's answer holds only for what was
  // saved, so an edited field is answered from the form instead.
  const inheritedLimitsFor = (model: string): ModelLimit | null | undefined => {
    const typedWin = Number(win) || 0;
    const typedOut = Number(maxOut) || 0;
    const base = entry.inheritedLimits?.[model];
    const winMoved = typedWin !== (entry.contextWindow ?? 0);
    const outMoved = typedOut !== (entry.maxOutputTokens ?? 0);
    return {
      contextWindow: winMoved ? typedWin || undefined : base?.contextWindow,
      maxOutputTokens: outMoved ? typedOut || undefined : base?.maxOutputTokens,
    };
  };

  const thinkingShape = (className: string) => (
    <EffortShape field={think === (entry.reasoningProtocol ?? "") || kind !== entry.kind ? effortField : undefined} level={levels[0]} className={className} />
  );

  const draft = (): ProviderEdit => ({
    name: entry.name,
    ...(kind !== entry.kind && { kind }),
    baseUrl: baseUrl.trim(),
    apiKey: apiKey.trim(),
    models: picked,
    ...(hidePrefix !== (entry.hideModelPrefix === true) && { hideModelPrefix: hidePrefix }),
    modelAliases: Object.fromEntries(Object.entries(aliases).filter(([m, value]) => picked.includes(m) && value.trim()).map(([m, value]) => [m, value.trim()])),
    default: picked.includes(def) ? def : picked[0] ?? "",
    vision: vision.filter((m) => picked.includes(m)),
    contextWindow: Number(win.replace(/\D/g, "")) || 0,
    maxOutputTokens: Number(maxOut.replace(/\D/g, "")) || 0,
    idleTimeoutSeconds: idle.ok ? idle.secs : 0,
    ...(billing !== (entry.billingMode || PAYG) && { billingMode: billing }),
    ...(balUrl.trim() !== (entry.balanceUrl ?? "") && { balanceUrl: balUrl.trim() }),
    ...(balFormat !== (entry.balanceFormat ?? "") && { balanceFormat: balFormat }),
    reasoningProtocol: think,
    ...(wireSwitches && entry.canWebSearch && search !== (entry.webSearch ?? false) && { webSearch: search }),
    ...(wireSwitches && entry.canSetContinuation && carry !== (entry.continuation ?? "") && { continuation: carry }),
    supportedEfforts: levels,
    defaultEffort: levels.includes(defEffort) ? defEffort : "",
    modelEfforts: Object.fromEntries(picked.filter((m) => ownEfforts[m]).map((m) => [m, ownEfforts[m]])),
    modelLimits: limitsToSend(picked, limitText, entry.modelLimits ?? {}),
    modelPrices: pricesToSend(picked, priceText, entry.modelPrices ?? {}),
    headers: parseHeaders(heads),
    extraBody: parseExtraBody(extra) ?? {},
  });

  // What a save would send, with order and unparsable text normalised: ticking
  // a row off and on again, or typing a value back, is no change.
  const fingerprint = (readers = vision) => JSON.stringify({
    ...draft(), models: [...picked].sort(), vision: readers.filter((m) => picked.includes(m)).sort(),
    idle: idle.ok ? idle.secs : idleText, extra: parseExtraBody(extra) ?? extra,
  });
  // The baseline is the saved configuration; image support a connection test
  // seeded into the form is a change until it is saved.
  const [stored, setStored] = useState(() => fingerprint(entry.visionModels ?? []));
  const draftKey = fingerprint();
  const dirty = draftKey !== stored;
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);
  useEffect(() => () => onDirty?.(false), [onDirty]);
  const { batch, testAll } = useModelBatch({
    port, picked, draftKey, setFacts, origin: "configured",
    blocked: checkingModel !== "" || busy !== "",
    request: { name: entry.name, baseUrl: baseUrl.trim(), apiKey: apiKey.trim(), kind },
  });
  const testing = checkingModel !== "" || batch === "running";
  const [edited, setEdited] = useState(false);
  if (dirty && !edited) setEdited(true);

  const canSave = (dirty || err?.kind === "unapplied") && busy === "" && !testing && picked.length > 0 && !extraBad && idle.ok && !picked.some((m) => incomplete(priceText[m]) || sourceBad(priceText[m]));
  const phase: Phase = saving ? "saving"
    : err?.kind === "save" && dirty ? "failed"
      : dirty ? "dirty"
        : err?.kind === "unapplied" ? "unapplied"
          : justSaved && !edited ? "saved" : "clean";

  const save = async () => {
    setBusy(`edit:${entry.name}`);
    setErr(null);
    const sent = fingerprint();
    try {
      await port.editProvider(draft());
      await onDone();
    } catch (e) {
      const unapplied = savedNotApplied(e);
      setErr({ text: reason(e), kind: unapplied ? "unapplied" : "save" });
      // Saved but not yet applied: the list has to show what is on file while
      // the form stays open to say why.
      if (unapplied) {
        setStored(sent);
        onSaved?.();
      }
    } finally {
      setBusy("");
    }
  };

  return (
    <fieldset className="addp" data-edit disabled={saving} data-action-keydown="provider.save" onKeyDown={(e) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey || e.key.toLowerCase() !== "s") return;
      e.preventDefault();
      if (canSave) void save();
    }}>
      <div className="fields">
        {kinds.length > 1 && (
          <label className="grow full">
            <span>{t("models.api_protocol")}</span>
            <select aria-label={t("models.api_protocol")} data-action="provider.draft" data-value="protocol" value={kind} onChange={(e) => {
              setKind(e.target.value);
              setFacts(clearModelCheckFacts);
            }} disabled={busy !== "" || checkingModel !== ""}>
              {kinds.map((k) => <option key={k} value={k}>{t(KIND_LABEL[k] ?? k)}</option>)}
            </select>
            <i className="tip">{t("models.follow_the_provider_documentation_a")}</i>
          </label>
        )}
        <label className="grow full">
          <span>{t("models.endpoint_address")}</span>
          <input value={baseUrl} onChange={(e) => {
            setBaseUrl(e.target.value);
            setFacts(clearModelCheckFacts);
            setRefreshFail("");
            setDiff(null);
          }} disabled={busy !== "" || checkingModel !== ""} spellCheck={false} />
        </label>
        <label className="grow full">
          <span>{t("models.api_key_leave_blank_to")}</span>
          <input type="password" value={apiKey} placeholder="········"
            onChange={(e) => {
              setApiKey(e.target.value);
              setFacts(clearModelCheckFacts);
              setRefreshFail("");
              setDiff(null);
            }} disabled={busy !== "" || checkingModel !== ""} spellCheck={false} />
        </label>
      </div>

      <div className="addp-section compact">
        <div className="addp-section-head">
          <div><strong>{t("models.model_limits")}</strong><span>{t("models.use_the_model_documentation_instead")}</span></div>
        </div>
        <div className="fields token-limits">
          <label className="grow">
            <span>{t("common.context_window")}</span>
            <span className="unit-field"><input aria-label={t("common.context_window")} inputMode="numeric" value={win} placeholder={t("common.not_declared")} onChange={(e) => setWin(e.target.value.replace(/\D/g, ""))} /><i>tokens</i></span>
            <i className="tip">{t("models.the_total_input_tool_results")}</i>
          </label>
          <label className="grow">
            <span>{t("models.maximum_output")}</span>
            <span className="unit-field"><input aria-label={t("models.maximum_output")} inputMode="numeric" value={maxOut} placeholder={t("common.auto")} onChange={(e) => setMaxOut(e.target.value.replace(/\D/g, ""))} /><i>tokens</i></span>
            <i className="tip">{t("models.the_per_turn_generation_limit")}</i>
          </label>
        </div>
        {(picked.length > 1 || picked.some((m) => entry.modelLimits?.[m])) && (
          <ModelLimits models={picked} value={limitText} onChange={setLimitText} inherited={inheritedLimitsFor} labels={entry.modelShorts} />
        )}
        {picked.length > 0 && <ModelPrices models={picked} value={priceText} onChange={setPriceText} official={entry.officialPricing === true} stored={entry.modelPrices ?? {}} labels={entry.modelShorts}
          onOpenSource={(url) => void Promise.resolve(port.openExternal(url)).catch(() => {})} />}
        {picked.length > 0 && <div className="fields"><BillingModeField value={billing} onChange={setBilling} /></div>}
      </div>

      <div className="vway" data-field="hide-model-prefix">
        <span className="lb">{t("models.hide_the_shared_prefix")}</span>
        <div className="seg" role="group" aria-label={t("models.model_name_prefix_of_name", { name: entry.displayName?.trim() || entry.name })}>
          {[true, false].map((on) => (
            <button key={String(on)} type="button" data-action="provider.draft" data-value={`hide-model-prefix:${on}`}
              aria-pressed={hidePrefix === on} disabled={busy !== ""} onClick={() => setHidePrefix(on)}>
              {t(on ? "models.on" : "common.off")}
            </button>
          ))}
        </div>
        <span className="why">{t("models.the_same_prefix_a_routing")}</span>
      </div>

      <div className="mlist">
        <div className="mlhead">
          <span className="ttl">{t("common.models")}</span>
          <span className="count">{t("models.on_of_all_enabled", { on: picked.length, all: models.length })}</span>
          {listable && (
            <button className="mrefresh" data-action="provider.probe" onClick={refetch} disabled={busy !== "" || testing}
              title={t("models.read_the_model_list_from")}>
              {t(refreshing ? "common.reading_from_provider" : "models.read_available_models_from_provider")}
            </button>
          )}
          <button className="mrefresh" data-action="provider.model-check-all" onClick={testAll}
            disabled={busy !== "" || testing || picked.length === 0} aria-busy={batch === "running" || undefined}>
            {t("models.test_enabled_models_n", { n: picked.length })}
          </button>
        </div>
        <p className="mguide">
          {t("models.the_catalog_is_for_discovery")}
        </p>
        {completed !== "" && completed === baseUrl.trim() && (
          <p className="mdiff" role="status">{t("common.address_completed_to_url", { url: completed })}</p>
        )}
        {batch === "stopped" && <p className="mdiff" role="status">{t("models.no_further_checks_started_the")}</p>}
        {diff && diff.fresh && diff.added === 0 && diff.missing === 0 && (
          <p className="mdiff" role="status">{t("models.no_new_models_found")}</p>
        )}
        {diff && (diff.added > 0 || diff.missing > 0) && (
          <p className="mdiff" role="status">
            {diff.added > 0 && t("models.found_n_new_models", { n: diff.added })}
            {diff.missing > 0 && t("models.n_configured_models_were_not", { n: diff.missing })}
          </p>
        )}
        <ModelChoice
          models={models}
          picked={picked}
          vision={vision}
          def={def}
          facts={facts}
          visionLocked={visionLocked}
          onCheck={checkModel}
          aliases={aliases}
          labels={entry.modelShorts}
          onAlias={(m, value) => setAliases((current) => ({ ...current, [m]: value }))}
          checkDisabled={busy !== "" || testing}
          onToggle={(m) => toggle(picked, setPicked, m)}
          onVision={(m) => toggle(vision, setVision, m)}
          onDefault={setDef}
          onAdd={addModel}
          onFetch={listable ? refetch : undefined}
          fetching={refreshing}
          fetchDisabled={busy !== "" || testing}
          fetchFail={refreshFail}
        />
      </div>

      {/* Folded, and worth folding: no probe can answer these, and most
          endpoints need none of them. The fold names its contents, because a
          relay's effort levels are declared nowhere else. */}
      {wireSwitches && hasEndpointSwitches(entry) && (
        <EndpointSwitches
          entry={entry} label={label} disabled={saving} shape={thinkingShape("why")}
          search={search} sendsThinking={think !== "none"} continuation={carry}
          onSearch={setSearch} onThinking={(send) => setThink(send ? (think === "none" ? (entry.reasoningProtocol === "none" ? "" : entry.reasoningProtocol ?? "") : think) : "none")} onContinuation={setCarry}
        />
      )}

      <details className="addp-options" open={more} onToggle={(e) => setMore(e.currentTarget.open)}>
        <summary data-action="provider.draft" data-value="compat">
          <span className="tx">
            <strong>{t("models.thinking_and_reasoning_levels")}</strong>
            <small>{t("models.plus_extra_headers_and_body")}</small>
          </span>
          <span className="summary-value">
            {compatSummary(think, heads, extra, levels.length, picked.filter((m) => ownEfforts[m]?.supportedEfforts.length).length, idle.ok ? idle.secs : 0) || t("common.optional")}
          </span>
        </summary>
        {more && (
          <div className="fields compat addp-options-body">
            <label className="grow full">
              <span>{t("models.thinking_parameter")}</span>
              <select ref={reasoning} value={think} onChange={(e) => setThink(e.target.value)}>
                {THINKING.map(([value, label]) => (
                  <option key={value} value={value}>
                    {t(label)}
                  </option>
                ))}
              </select>
              <i className="tip">
                {t("models.how_this_endpoint_expresses_thinking")}
              </i>
              {!(wireSwitches && entry.canSetThinking) && thinkingShape("tip")}
            </label>
            <label className="grow">
              <span>{t("models.reasoning_levels")}</span>
              <input
                data-action="provider.draft"
                data-value="effort-levels"
                value={levelText}
                spellCheck={false}
                placeholder="low, medium, high"
                disabled={levelsDormant}
                onChange={(e) => setLevelText(e.target.value)}
              />
              <i className="tip">
                {levelsDormant
                  ? t("models.this_thinking_parameter_does_not")
                  : effortField
                    ? t("models.the_values_this_endpoint_accepts", { field: effortField })
                    : t("models.the_values_this_endpoint_accepts_2")}
              </i>
            </label>
            <label className="grow">
              <span>{t("models.default_level")}</span>
              <select data-action="provider.draft" data-value="default-effort" value={levels.includes(defEffort) ? defEffort : ""} disabled={levelsDormant || levels.length === 0}
                onChange={(e) => setDefEffort(e.target.value)}>
                <option value="">{t("models.the_first_level")}</option>
                {levels.map((l) => <option key={l} value={l}>{l}</option>)}
              </select>
              <i className="tip">{t("models.the_level_used_when_reasoning")}</i>
            </label>
            {(picked.length > 1 || picked.some((m) => ownEfforts[m])) && (
              <ModelEfforts
                models={picked}
                value={ownEfforts}
                onChange={setOwnEfforts}
                inherited={inheritedFor}
                protocols={entry.modelProtocols ?? {}}
                connProtocol={think}
              />
            )}
            <label className="grow">
              <span>{t("models.no_answer_timeout")}</span>
              <span className="unit-field">
                <input
                  aria-label={t("models.no_answer_timeout")}
                  data-action="provider.draft"
                  data-value="idle-timeout"
                  inputMode="numeric"
                  value={idleText}
                  placeholder={entry.idleTimeoutDefault ? t("models.default_n", { n: entry.idleTimeoutDefault }) : t("common.default")}
                  aria-invalid={!idle.ok || undefined}
                  onChange={(e) => setIdleText(e.target.value)}
                />
                <i>{t("common.s")}</i>
              </span>
              <i className="tip">
                {t("models.if_the_server_sends_nothing")}
              </i>
            </label>
            <BalanceFields entry={entry} url={balUrl} format={balFormat} onUrl={setBalUrl} onFormat={setBalFormat} />
            {!idle.ok && <div className="why">{t("models.enter_a_whole_number_of", { min: IDLE_TIMEOUT_MIN, max: IDLE_TIMEOUT_MAX })}</div>}
            <label className="grow full">
              <span>{t("models.extra_request_headers")}</span>
              <textarea
                rows={3}
                value={heads}
                spellCheck={false}
                placeholder={"HTTP-Referer: https://example.com\nX-Title: Reasonix"}
                onChange={(e) => setHeads(e.target.value)}
              />
              <i className="tip">{t("models.one_name_value_per_line")}</i>
            </label>
            <label className="grow full">
              <span>{t("models.extra_request_body")}</span>
              <textarea
                rows={4}
                value={extra}
                spellCheck={false}
                placeholder={'{\n  "enable_thinking": true\n}'}
                onChange={(e) => setExtra(e.target.value)}
                aria-invalid={extraBad || undefined}
              />
              <i className="tip">
                {t("models.merged_into_the_top_level")}
              </i>
            </label>
            {extraBad && <div className="why">{t("models.this_is_not_a_valid")}</div>}
          </div>
        )}
      </details>

      <div className="acts-bar">
        {err && (
          <div className="find" data-lvl={err.kind === "unapplied" ? "warn" : "err"} role={err.kind === "unapplied" ? "status" : "alert"}>
            {err.kind === "save" && <span className="t">{t("models.save_failed")}</span>}
            <span className="why">{err.text}</span>
          </div>
        )}
        <div className="acts">
          <button className="act" data-action="provider.save" data-primary onClick={save} disabled={!canSave}>
            {t(saving ? "common.saving" : "common.save")}
          </button>
          <button className="act" data-action="provider.revert" onClick={onRevert} disabled={!dirty || busy !== "" || testing}>{t("common.revert")}</button>
          <span className="acts-state" role="status" data-state={phase}>
            <i className="acts-dot" aria-hidden="true" />
            {t(PHASE_TEXT[phase])}
          </span>
        </div>
      </div>
    </fieldset>
  );
}

function modelFacts(configured: string[], check?: ProviderCheck): Record<string, ModelFact> {
  const found = new Set(check?.ok ? check.models ?? [] : []);
  const out: Record<string, ModelFact> = {};
  for (const model of configured) out[model] = { origin: check?.ok ? (found.has(model) ? "endpoint" : "missing") : "configured" };
  for (const model of found) if (!out[model]) out[model] = { origin: "endpoint" };
  return out;
}

function refreshedFacts(models: string[], found: string[], current: Record<string, ModelFact>): Record<string, ModelFact> {
  const listed = new Set(found);
  const out = { ...current };
  for (const model of [...new Set([...found, ...models])]) {
    const previous = current[model];
    out[model] = {
      ...previous,
      origin: listed.has(model) ? "endpoint" : previous?.origin === "manual" ? "manual" : "missing",
    };
  }
  return out;
}

function catalogDiff(before: string[], found: string[], fresh: boolean) {
  const had = new Set(before);
  const now = new Set(found);
  return {
    fresh,
    added: found.filter((model) => !had.has(model)).length,
    missing: before.filter((model) => !now.has(model)).length,
  };
}

function compatSummary(think: string, heads: string, extra: string, levels: number, ownModels: number, idleSecs: number): string {
  const parts: string[] = [];
  const protocol = THINKING.find(([value]) => value === think);
  if (think && protocol) parts.push(t(protocol[1]));
  if (levels) parts.push(t("models.n_levels", { n: levels }));
  if (ownModels) parts.push(t("models.n_models_with_their_own", { n: ownModels }));
  if (idleSecs) parts.push(t("models.n_s_no_answer_timeout", { n: idleSecs }));
  const headCount = Object.keys(parseHeaders(heads)).length;
  if (headCount) parts.push(t("models.n_headers", { n: headCount }));
  const body = parseExtraBody(extra);
  if (body && Object.keys(body).length) parts.push(t("models.extra_body"));
  return parts.join(" · ");
}

type Phase = "clean" | "dirty" | "saving" | "saved" | "unapplied" | "failed";
const PHASE_TEXT: Record<Phase, string> = {
  clean: "models.no_changes", dirty: "models.unsaved_changes", failed: "models.unsaved_changes", saving: "common.saving_2",
  saved: "models.saved", unapplied: "common.saved_not_applied_yet",
};
