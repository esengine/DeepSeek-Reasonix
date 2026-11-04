import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import logoWordmark from "../assets/logo-wordmark.svg";
import { HttpError } from "../port/port";
import type { AgentPort, Protocol, ProviderProbe, ProviderSetup } from "../port/port";
import { KIND_LABEL, nameFrom } from "./vendors";
import { Picker } from "./Menu";
import { WindowControls, zoomOnTitleBar } from "./WindowControls";

interface Props {
  port: AgentPort;
  setup: ProviderSetup;
  onDone: () => void;
  onSkip?: () => void;
}

// Shortcuts fill the address, nothing more. A service missing from this row is
// not unsupported — what decides that is what the endpoint answers. 自建 is a
// first-class entry rather than a fallback, because a relay is what many
// people actually have.
//
// console is where a person goes to get the key this screen is asking for —
// the step nothing in the app can do for them. A vendor whose key page we
// cannot name links to its platform root instead: sending someone to a 404 is
// worse than sending them to a front door. These are addresses on someone
// else's site and will rot; a dead one is a bug report, not a silent failure.
const SHORTCUTS: { label: string; url: string; console?: string; protocolUrls?: Record<string, string> }[] = [
  {
    label: "DeepSeek",
    url: "https://api.deepseek.com",
    console: "https://platform.deepseek.com/api_keys",
    protocolUrls: {
      openai: "https://api.deepseek.com",
      responses: "https://api.deepseek.com",
      anthropic: "https://api.deepseek.com/anthropic",
    },
  },
  { label: "onboarding.siliconflow", url: "https://api.siliconflow.cn/v1", console: "https://cloud.siliconflow.cn" },
  { label: "onboarding.moonshot", url: "https://api.moonshot.cn/v1", console: "https://platform.moonshot.cn" },
  { label: "onboarding.zhipu", url: "https://open.bigmodel.cn/api/paas/v4", console: "https://bigmodel.cn/usercenter/proj-mgmt/apikeys" },
  { label: "MiMo", url: "https://api.xiaomimimo.com/v1", console: "https://platform.xiaomimimo.com" },
  { label: "LongCat", url: "https://api.longcat.chat/openai", console: "https://longcat.chat/platform" },
  { label: "onboarding.relay_self_hosted", url: "" },
];

// The two failures nobody fixes in this window: the key is refused, or the
// account behind it is out of credit. Both are settled on the vendor's site,
// so the refusal carries a way to get there instead of only a sentence.
const FIX_AT_VENDOR = new Set(["provider.probe.unauthorized", "provider.probe.payment_required"]);

const KIND_DETAIL: Record<string, string> = {
  openai: "Chat Completions",
  responses: "Responses API",
  anthropic: "Messages API",
};

// Failed is a refusal kept whole: the sentence to read, and the code that says
// whether reading it is enough.
interface Failed {
  text: string;
  code?: string;
}

function failed(e: unknown): Failed {
  return { text: reason(e), code: e instanceof HttpError ? e.reason?.code : undefined };
}

