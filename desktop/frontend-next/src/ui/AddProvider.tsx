import { StudioIcon } from "./StudioIcon";
import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import type { Protocol, ProviderEntry, ProviderPreset, ProviderProbe } from "../port/port";
import { PresetGallery } from "./PresetGallery";
import { PresetInstall } from "./PresetInstall";
import { checkedFact, clearModelCheckFacts, ModelChoice, type ModelFact } from "./ModelChoice";
import { KIND_LABEL, hostOf, nameFrom, sourceNameUsable, vendorLabel } from "./vendors";
import type { Port } from "./Providers";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/http_error";
import { EffortShape } from "./EffortShape";
import { THINKING, parseExtraBody, parseHeaders } from "./provider_compat";
import { useModelBatch } from "./useModelBatch";

// Detection is assistance, not authority. Compatible relays often expose one
// model-list shape while expecting another request protocol, so every durable
// value remains editable and a failed probe never blocks manual setup.
const NAME_CODES = new Set(["provider.name_required", "provider.name_invalid", "provider.name_taken"]);

export function AddProvider({
  port, taken, known, onDone, onCancel,
}: {
  port: Port; taken: string[]; known: ProviderEntry[]; onDone: () => void; onCancel: () => void;
}) {
  const [baseUrl, setBaseUrl] = useState("");
  const [completed, setCompleted] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [probe, setProbe] = useState<ProviderProbe | null>(null);
  const [preset, setPreset] = useState<ProviderPreset | null>(null);
  const [catalog, setCatalog] = useState<Protocol[]>([]);
  const [err, setErr] = useState("");
  const [errOnSave, setErrOnSave] = useState(false);
  const [nameErr, setNameErr] = useState("");
  const [composing, setComposing] = useState(false);
  const [busy, setBusy] = useState(false);

  // The endpoint says which wires are on the table and the kernel says what
  // each one is, so neither list is written here.
  useEffect(() => {
    port.protocols().then((items) => {
      setCatalog(items);
      setKind((current) => current || (items.some((p) => p.kind === "openai") ? "openai" : items[0]?.kind ?? ""));
    }).catch(() => setCatalog([]));
  }, [port]);
  const loadPresets = useCallback(() => port.providerPresets(), [port]);
  const choices = catalog.map((p) => p.kind);
  const detectedChoices = probe ? (probe.kinds?.length ? probe.kinds : [probe.kind]) : [];
  const searchOn = detectedChoices.filter((k) => catalog.find((p) => p.kind === k)?.serverWebSearch);
  const searchSplit = searchOn.length > 0 && searchOn.length < detectedChoices.length;

  // Everything below is editable after the probe, because every one of these
  // is something the endpoint could not tell us for certain.
  const [name, setName] = useState("");
  const [kind, setKind] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  // What the probe reported, plus anything typed in. Kept apart from `probe` so
  // an added name survives without pretending the endpoint reported it.
  const [models, setModels] = useState<string[]>([]);
  const [facts, setFacts] = useState<Record<string, ModelFact>>({});
  const [checkingModel, setCheckingModel] = useState("");
  const [win, setWin] = useState("");
  const [maxOut, setMaxOut] = useState("");
  const [thinkingOn, setThinkingOn] = useState(true);
  const [thinkingProtocol, setThinkingProtocol] = useState("");
  const [heads, setHeads] = useState("");
  const [extra, setExtra] = useState("");
  const [noProxy, setNoProxy] = useState(false);
  const nameBad = !composing && name.trim() !== "" && !sourceNameUsable(name);
  const extraBad = extra.trim() !== "" && parseExtraBody(extra) === null;
  const protocolCanThink = catalog.find((p) => p.kind === kind)?.reasoningParams ?? false;
  const wire = catalog.find((p) => p.kind === kind);
  const effortField = wire?.effortUnder?.length && !wire.effortUnder.includes(thinkingProtocol) ? undefined : wire?.effortField;
  // A wire with no listing shape has nothing to read: the model id is declared
  // rather than discovered, and offering a probe that must fail reads as a
  // broken endpoint instead of a protocol that never had one.
  const protocolCanProbe = (catalog.find((p) => p.kind === kind)?.discovery ?? "") !== "";

  // A source already at this host changes what a blank key means: another door
  // onto that account rather than an account with no credential.
  const sibling = known.find((p) => hostOf(p.baseUrl) === hostOf(baseUrl) && baseUrl.trim() !== "");

  const connect = async () => {
    setBusy(true);
    setErr("");
    setErrOnSave(false);
    try {
      const got = await port.probeProvider(baseUrl.trim(), apiKey.trim());
      setProbe(got);
      const resolved = got.baseUrl?.trim() ?? "";
      const changed = resolved !== "" && resolved !== baseUrl.trim();
      if (changed) setBaseUrl(resolved);
      setCompleted(changed ? resolved : "");
      setKind((current) => current || got.kind);
      setModels((current) => [...new Set([...got.models, ...current])]);
      setPicked((current) => current.length ? current : got.models.slice(0, 8));
      setFacts((current) => ({
        ...Object.fromEntries(got.models.map((model) => [model, { origin: "endpoint" as const }])),
        ...current,
      }));
      setName((current) => current.trim() || uniqueName(nameFrom(baseUrl), taken));
    } catch (e) {
      setErr(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const save = async () => {
    setBusy(true);
    setErr("");
    setNameErr("");
    setErrOnSave(true);
    try {
      await port.saveProvider({
        name: name.trim(),
        kind,
        baseUrl: baseUrl.trim(),
        apiKey: apiKey.trim(),
        models: picked,
        default: picked[0] ?? "",
        authHeader: probe?.authHeader ?? false,
        noProxy: noProxy || (probe?.noProxy ?? false),
        effort: "",
        vision: (probe?.vision ?? []).filter((m) => picked.includes(m)),
        contextWindow: numeric(win),
        maxOutputTokens: numeric(maxOut),
        reasoningProtocol: protocolCanThink ? (thinkingOn ? thinkingProtocol : "none") : undefined,
        headers: parseHeaders(heads),
        extraBody: parseExtraBody(extra) ?? {},
      });
      onDone();
    } catch (e) {
      if (e instanceof HttpError && e.reason?.code && NAME_CODES.has(e.reason.code)) setNameErr(reason(e));
      else setErr(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const toggle = (m: string) =>
    setPicked((cur) => (cur.includes(m) ? cur.filter((x) => x !== m) : [...cur, m]));

  const addModel = (m: string) => {
    setModels((cur) => (cur.includes(m) ? cur : [m, ...cur]));
    setPicked((cur) => (cur.includes(m) ? cur : [...cur, m]));
    setFacts((cur) => ({ ...cur, [m]: { origin: "manual" } }));
  };

  const checkModel = async (model: string) => {
    if (testing || busy) return;
    setCheckingModel(model);
    setFacts((current) => ({
      ...current,
      [model]: { ...(current[model] ?? { origin: "manual" }), checking: true },
    }));
    try {
      const got = await port.checkProviderModel({
        model,
        baseUrl: baseUrl.trim(),
        apiKey: apiKey.trim(),
        kind,
        authHeader: probe?.authHeader ?? false,
        noProxy: noProxy || (probe?.noProxy ?? false),
      });
      setFacts((current) => ({
        ...current,
        [model]: checkedFact(current[model], "manual", got),
      }));
    } catch {
      setFacts((current) => ({
        ...current,
        [model]: { ...(current[model] ?? { origin: "manual" }), status: "unknown", reason: "network" },
      }));
    } finally {
      setCheckingModel("");
      setFacts((current) => ({
        ...current,
        [model]: { ...(current[model] ?? { origin: "manual" }), checking: false },
      }));
    }
  };

  const draftKey = JSON.stringify([name, baseUrl, apiKey, kind, picked, win, maxOut, thinkingOn, thinkingProtocol, heads, extra, noProxy, probe?.authHeader, probe?.noProxy]);
  const { batch, testAll } = useModelBatch({
    port, picked, draftKey, setFacts, origin: "manual",
    blocked: checkingModel !== "" || busy || baseUrl.trim() === "",
    request: {
      baseUrl: baseUrl.trim(), apiKey: apiKey.trim(), kind,
      authHeader: probe?.authHeader ?? false, noProxy: noProxy || (probe?.noProxy ?? false),
    },
  });
  const testing = checkingModel !== "" || batch === "running";
  if (preset) return <PresetInstall port={port} preset={preset} onDone={onDone} onCancel={onCancel} onBack={() => setPreset(null)} />;

  return (
    <fieldset className="addp" disabled={busy && errOnSave}>
      <div className="addp-head">
        <div>
          <span className="step">{t("models.custom_source")}</span>
          <strong>{t("models.add_a_model_source")}</strong>
          <p>{t("models.use_the_service_documentation_connection")}</p>
        </div>
        <button className="addp-close" onClick={onCancel} disabled={busy || testing} aria-label={t("common.cancel")}><StudioIcon name="close" /></button>
      </div>
      {baseUrl.trim() === "" && <PresetGallery load={loadPresets} onPick={setPreset} disabled={busy || checkingModel !== ""} />}
      <div className="fields">
        <label className="grow">
          <span>{t("models.api_protocol")}</span>
          <select aria-label={t("models.api_protocol")} data-action="provider.draft" data-value="protocol" value={kind} onChange={(e) => {
            setKind(e.target.value);
            setFacts(clearModelCheckFacts);
          }} disabled={busy || testing}>
            {choices.map((k) => <option key={k} value={k}>{t(KIND_LABEL[k] ?? k)}</option>)}
          </select>
          <i className="tip">{t("models.follow_the_provider_documentation_a")}</i>
        </label>
        <label className="grow">
          <span>{t("models.endpoint_address")}</span>
          <input
            aria-label={t("models.endpoint_address")}
            data-action="provider.draft" data-value="endpoint"
            value={baseUrl}
            placeholder="https://api.moonshot.cn/v1"
            onChange={(e) => {
              if (e.target.value.trim() !== baseUrl.trim()) {
                setProbe(null);
                setCompleted("");
              }
              setBaseUrl(e.target.value);
              setFacts(clearModelCheckFacts);
            }}
            disabled={busy || testing}
            spellCheck={false}
          />
          {wire?.answers === "decision" && <i className="tip">{t("models.enter_the_services_base_address")}</i>}
        </label>
        <label className="grow full">
          <span>API Key{t(sibling ? "models.leave_blank_to_keep_the" : "")}</span>
          <input type="password" data-action="provider.draft" data-value="credential" value={apiKey} placeholder={sibling ? "········" : ""}
            onChange={(e) => {
              setApiKey(e.target.value);
              setFacts(clearModelCheckFacts);
            }} disabled={busy || testing} spellCheck={false} />
        </label>
        <p className="addp-privacy">{t("common.the_api_key_is_stored")}</p>
        <div className="grow full name-cell">
        <label>
          <span>{t("common.source_name")}</span>
          <input
            aria-label={t("common.source_name")}
            data-action="provider.draft" data-value="name"
            value={name}
            placeholder={t("models.made_after_connecting_editable")}
            onChange={(e) => { setName(e.target.value); setNameErr(""); }}
            disabled={busy || testing}
            spellCheck={false}
            aria-invalid={nameBad || nameErr !== "" || undefined}
            aria-describedby="addp-name-rule"
            onCompositionStart={() => setComposing(true)}
            onCompositionEnd={() => setComposing(false)}
          />
        </label>
        <p id="addp-name-rule" className="name-rule" data-bad={nameBad || nameErr !== "" || undefined} aria-live="polite">
          {nameErr !== "" ? nameErr : t("models.use_only_letters_digits_dots")}
        </p>
        </div>
        {sibling && (
          <p className="acct-note">
            {t("models.this_address_already_holds_name", { name: vendorLabel(hostOf(sibling.baseUrl)) })}
            {t("models.this_adds_a_second_way")}
          </p>
        )}
      </div>

      <div className="addp-section">
        <div className="addp-section-head">
          <div><strong>{t("models.model_limits")}</strong><span>{t("models.defaults_for_models_from_this")}</span></div>
        </div>
        <div className="fields token-limits">
          <label className="grow">
            <span>{t("common.context_window")}</span>
            <span className="unit-field"><input aria-label={t("common.context_window")} data-action="provider.draft" data-value="context-window" inputMode="numeric" value={win} placeholder="128000" onChange={(e) => setWin(digits(e.target.value))} /><i>tokens</i></span>
            <i className="tip">{t("models.the_shared_capacity_for_input")}</i>
          </label>
          <label className="grow">
            <span>{t("models.maximum_output")}</span>
            <span className="unit-field"><input aria-label={t("models.maximum_output")} data-action="provider.draft" data-value="max-output" inputMode="numeric" value={maxOut} placeholder={t("common.auto")} onChange={(e) => setMaxOut(digits(e.target.value))} /><i>tokens</i></span>
            <i className="tip">{t("models.the_per_turn_reply_limit")}</i>
          </label>
        </div>
      </div>

      <details className="addp-options">
        <summary>
          <span className="tx">
            <strong>{t("models.advanced_connection_options")}</strong>
            <small>{t("models.thinking_controls_proxy_and_gateway")}</small>
          </span>
          <span className="summary-value">{t("common.optional")}</span>
        </summary>
        <div className="addp-options-body">
          <div className="setting-line">
            <span className="setting-copy">
              <strong>{t("models.send_thinking_controls")}</strong>
              <small>{t(protocolCanThink ? "models.allow_this_protocol_to_send" : "models.this_protocol_has_no_thinking")}</small>
            </span>
            <button
              type="button"
              className="switch-control"
              data-action="provider.draft" data-value="thinking"
              role="switch"
              aria-label={t("models.send_thinking_controls")}
              aria-checked={protocolCanThink && thinkingOn}
              disabled={!protocolCanThink || busy || testing}
              onClick={() => setThinkingOn((v) => !v)}
            ><span /></button>
          </div>
          {protocolCanThink && thinkingOn && (
            <label className="advanced-field">
              <span>{t("models.thinking_protocol")}</span>
              <select value={thinkingProtocol} onChange={(e) => setThinkingProtocol(e.target.value)}>
                {THINKING.filter(([value]) => value !== "none").map(([value, label]) => (
                  <option key={value} value={value}>{t(label)}</option>
                ))}
              </select>
              <i>{t("models.use_auto_to_follow_the")}</i>
              <EffortShape field={effortField} />
            </label>
          )}
          <div className="setting-line">
            <span className="setting-copy">
              <strong>{t("models.bypass_system_proxy")}</strong>
              <small>{t("models.enable_only_when_the_endpoint")}</small>
            </span>
            <button type="button" className="switch-control" data-action="provider.draft" data-value="no-proxy" role="switch" aria-label={t("models.bypass_system_proxy")} aria-checked={noProxy || (probe?.noProxy ?? false)}
              disabled={busy || testing || probe?.noProxy === true} onClick={() => {
                setNoProxy((v) => !v);
                setFacts(clearModelCheckFacts);
              }}><span /></button>
          </div>
          <label className="advanced-field">
            <span>{t("models.extra_request_headers")}</span>
            <textarea rows={3} value={heads} spellCheck={false}
              placeholder={"HTTP-Referer: https://example.com\nX-Title: Reasonix"}
              onChange={(e) => setHeads(e.target.value)} />
            <i>{t("models.one_name_value_pair_per")}</i>
          </label>
          <label className="advanced-field">
            <span>{t("models.extra_request_body")}</span>
            <textarea rows={4} value={extra} spellCheck={false} aria-invalid={extraBad || undefined}
              placeholder={'{\n  "temperature": 0.7\n}'} onChange={(e) => setExtra(e.target.value)} />
            <i>{t("models.only_for_optional_fields_required")}</i>
            {extraBad && <em className="field-error">{t("models.enter_a_valid_json_object")}</em>}
          </label>
        </div>
      </details>

      <div className="addp-section">
        <div className="addp-section-head">
          <div>
            <strong>{t("models.model_catalogue")}</strong>
            <span>{t(protocolCanProbe ? "models.add_at_least_one_model" : "models.this_protocol_has_no_model")}</span>
          </div>
          {protocolCanProbe && (
            <button className="act quiet" data-action="provider.probe" onClick={connect} disabled={busy || testing || baseUrl.trim() === ""}>
              {t(busy ? "models.checking_2" : "models.check_connection_and_read_models")}
            </button>
          )}
        </div>
        {err && !errOnSave && (
          <div className="find" data-lvl="warn" role="alert">
            <span className="t">{t("models.unreachable")}</span>
            <span className="why">{err}</span>
          </div>
        )}
        {probe && <p className="probe-ok">{t("models.connection_works_n_models_found", { n: probe.models.length })}</p>}
        {completed !== "" && completed === baseUrl.trim() && <p className="probe-ok">{t("common.address_completed_to_url", { url: completed })}</p>}
        <div className="mlist">
          <div className="mlhead">
            <span className="ttl">{t("models.enabled_models")}</span>
            <span className="count">{t("models.on_of_all_enabled", { on: picked.length, all: models.length })}</span>
            <button className="mrefresh" data-action="provider.model-check-all" onClick={testAll}
              disabled={busy || testing || picked.length === 0 || baseUrl.trim() === ""} aria-busy={batch === "running" || undefined}>
              {t("models.test_enabled_models_n", { n: picked.length })}
            </button>
          </div>
          <p className="mguide">{t("models.the_catalog_is_for_discovery")}</p>
          {batch === "stopped" && <p className="mdiff" role="status">{t("models.no_further_checks_started_the")}</p>}
          <ModelChoice
            models={models}
            picked={picked}
            vision={probe?.vision ?? []}
            facts={facts}
            onCheck={checkModel}
            checkDisabled={busy || testing || baseUrl.trim() === ""}
            onToggle={toggle}
            onAdd={addModel}
          />
        </div>
      </div>

      {probe && (probe.ambiguous || probe.noProxy || searchSplit) && (
        <>
          {searchSplit && (
            <p className="acct-note">
              {t("models.this_address_answers_on_both", {
                on: searchOn.map((k) => t(KIND_LABEL[k] ?? k)).join("、"),
              })}
            </p>
          )}
          {probe.ambiguous && (
            <p className="acct-note">
              {t("common.both_access_methods_answer_with")}
            </p>
          )}
          {probe.noProxy && (
            <p className="acct-note">{t("models.the_proxy_could_not_reach")}</p>
          )}

        </>
      )}
      <div className="acts-bar">
        {err && errOnSave && (
          <div className="find" data-lvl="err" role="alert">
            <span className="t">{t("common.could_not_save")}</span>
            <span className="why">{err}</span>
          </div>
        )}
        <div className="acts addp-footer">
          <button className="act" data-action="provider.add" data-primary onClick={save} aria-describedby={nameBad ? "addp-name-rule" : undefined} disabled={busy || composing || testing || picked.length === 0 || name.trim() === "" || nameBad || kind === "" || baseUrl.trim() === "" || extraBad}>
            {t(busy ? "common.saving" : "common.add_source")}
          </button>
          <button className="act" onClick={onCancel} disabled={busy || testing}>{t("common.cancel")}</button>
        </div>
      </div>
    </fieldset>
  );
}

function digits(value: string): string { return value.replace(/\D/g, ""); }
function numeric(value: string): number { return value.trim() ? Number(value) : 0; }

// A second provider from the same vendor must not overwrite the first.
function uniqueName(base: string, taken: string[]): string {
  if (!taken.includes(base)) return base;
  for (let i = 2; i < 100; i++) {
    if (!taken.includes(`${base}-${i}`)) return `${base}-${i}`;
  }
  return base;
}
