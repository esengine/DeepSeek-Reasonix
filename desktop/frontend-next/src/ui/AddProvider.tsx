import { useEffect, useState } from "react";
import { t } from "../i18n";
import type { Protocol, ProviderEntry, ProviderProbe } from "../port/port";
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

  return (
    <fieldset className="addp" disabled={busy && errOnSave}>
      <div className="addp-head">
        <div>
          <span className="step">{t("自定义来源")}</span>
          <strong>{t("添加模型来源")}</strong>
          <p>{t("按服务文档填写。连接检测只帮助读取模型，不会替你决定协议。")}</p>
        </div>
        <button className="addp-close" onClick={onCancel} disabled={busy || testing} aria-label={t("取消")}>×</button>
      </div>
      <div className="fields">
        <div className="grow name-cell">
        <label>
          <span>{t("来源名称")}</span>
          <input
            aria-label={t("来源名称")}
            data-action="provider.draft" data-value="name"
            value={name}
            placeholder={t("例如：公司中转站")}
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
          {nameErr !== "" ? nameErr : t("名称只能用字母、数字、点、连字符和下划线，以字母或数字开头，最长 64 个字符")}
        </p>
        </div>
        <label className="grow">
          <span>{t("接口协议")}</span>
          <select aria-label={t("接口协议")} data-action="provider.draft" data-value="protocol" value={kind} onChange={(e) => {
            setKind(e.target.value);
            setFacts(clearModelCheckFacts);
          }} disabled={busy || testing}>
            {choices.map((k) => <option key={k} value={k}>{t(KIND_LABEL[k] ?? k)}</option>)}
          </select>
          <i className="tip">{t("以服务商文档为准；模型列表无法可靠判断聊天协议。")}</i>
        </label>
        <label className="grow">
          <span>{t("接口地址")}</span>
          <input
            aria-label={t("接口地址")}
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
          {wire?.answers === "decision" && <i className="tip">{t("填写服务的基地址，不要带 /v1/systemone；Reasonix 会自己在后面追加。本地的 Ollama 之类地址不适用这个协议。")}</i>}
        </label>
        <label className="grow full">
          <span>API Key{t(sibling ? "（留空就用现有那个来源的 key）" : "")}</span>
          <input type="password" data-action="provider.draft" data-value="credential" value={apiKey} placeholder={sibling ? "········" : ""}
            onChange={(e) => {
              setApiKey(e.target.value);
              setFacts(clearModelCheckFacts);
            }} disabled={busy || testing} spellCheck={false} />
        </label>
        <p className="addp-privacy">{t("API Key 仅保存在运行内核的这台机器上。")}</p>
        {sibling && (
          <p className="acct-note">
            {t("这个地址上已经有「{name}」了。留空 key", { name: vendorLabel(hostOf(sibling.baseUrl)) })}
            {t("这会为该来源添加另一种接入方式，两者合并为同一个来源，通过「接入方式」切换；若填写新的 key，则视为本机的另一个账号，用量分别计算。")}
          </p>
        )}
      </div>

      <div className="addp-section">
        <div className="addp-section-head">
          <div><strong>{t("模型限制")}</strong><span>{t("作为该来源中模型的默认值")}</span></div>
        </div>
        <div className="fields token-limits">
          <label className="grow">
            <span>{t("上下文窗口")}</span>
            <span className="unit-field"><input aria-label={t("上下文窗口")} data-action="provider.draft" data-value="context-window" inputMode="numeric" value={win} placeholder="128000" onChange={(e) => setWin(digits(e.target.value))} /><i>tokens</i></span>
            <i className="tip">{t("输入、工具结果与输出共享的总容量。")}</i>
          </label>
          <label className="grow">
            <span>{t("最大输出")}</span>
            <span className="unit-field"><input aria-label={t("最大输出")} data-action="provider.draft" data-value="max-output" inputMode="numeric" value={maxOut} placeholder={t("自动")} onChange={(e) => setMaxOut(digits(e.target.value))} /><i>tokens</i></span>
            <i className="tip">{t("单轮回复上限；留空由内核与模型共同决定。")}</i>
          </label>
        </div>
      </div>

      <details className="addp-options">
        <summary>
          <span className="tx">
            <strong>{t("高级连接选项")}</strong>
            <small>{t("思考控制、代理与中转站自定义参数")}</small>
          </span>
          <span className="summary-value">{t("可选")}</span>
        </summary>
        <div className="addp-options-body">
          <div className="setting-line">
            <span className="setting-copy">
              <strong>{t("发送思考控制")}</strong>
              <small>{t(protocolCanThink ? "允许当前协议发送思考控制字段；模型是否真的思考仍由模型决定。" : "当前协议没有思考控制字段，切换模型时仍可在推理强度菜单查看支持情况。")}</small>
            </span>
            <button
              type="button"
              className="switch-control"
              data-action="provider.draft" data-value="thinking"
              role="switch"
              aria-label={t("发送思考控制")}
              aria-checked={protocolCanThink && thinkingOn}
              disabled={!protocolCanThink || busy || testing}
              onClick={() => setThinkingOn((v) => !v)}
            ><span /></button>
          </div>
          {protocolCanThink && thinkingOn && (
            <label className="advanced-field">
              <span>{t("思考协议")}</span>
              <select value={thinkingProtocol} onChange={(e) => setThinkingProtocol(e.target.value)}>
                {THINKING.filter(([value]) => value !== "none").map(([value, label]) => (
                  <option key={value} value={value}>{t(label)}</option>
                ))}
              </select>
              <i>{t("使用「自动」时由模型和接口协议决定；只有中转站文档明确要求时才手动指定。")}</i>
              <EffortShape field={effortField} />
            </label>
          )}
          <div className="setting-line">
            <span className="setting-copy">
              <strong>{t("绕过系统代理")}</strong>
              <small>{t("仅当该地址通过系统代理无法连接、直连可用时开启。")}</small>
            </span>
            <button type="button" className="switch-control" data-action="provider.draft" data-value="no-proxy" role="switch" aria-label={t("绕过系统代理")} aria-checked={noProxy || (probe?.noProxy ?? false)}
              disabled={busy || testing || probe?.noProxy === true} onClick={() => {
                setNoProxy((v) => !v);
                setFacts(clearModelCheckFacts);
              }}><span /></button>
          </div>
          <label className="advanced-field">
            <span>{t("额外请求头")}</span>
            <textarea rows={3} value={heads} spellCheck={false}
              placeholder={"HTTP-Referer: https://example.com\nX-Title: Reasonix"}
              onChange={(e) => setHeads(e.target.value)} />
            <i>{t("每行一个「名称: 值」。API Key 仍填写在上方。")}</i>
          </label>
          <label className="advanced-field">
            <span>{t("额外请求体")}</span>
            <textarea rows={4} value={extra} spellCheck={false} aria-invalid={extraBad || undefined}
              placeholder={'{\n  "temperature": 0.7\n}'} onChange={(e) => setExtra(e.target.value)} />
            <i>{t("仅用于服务商要求的可选字段；model、messages、tools 和 stream 仍由内核管理。")}</i>
            {extraBad && <em className="field-error">{t("请输入合法的 JSON 对象。")}</em>}
          </label>
        </div>
      </details>

      <div className="addp-section">
        <div className="addp-section-head">
          <div>
            <strong>{t("模型目录")}</strong>
            <span>{t(protocolCanProbe ? "至少手动添加一个模型 ID，也可尝试从接口读取" : "这个协议没有模型列表接口，直接填写服务商给出的模型 ID")}</span>
          </div>
          {protocolCanProbe && (
            <button className="act quiet" data-action="provider.probe" onClick={connect} disabled={busy || testing || baseUrl.trim() === ""}>
              {t(busy ? "检测中…" : "验证连接并读取")}
            </button>
          )}
        </div>
        {err && !errOnSave && (
          <div className="find" data-lvl="warn" role="alert">
            <span className="t">{t("无法连接")}</span>
            <span className="why">{err}</span>
          </div>
        )}
        {probe && <p className="probe-ok">{t("连接可用 · 找到 {n} 个模型", { n: probe.models.length })}</p>}
        {completed !== "" && completed === baseUrl.trim() && <p className="probe-ok">{t("接口地址已补全为 {url}", { url: completed })}</p>}
        <div className="mlist">
          <div className="mlhead">
            <span className="ttl">{t("启用的模型")}</span>
            <span className="count">{t("已启用 {on}/{all}", { on: picked.length, all: models.length })}</span>
            <button className="mrefresh" data-action="provider.model-check-all" onClick={testAll}
              disabled={busy || testing || picked.length === 0 || baseUrl.trim() === ""} aria-busy={batch === "running" || undefined}>
              {t("测试已启用模型（{n}）", { n: picked.length })}
            </button>
          </div>
          <p className="mguide">{t("目录只用于发现，不是白名单。未列出的模型会按原始 ID 保存；「测试已启用模型」会给每个已勾选的模型各发送一次小请求，可能产生少量 Token 费用。")}</p>
          {batch === "stopped" && <p className="mdiff" role="status">{t("已停止启动新的验证：草稿已改动")}</p>}
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
              {t("该地址支持两种接入方式。{on} 支持由供应商执行的联网搜索，另一种不支持；这是协议差异，不是可配置项。", {
                on: searchOn.map((k) => t(KIND_LABEL[k] ?? k)).join("、"),
              })}
            </p>
          )}
          {probe.ambiguous && (
            <p className="acct-note">
              {t("两种接入方式都能返回模型列表，仅凭列表无法区分；两者的聊天入口路径通常不同，选错会导致聊天报错。如需同时使用，请再添加一次并选择另一种。")}
            </p>
          )}
          {probe.noProxy && (
            <p className="acct-note">{t("该来源通过代理无法连接、直连可用，已记录为「此来源不使用代理」。")}</p>
          )}

        </>
      )}
      <div className="acts-bar">
        {err && errOnSave && (
          <div className="find" data-lvl="warn">
            <span className="t">{t("无法保存")}</span>
            <span className="why">{err}</span>
          </div>
        )}
        <div className="acts addp-footer">
          <button className="act" data-action="provider.add" data-primary onClick={save} aria-describedby={nameBad ? "addp-name-rule" : undefined} disabled={busy || composing || testing || picked.length === 0 || name.trim() === "" || nameBad || kind === "" || baseUrl.trim() === "" || extraBad}>
            {t(busy ? "保存中…" : "添加来源")}
          </button>
          <button className="act" onClick={onCancel} disabled={busy || testing}>{t("取消")}</button>
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
