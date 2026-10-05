import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/port";
import type { AgentPort, Protocol, ProviderProbe, ProviderSetup } from "../port/port";
import { KIND_LABEL, nameFrom } from "./vendors";
import { Picker } from "./Menu";
import { WindowControls, zoomOnTitleBar } from "./WindowControls";

interface Props {
  port: AgentPort;
  setup: ProviderSetup;
  onDone: () => void;
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
  { label: "硅基流动", url: "https://api.siliconflow.cn/v1", console: "https://cloud.siliconflow.cn" },
  { label: "月之暗面", url: "https://api.moonshot.cn/v1", console: "https://platform.moonshot.cn" },
  { label: "智谱", url: "https://open.bigmodel.cn/api/paas/v4", console: "https://bigmodel.cn/usercenter/proj-mgmt/apikeys" },
  { label: "MiMo", url: "https://api.xiaomimimo.com/v1", console: "https://platform.xiaomimimo.com" },
  { label: "LongCat", url: "https://api.longcat.chat/openai", console: "https://longcat.chat/platform" },
  { label: "中转站 / 自建", url: "" },
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
export function Onboarding({ port, setup, onDone }: Props) {
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
  const open = (at: string) => void port.openExternal(at).catch(() => setErr({ text: t("无法打开浏览器，请手动访问 {at}", { at }) }));

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
      await port.setModel(`${name}/${model}`, true);
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
      name === found.default ? t("默认") : "",
      found.vision.includes(name) ? t("读图") : "",
    ].filter(Boolean).join(" · ");
    return { value: name, label: name, desc: detail || undefined };
  }) ?? [];

  return (
    <div className="onb-stage">
      <header className="onb-brandbar" onDoubleClick={zoomOnTitleBar}>
        <span className="onb-brandmark" aria-hidden="true">R</span>
        <span className="onb-brandname"><b>reasonix</b><small>studio</small></span>
        <WindowControls />
      </header>
      <div className="onb-shell">
        <aside className="onb-progress" aria-label={t("开始设置")}>
          <span className="onb-progress-kicker">{t("开始设置")}</span>
          <ol>
            <li data-active=""><i>1</i><span><b>{t("连接模型服务")}</b><small>{t("地址、协议与凭据")}</small></span></li>
            <li data-active={found ? "" : undefined}><i>2</i><span><b>{t("选择默认模型")}</b><small>{t("确认模型与能力")}</small></span></li>
            <li><i>3</i><span><b>{t("打开工作区")}</b><small>{t("开始第一个任务")}</small></span></li>
          </ol>
          <p>{t("之后可以在设置中随时修改，不会锁定当前选择。")}</p>
        </aside>
        <main className="onb">
          <div className="onb-card">
            <span className="onb-eyebrow">MODEL CONNECTION</span>
        <h1 className="onb-t">{t("首先连接模型服务")}</h1>
        <p className="onb-s">
          {t("填写地址与 key，系统会探测可用协议、模型清单与读图能力；无法自动区分的协议由你确认。")}
        </p>

        <div className="onb-chips" role="group" aria-label={t("常用服务")}>
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
          <label htmlFor="onb-url">{t("服务地址")}</label>
          <input
            id="onb-url"
            ref={first}
            value={url}
            spellCheck={false}
            autoComplete="off"
            placeholder={t("https://你的地址/v1")}
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
                {t("尚无 key？前往获取")}
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
                  {t("打开 {name} 控制台", { name: t(pick) })}
                </button>
              )}
            </div>
          )}
        </div>

        {found && (
          <div className="onb-found">
            <div className="onb-found-hd">
              <span className="tick">✓</span>
              <span>{t("连上了")}</span>
              <span className="k">{t("{n} 个模型 · key 保存在本机", { n: found.models.length })}</span>
            </div>
            {completed !== "" && completed === url.trim() && (
              <p className="probe-ok" role="status">{t("接口地址已补全为 {url}", { url: completed })}</p>
            )}
            <div className="onb-field">
              <span className="lb">{t("接入方式")}</span>
              <div className="onb-protocols" role="radiogroup" aria-label={t("接入方式")}>
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
                {t("模型目录只能确认账号与模型，不能排除其他聊天协议。请选择服务商实际支持的协议；已知服务的专用地址会自动切换。")}
              </p>
            </div>
            {(found.ambiguous || (found.kinds?.length ?? 0) > 1) && (
              <p className="onb-why">
                {t("两种接入方式都能返回模型列表，仅凭列表无法区分；两者的聊天入口路径通常不同，选错会导致聊天报错。如需同时使用，请再添加一次并选择另一种。")}
              </p>
            )}
            <div className="onb-field">
              <span className="lb">{t("选择默认模型")}</span>
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
                        <b>{model || t("请选择一项")}</b>
                        <small>{t(KIND_LABEL[kind] || kind)}</small>
                      </span>
                    </span>
                  )}
                  items={modelItems}
                  current={model}
                  onPick={setModel}
                  place="bottom"
                  menuClassName="onb-model-menu"
                  title={t("选择默认模型")}
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
          {t(busy ? (found ? "正在保存…" : "正在连接…") : found ? "开始" : "连接并继续")}
        </button>

        <div className="onb-note">
          {t("key 保存在本机配置中，不会上传至任何第三方。模型、推理强度与执行设定均有默认值，可随时在输入框上方调整。")}
        </div>
          </div>
        </main>
      </div>
      <footer className="onb-foot">{t("本地优先 · 凭据由系统安全存储 · 连接前不会发送请求")}</footer>
    </div>
  );
}