// The kernel blocks on one thing at first launch: a usable key. Which service
// it belongs to is the user's business, so this asks where and with what, then
// lets the endpoint answer the rest — protocol, model list, image support are
// all knowable by asking, and a person cannot be expected to know them.
export function Onboarding({ port, setup, onDone, onSkip }: Props) {
  const [pick, setPick] = useState(SHORTCUTS[0].label);
  const [url, setUrl] = useState(SHORTCUTS[0].url);
  const [completed, setCompleted] = useState("");
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<Failed | null>(setup.error ? { text: setup.error } : null);
  const [found, setFound] = useState<ProviderProbe | null>(null);
  const [catalog, setCatalog] = useState<Protocol[]>([]);
  const [kind, setKind] = useState("");
  const [model, setModel] = useState("");
  const first = useRef<HTMLInputElement>(null);

  useEffect(() => {
    first.current?.focus();
    void port.protocols().then(setCatalog).catch(() => setCatalog([]));
  }, [port]);

  // Where this vendor hands out keys, and — for the refusals that are settled
  // there rather than here — where this particular failure sends the reader.
  const vendor = SHORTCUTS.find((s) => s.label === pick);
  const fixAt = err?.code && FIX_AT_VENDOR.has(err.code) ? vendor?.console : undefined;
  const open = (at: string) => void port.openExternal(at).catch(() => setErr({ text: t("common.could_not_open_a_browser", { at }) }));

  const choose = (label: string, next: string) => {
    setPick(label);
    setUrl(next);
    setFound(null);
    setKind("");
    setErr(null);
  };

  // The endpoint's own words are the answer: a 401, a wrong path and "no chat
  // models" send the user to three different fixes, so none of them collapses
  // into "connection failed".
  const connect = async () => {
    const at = url.trim();
    const k = key.trim();
    if (!at || !k || busy) return;
    setBusy(true);
    setErr(null);
    try {
      const probe = await port.probeProvider(at, k);
      setFound(probe);
      const changed = !!probe.baseUrl && probe.baseUrl !== at;
      if (changed) setUrl(probe.baseUrl!);
      setCompleted(changed ? probe.baseUrl! : "");
      setKind(probe.kind);
      setModel(probe.default || probe.models[0] || "");
    } catch (e) {
      setErr(failed(e));
    } finally {
      setBusy(false);
    }
  };

  const start = async () => {
    if (!found || !kind || !model || busy) return;
    setBusy(true);
    setErr(null);
    const name = nameFrom(url.trim());
    try {
      await port.saveProvider({
        name,
        kind,
        baseUrl: url.trim(),
        apiKey: key.trim(),
        models: found.models,
        default: model,
        authHeader: found.authHeader,
        noProxy: found.noProxy,
        effort: "",
        vision: found.vision,
        replace: true,
      });
      await port.setModel(`${name}/${model}`);
      await port.setDefaultModel(`${name}/${model}`);
      onDone();
    } catch (e) {
      setErr(failed(e));
      setBusy(false);
    }
  };

  const kinds = found ? (found.kinds?.length ? found.kinds : [found.kind]) : [];
  const protocols = [...new Set([...catalog.map((entry) => entry.kind), ...kinds])];
  const selectProtocol = (value: string) => {
    setKind(value);
    const mapped = vendor?.protocolUrls?.[value];
    const known = Object.values(vendor?.protocolUrls ?? {});
    if (mapped && known.includes(url.trim().replace(/\/$/, ""))) setUrl(mapped);
  };
  const modelItems = found?.models.map((name) => {
    const detail = [
      name === found.default ? t("common.default") : "",
      found.vision.includes(name) ? t("common.reads_images") : "",
    ].filter(Boolean).join(" · ");
    return { value: name, label: name, desc: detail || undefined };
  }) ?? [];

  return (
    <div className="onb-stage">
      <header className="onb-brandbar" onDoubleClick={zoomOnTitleBar}>
        <span className="onb-lockup">
          <img className="onb-wordmark" src={logoWordmark} alt="Reasonix Studio" draggable={false} />
          <small className="onb-brandsub" aria-hidden="true">studio</small>
        </span>
        <WindowControls />
      </header>
      <div className="onb-shell">
        <aside className="onb-progress" aria-label={t("onboarding.set_up_reasonix")}>
          <span className="onb-progress-kicker">{t("onboarding.set_up_reasonix")}</span>
          <ol>
            <li data-active=""><i>1</i><span><b>{t("onboarding.connect_a_model_service")}</b><small>{t("onboarding.address_protocol_and_credentials")}</small></span></li>
            <li data-active={found ? "" : undefined}><i>2</i><span><b>{t("onboarding.choose_the_default_model")}</b><small>{t("onboarding.confirm_the_model_and_its")}</small></span></li>
            <li><i>3</i><span><b>{t("onboarding.open_a_workspace")}</b><small>{t("onboarding.start_your_first_task")}</small></span></li>
          </ol>
          <p>{t("onboarding.you_can_change_any_of")}</p>
        </aside>
        <main className="onb">
          <div className="onb-card">
            <span className="onb-eyebrow">MODEL CONNECTION</span>
        <h1 className="onb-t">{t("onboarding.connect_a_model_service_first")}</h1>
        <p className="onb-s">
          {t("onboarding.give_it_an_address_and")}
        </p>

        <div className="onb-chips" role="group" aria-label={t("onboarding.common_services")}>
          {SHORTCUTS.map((s) => (
            <button
              key={s.label}
              className="onb-chip"
              data-custom={s.url === "" ? "" : undefined}
              aria-pressed={pick === s.label}
              disabled={busy}
              onClick={() => choose(s.label, s.url)}
            >
              {t(s.label)}
            </button>
          ))}
        </div>

        <div className="onb-field">
          <label htmlFor="onb-url">{t("onboarding.service_address")}</label>
          <input
            id="onb-url"
            ref={first}
            value={url}
            spellCheck={false}
            autoComplete="off"
            placeholder={t("onboarding.https_your_host_v1")}
            disabled={busy}
            onChange={(e) => { setUrl(e.target.value); setFound(null); setKind(""); }}
          />
        </div>

        <div className="onb-field">
          <label htmlFor="onb-key">
            API key
            {setup.keyEnv && <code>{setup.keyEnv}</code>}
            {/* 这一步应用替不了：key 只能在服务商那边生成。不给入口，
                没有 key 的人就卡死在这个框上，而界面什么都没说。 */}
            {vendor?.console && (
              <button type="button" className="onb-getkey" data-action="external.open" onClick={() => open(vendor.console!)}>
                {t("onboarding.no_key_yet_get_one")}
              </button>
            )}
          </label>
          <input
            id="onb-key"
              data-action-keydown="onboarding.connect"
            type="password"
            value={key}
            spellCheck={false}
            autoComplete="off"
            placeholder="sk-…"
            disabled={busy}
            onChange={(e) => { setKey(e.target.value); setFound(null); setKind(""); }}
            onKeyDown={(e) => e.key === "Enter" && (found ? start() : connect())}
          />
          {err && (
            <div className="onb-err">
              {err.text}
              {fixAt && (
                <button type="button" className="onb-fix" data-action="external.open" onClick={() => open(fixAt)}>
                  {t("onboarding.open_the_name_console", { name: t(pick) })}
                </button>
              )}
            </div>
          )}
        </div>

        {found && (
          <div className="onb-found">
            <div className="onb-found-hd">
              <span className="tick">✓</span>
              <span>{t("common.connected")}</span>
              <span className="k">{t("onboarding.n_models_key_stays_on", { n: found.models.length })}</span>
            </div>
            {completed !== "" && completed === url.trim() && (
              <p className="probe-ok" role="status">{t("common.address_completed_to_url", { url: completed })}</p>
            )}
            <div className="onb-field">
              <span className="lb">{t("common.protocol")}</span>
              <div className="onb-protocols" role="radiogroup" aria-label={t("common.protocol")}>
                {protocols.map((value) => (
                  <button
                    key={value}
                    type="button"
                    role="radio"
                    data-action="onboarding.protocol"
                    data-value={value}
                    aria-checked={kind === value}
                    disabled={busy}
                    onClick={() => selectProtocol(value)}
                  >
                    <i aria-hidden="true" />
                    <span>
                      <b>{t(KIND_LABEL[value] || value)}</b>
                      <small>{KIND_DETAIL[value] || value}</small>
                    </span>
                  </button>
                ))}
              </div>
              <p className="onb-protocol-note">
                {t("onboarding.a_model_catalog_confirms_the")}
              </p>
            </div>
            {(found.ambiguous || (found.kinds?.length ?? 0) > 1) && (
              <p className="onb-why">
                {t("common.both_access_methods_answer_with")}
              </p>
            )}
            <div className="onb-field">
              <span className="lb">{t("onboarding.choose_the_default_model")}</span>
              {/* The app's own picker rather than a <select>: a gateway can
                  publish a hundred models, and this one grows a filter past
                  ten. A native dropdown would also paint its list in the
                  system's colours on top of this scene. */}
              <div className="onb-pick" data-busy={busy ? "" : undefined}>
                <Picker
                  label={(
                    <span className="onb-model-trigger">
                      <i aria-hidden="true">M</i>
                      <span>
                        <b>{model || t("common.pick_one")}</b>
                        <small>{t(KIND_LABEL[kind] || kind)}</small>
                      </span>
                    </span>
                  )}
                  items={modelItems}
                  current={model}
                  onPick={setModel}
                  place="bottom"
                  menuClassName="onb-model-menu"
                  title={t("onboarding.choose_the_default_model")}
                />
              </div>
            </div>
          </div>
        )}

        <button
          className="btn onb-go"
          data-primary
          disabled={busy || !url.trim() || !key.trim() || (!!found && (!kind || !model))}
          onClick={found ? start : connect}
        >
          {t(busy ? (found ? "common.saving_2" : "onboarding.connecting") : found ? "common.start" : "onboarding.connect")}
        </button>
        {onSkip && (
          <button type="button" className="onb-skip" data-action="onboarding.skip" disabled={busy} onClick={onSkip}>
            {t("onboarding.not_now")}
          </button>
        )}

        <div className="onb-note">
          {t("onboarding.the_key_goes_into_this")}
        </div>
          </div>
        </main>
      </div>
      <footer className="onb-foot">{t("onboarding.local_first_credentials_stay_in")}</footer>
    </div>
  );
}
