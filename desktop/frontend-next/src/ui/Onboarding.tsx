import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/port";
import type { AgentPort, ProviderProbe, ProviderSetup } from "../port/port";
import { KIND_LABEL, nameFrom } from "./vendors";
import { Picker } from "./Menu";

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
const SHORTCUTS: { label: string; url: string; console?: string }[] = [
  { label: "DeepSeek", url: "https://api.deepseek.com", console: "https://platform.deepseek.com/api_keys" },
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
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<Failed | null>(setup.error ? { text: setup.error } : null);
  const [found, setFound] = useState<ProviderProbe | null>(null);
  const [model, setModel] = useState("");
  const first = useRef<HTMLInputElement>(null);

  useEffect(() => first.current?.focus(), []);

  // Where this vendor hands out keys, and — for the refusals that are settled
  // there rather than here — where this particular failure sends the reader.
  const vendor = SHORTCUTS.find((s) => s.label === pick);
  const fixAt = err?.code && FIX_AT_VENDOR.has(err.code) ? vendor?.console : undefined;
  const open = (at: string) => void port.openExternal(at).catch(() => setErr({ text: t("打不开浏览器，手动访问 {at}", { at }) }));

  const choose = (label: string, next: string) => {
    setPick(label);
    setUrl(next);
    setFound(null);
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
      setModel(probe.default || probe.models[0] || "");
    } catch (e) {
      setErr(failed(e));
    } finally {
      setBusy(false);
    }
  };

  const start = async () => {
    if (!found || !model || busy) return;
    setBusy(true);
    setErr(null);
    const name = nameFrom(url.trim());
    try {
      await port.saveProvider({
        name,
        kind: found.kind,
        baseUrl: url.trim(),
        apiKey: key.trim(),
        models: found.models,
        default: model,
        authHeader: found.authHeader,
        noProxy: found.noProxy,
        effort: "",
        vision: found.vision,
      });
      await port.setModel(`${name}/${model}`);
      onDone();
    } catch (e) {
      setErr(failed(e));
      setBusy(false);
    }
  };

  return (
    <div className="onb">
      <div className="onb-card">
        <h1 className="onb-t">{t("先连一个模型服务")}</h1>
        <p className="onb-s">
          {t("填地址和 key，剩下的问它自己 —— 协议、模型清单、能不能读图，都是探得到的。")}
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
            onChange={(e) => { setUrl(e.target.value); setFound(null); }}
          />
        </div>

        <div className="onb-field">
          <label htmlFor="onb-key">
            API key
            {setup.keyEnv && <code>{setup.keyEnv}</code>}
            {/* 这一步应用替不了：key 只能在服务商那边生成。不给入口，
                没有 key 的人就卡死在这个框上，而界面什么都没说。 */}
            {vendor?.console && (
              <button type="button" className="onb-getkey" onClick={() => open(vendor.console!)}>
                {t("还没有？去拿一个")}
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
            onChange={(e) => { setKey(e.target.value); setFound(null); }}
            onKeyDown={(e) => e.key === "Enter" && (found ? start() : connect())}
          />
          {err && (
            <div className="onb-err">
              {err.text}
              {fixAt && (
                <button type="button" className="onb-fix" onClick={() => open(fixAt)}>
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
              <span>{KIND_LABEL[found.kind] || found.kind}</span>
              <span className="k">{t("{n} 个模型 · key 存本机", { n: found.models.length })}</span>
            </div>
            {/* More than one wire answered, or one listing several may be
                driven with: either way the line above is a preference. */}
            {(found.ambiguous || (found.kinds?.length ?? 0) > 1) && (
              <p className="onb-why">
                {t("这个端点不止一种协议答应了，上面是偏好而不是事实。连上之后能在设置里换。")}
              </p>
            )}
            <div className="onb-field">
              <span className="lb">{t("先用哪个")}</span>
              {/* The app's own picker rather than a <select>: a gateway can
                  publish a hundred models, and this one grows a filter past
                  ten. A native dropdown would also paint its list in the
                  system's colours on top of this scene. */}
              <div className="onb-pick" data-busy={busy ? "" : undefined}>
                <Picker
                  label={model || t("选一个")}
                  items={found.models.map((m) => ({ value: m, label: m }))}
                  current={model}
                  onPick={setModel}
                  place="bottom"
                  title={t("选择默认模型")}
                />
              </div>
            </div>
          </div>
        )}

        <button
          className="btn onb-go"
          data-primary
          disabled={busy || !url.trim() || !key.trim() || (!!found && !model)}
          onClick={found ? start : connect}
        >
          {t(busy ? (found ? "正在保存…" : "正在连…") : found ? "开始" : "连上看看")}
        </button>

        <div className="onb-note">
          {t("key 存进本机配置，不上传任何第三方。模型、推理强度、执行设定都有默认值，随时能在输入框那排改。")}
        </div>
      </div>
    </div>
  );
}
