import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import logoWordmark from "../assets/logo-wordmark.svg";
import { HttpError } from "../port/port";
import type { AgentPort, Protocol, ProviderProbe, ProviderSetup } from "../port/port";
import { KIND_LABEL, nameFrom } from "./vendors";
import { hasVendorMark, VendorMark } from "./vendormarks";
import { OnboardingDemo } from "./OnboardingDemo";
import { RMark } from "./RMark";
import { OnboardingProject, type ProjectChoice } from "./OnboardingProject";
import { startRise } from "./firstRise";
import { Picker } from "./Menu";
import { WindowControls, zoomOnTitleBar } from "./WindowControls";

interface Props {
  port: AgentPort;
  setup: ProviderSetup;
  onDone: () => void;
  onSkip?: () => void;
  // Which folder the work happens in. Without it setup ends at the model.
  project?: ProjectChoice;
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
const SHORTCUTS: { label: string; hint: string; url: string; mark?: string; console?: string; protocolUrls?: Record<string, string> }[] = [
  {
    label: "DeepSeek",
    mark: "deepseek",
    hint: "onboarding.deepseek_hint",
    url: "https://api.deepseek.com",
    console: "https://platform.deepseek.com/api_keys",
    protocolUrls: {
      openai: "https://api.deepseek.com",
      responses: "https://api.deepseek.com",
      anthropic: "https://api.deepseek.com/anthropic",
    },
  },
  { label: "onboarding.siliconflow", hint: "onboarding.siliconflow_hint", url: "https://api.siliconflow.cn/v1", console: "https://cloud.siliconflow.cn" },
  { mark: "kimi", label: "onboarding.moonshot", hint: "onboarding.moonshot_hint", url: "https://api.moonshot.cn/v1", console: "https://platform.moonshot.cn" },
  { mark: "zhipu", label: "onboarding.zhipu", hint: "onboarding.zhipu_hint", url: "https://open.bigmodel.cn/api/paas/v4", console: "https://bigmodel.cn/usercenter/proj-mgmt/apikeys" },
  { mark: "mimo", label: "MiMo", hint: "onboarding.mimo_hint", url: "https://api.xiaomimimo.com/v1", console: "https://platform.xiaomimimo.com" },
  { mark: "longcat", label: "LongCat", hint: "onboarding.longcat_hint", url: "https://api.longcat.chat/openai", console: "https://longcat.chat/platform" },
  { label: "onboarding.relay_self_hosted", hint: "onboarding.relay_hint", url: "" },
];

// A listed service at its listed address answers in a known way, so only an
// address the person typed is ever asked how it is spoken to.
const MODEL_LIST_MAX = 8;

type Step = "welcome" | "connect" | "model" | "project";

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
export function Onboarding({ port, setup, onDone, onSkip, project }: Props) {
  const [pick, setPick] = useState(SHORTCUTS[0].label);
  const [url, setUrl] = useState(SHORTCUTS[0].url);
  const [completed, setCompleted] = useState("");
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [connected, setConnected] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const [err, setErr] = useState<Failed | null>(setup.error ? { text: setup.error } : null);
  const [found, setFound] = useState<ProviderProbe | null>(null);
  const [catalog, setCatalog] = useState<Protocol[]>([]);
  const [kind, setKind] = useState("");
  const [model, setModel] = useState("");
  const first = useRef<HTMLInputElement>(null);
  const [step, setStep] = useState<Step>("welcome");
  const [back, setBack] = useState(false);
  const go = (next: Step, backwards = false) => { setBack(backwards); setStep(next); };
  // The card shrinks and fades before the conversation takes over, and the
  // composer rises into the place it left.
  const leave = (then: () => void) => {
    setLeaving(true);
    startRise();
    window.setTimeout(then, matchMedia?.("(prefers-reduced-motion: reduce)").matches ? 0 : 240);
  };

  useEffect(() => {
    void port.protocols().then(setCatalog).catch(() => setCatalog([]));
  }, [port]);
  useEffect(() => { if (step === "connect") first.current?.focus(); }, [step, pick]);

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
      // The button says it worked, and the banner with it, before the page moves on.
      setConnected(true);
      await new Promise((done) => setTimeout(done, 750));
      go("model");
    } catch (e) {
      setErr(failed(e));
    } finally {
      setBusy(false);
      setConnected(false);
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
      if (project) {
        setBusy(false);
        go("project");
      } else leave(onDone);
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
  const custom = vendor?.url === "";
  const asks = custom && !!found && (found.ambiguous || (found.kinds?.length ?? 0) > 1);
  const detail = (name: string) => [
    name === found?.default ? t("common.default") : "",
    found?.vision.includes(name) ? t("common.reads_images") : "",
  ].filter(Boolean).join(" · ");
  const modelItems = found?.models.map((name) => ({ value: name, label: name, desc: detail(name) || undefined })) ?? [];
  const steps: [Step | "project", string][] = [["connect", "onboarding.connect_a_model_service"], ["model", "onboarding.choose_the_default_model"], ["project", "onboarding.open_a_workspace"]];
  const at = steps.findIndex(([id]) => id === step);

  return (
    <div className="onb-stage" data-leaving={leaving ? "" : undefined}>
      <header className="onb-brandbar" onDoubleClick={zoomOnTitleBar}>
        <span className="onb-lockup">
          <img className="onb-wordmark" src={logoWordmark} alt="Reasonix Studio" draggable={false} />
          <small className="onb-brandsub" aria-hidden="true">studio</small>
        </span>
        <WindowControls />
      </header>
      <div className="onb-shell">
      <main className="onb onb-flow">
        <div className="onb-card" key={step} data-step={step} data-back={back ? "" : undefined}>
          {step !== "welcome" && (
            <div className="onb-steps">
              <ol aria-label={t("onboarding.set_up_reasonix")}>
                {steps.map(([id, label], i) => (
                  <li key={id} data-state={i < at ? "done" : i === at ? "current" : "todo"} aria-current={i === at ? "step" : undefined}>
                    <i>{i < at ? "✓" : i + 1}</i><span>{t(label)}</span>
                  </li>
                ))}
              </ol>
              {onSkip && (
                <button type="button" className="onb-skip" data-action="onboarding.skip" disabled={busy} onClick={onSkip}>
                  {t("onboarding.not_now")}
                </button>
              )}
            </div>
          )}

          {step === "welcome" && (
            <>
            <div className="onb-welcome">
              <img className="onb-welcome-mark" src={logoWordmark} alt="" aria-hidden="true" draggable={false} />
              <h1 className="onb-t">{t("onboarding.hand_it_a_task")}</h1>
              <p className="onb-s">{t("onboarding.what_it_does_and_shows")}</p>
              <div className="onb-acts">
                <button className="btn onb-go" data-primary data-action="onboarding.begin" onClick={() => go("connect")}>{t("onboarding.set_up_reasonix")}</button>
                {onSkip && <button type="button" className="onb-skip" data-action="onboarding.skip" onClick={onSkip}>{t("onboarding.not_now")}</button>}
              </div>
              <p className="onb-note">{t("onboarding.about_a_minute")}</p>
            </div>
            <OnboardingDemo />
            </>
          )}

          {step === "connect" && (
            <>
              <h1 className="onb-t">{t("onboarding.connect_one_model_service")}</h1>
              <p className="onb-s">{t("onboarding.pick_one_and_fill_the_key")}</p>
              <div className="onb-services" role="radiogroup" aria-label={t("onboarding.common_services")}>
                {SHORTCUTS.map((s) => (
                  <button key={s.label} type="button" role="radio" className="onb-service" data-action="onboarding.service" data-value={s.label}
                    aria-checked={pick === s.label} disabled={busy} onClick={() => choose(s.label, s.url)}>
                    <b>{hasVendorMark(s.mark) && <VendorMark slug={s.mark} size={16} />}{t(s.label)}</b><small>{t(s.hint)}</small>
                  </button>
                ))}
              </div>
              {custom ? (
                <div className="onb-field">
                  <label htmlFor="onb-url">{t("onboarding.service_address")}</label>
                  <input id="onb-url" value={url} spellCheck={false} autoComplete="off" placeholder={t("onboarding.https_your_host_v1")} disabled={busy}
                    onChange={(e) => { setUrl(e.target.value); setFound(null); setKind(""); }} />
                </div>
              ) : null}
              <div className="onb-field">
                <label htmlFor="onb-key">
                  API key
                  {setup.keyEnv && <code>{setup.keyEnv}</code>}
                  {vendor?.console && (
                    <button type="button" className="onb-getkey" data-action="external.open" onClick={() => open(vendor.console!)}>
                      {t("onboarding.no_key_yet_get_it_from", { name: t(pick) })}
                    </button>
                  )}
                </label>
                <input id="onb-key" ref={first} data-action-keydown="onboarding.connect" type="password" value={key} spellCheck={false} autoComplete="off"
                  placeholder="sk-…" disabled={busy} aria-invalid={err ? true : undefined}
                  onChange={(e) => { setKey(e.target.value); setFound(null); setKind(""); }}
                  onKeyDown={(e) => e.key === "Enter" && connect()} />
                {err && (
                  <div className="onb-err" role="alert">
                    {err.text}
                    {fixAt && (
                      <button type="button" className="onb-fix" data-action="external.open" onClick={() => open(fixAt)}>
                        {t("onboarding.open_the_name_console", { name: t(pick) })}
                      </button>
                    )}
                  </div>
                )}
              </div>
              {connected && found && (
                <div className="onb-found" role="status" data-enter="">
                  <span className="tick">✓</span>
                  <span>{t("onboarding.connected_to_name_n_models", { name: t(pick), n: found.models.length })}</span>
                </div>
              )}
              <div className="onb-acts">
                <button className="btn onb-go" data-primary data-state={connected ? "ok" : busy ? "busy" : undefined} disabled={busy || !url.trim() || !key.trim()}
                  aria-label={busy ? t("onboarding.checking_the_key") : undefined} onClick={() => void connect()}>
                  {connected
                    ? <svg className="onb-check" viewBox="0 0 16 16" aria-hidden="true"><path pathLength={100} d="M3.5 8.5l3 3 6-7" /></svg>
                    : busy ? <RMark className="rmark onb-spin" /> : t("onboarding.connect")}
                </button>
                <button type="button" className="onb-skip" data-action="onboarding.back" disabled={busy} onClick={() => go("welcome", true)}>{t("common.back")}</button>
              </div>
              {!custom && <p className="onb-note">{t("onboarding.address_filled_for_name", { name: t(pick), host: url.replace(/^https?:\/\//, "").replace(/\/.*$/, "") })}</p>}
            </>
          )}

          {step === "project" && project && (
            <OnboardingProject project={project} onDone={() => leave(onDone)} onSkip={() => leave(onDone)} />
          )}

          {step === "model" && found && (
            <>
              <div className="onb-found" role="status">
                <span className="tick">✓</span>
                <span>{t("onboarding.connected_to_name_n_models", { name: t(pick), n: found.models.length })}</span>
              </div>
              {completed !== "" && completed === url.trim() && <p className="probe-ok" role="status">{t("common.address_completed_to_url", { url: completed })}</p>}
              {asks && (
                <div className="onb-field">
                  <span className="lb">{t("onboarding.how_it_is_spoken_to")}</span>
                  <p className="onb-why">{t("common.both_access_methods_answer_with")}</p>
                  <div className="onb-protocols" role="radiogroup" aria-label={t("common.protocol")}>
                    {protocols.map((value) => (
                      <button key={value} type="button" role="radio" data-action="onboarding.protocol" data-value={value} aria-checked={kind === value} disabled={busy}
                        onClick={() => selectProtocol(value)}>
                        <i aria-hidden="true" />
                        <span><b>{t(KIND_LABEL[value] || value)}</b><small>{KIND_DETAIL[value] || value}</small></span>
                      </button>
                    ))}
                  </div>
                </div>
              )}
              <div className="onb-field">
                <span className="lb">{t("onboarding.pick_a_default_model")}</span>
                {found.models.length <= MODEL_LIST_MAX ? (
                  <div className="onb-models" role="radiogroup" aria-label={t("onboarding.pick_a_default_model")}>
                    {found.models.map((name) => (
                      <button key={name} type="button" role="radio" className="onb-model" data-action="onboarding.model" data-value={name}
                        aria-checked={model === name} disabled={busy} onClick={() => setModel(name)}>
                        <i aria-hidden="true" /><b>{name}</b>{detail(name) && <small>{detail(name)}</small>}
                      </button>
                    ))}
                  </div>
                ) : (
                  <div className="onb-pick" data-busy={busy ? "" : undefined}>
                    <Picker
                      label={(
                        <span className="onb-model-trigger">
                          <i aria-hidden="true">M</i>
                          <span><b>{model || t("common.pick_one")}</b><small>{t(KIND_LABEL[kind] || kind)}</small></span>
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
                )}
                <p className="onb-note">{t("onboarding.each_conversation_can_switch")}</p>
              </div>
              {err && <div className="onb-err" role="alert">{err.text}</div>}
              <div className="onb-acts">
                <button className="btn onb-go" data-primary data-action="onboarding.start" disabled={busy || !kind || !model} onClick={() => void start()}>
                  {t(busy ? "common.saving_2" : "onboarding.continue")}
                </button>
                <button type="button" className="onb-skip" data-action="onboarding.back" disabled={busy} onClick={() => go("connect", true)}>{t("onboarding.previous_step")}</button>
              </div>
            </>
          )}
        </div>
      </main>
      </div>
      <footer className="onb-foot">{t("onboarding.local_first_credentials_stay_in")}</footer>
    </div>
  );
}
