import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { ProviderCheck, ProviderEntry } from "../port/port";
import { clearModelCheckFacts, ModelChoice, type ModelFact } from "./ModelChoice";
import type { Port } from "./Providers";
import { SAVED_NOT_APPLIED, reason } from "../i18n/kernel";
import { HttpError } from "../port/port";
import { THINKING, headerLines, parseEffortLevels, parseExtraBody, parseHeaders } from "./provider_compat";

// Only what this form owns is sent: the entry keeps its prices, effort
// vocabularies and everything else the panel cannot show.
// declare opens the form on the reasoning fields: the composer sends a user
// here when the endpoint reported no effort levels.
export function EditConn({
  entry, initialCheck, port, busy, setBusy, onDone, onSaved, declare = false,
}: {
  entry: ProviderEntry; initialCheck?: ProviderCheck; port: Port;
  busy: string; setBusy: (b: string) => void; onDone: () => void; onSaved?: () => void; declare?: boolean;
}) {
  const seededModels = [...new Set([...entry.models, ...(initialCheck?.models ?? [])])];
  const seededVision = [...new Set([...(entry.visionModels ?? []), ...(initialCheck?.vision ?? [])])];
  const [baseUrl, setBaseUrl] = useState(entry.baseUrl);
  const [apiKey, setApiKey] = useState("");
  const [models, setModels] = useState<string[]>(seededModels);
  const [picked, setPicked] = useState<string[]>(entry.models);
  const [vision, setVision] = useState<string[]>(seededVision);
  const [visionSettable, setVisionSettable] = useState<string[] | undefined>(
    entry.visionSettable ? [...new Set([...entry.visionSettable, ...(initialCheck?.vision ?? [])])] : undefined,
  );
  const [facts, setFacts] = useState<Record<string, ModelFact>>(() => modelFacts(entry.models, initialCheck));
  const [diff, setDiff] = useState(() => initialCheck?.ok ? catalogDiff(entry.models, initialCheck.models ?? []) : null);
  const [checkingModel, setCheckingModel] = useState("");
  const [def, setDef] = useState(entry.default || entry.models[0] || "");
  const [err, setErr] = useState("");
  const [more, setMore] = useState(declare);
  const [win, setWin] = useState(entry.contextWindow ? String(entry.contextWindow) : "");
  const [maxOut, setMaxOut] = useState(entry.maxOutputTokens ? String(entry.maxOutputTokens) : "");
  const [think, setThink] = useState(entry.reasoningProtocol ?? "");
  const [levelText, setLevelText] = useState((entry.supportedEfforts ?? []).join(", "));
  const [defEffort, setDefEffort] = useState(entry.defaultEffort ?? "");
  const levels = parseEffortLevels(levelText);
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
  // A blank key field means "keep the stored one", so re-probing has to go
  // through the saved source. Sending the empty field instead probes as a
  // provider with no credential at all, which fails before it reaches the host.
  const refetch = async () => {
    setBusy(`refresh:${entry.name}`);
    setErr("");
    try {
      const refreshed = apiKey.trim()
        ? await port.probeProvider(baseUrl.trim(), apiKey.trim())
        : await port.checkProvider(entry.name);
      const found = refreshed.models ?? [];
      if (found.length === 0) throw new Error("这个端点没报出任何聊天模型");
      const readers = refreshed.vision ?? [];
      setModels((current) => {
        setDiff(catalogDiff(current, found));
        setFacts((factsNow) => refreshedFacts(current, found, factsNow));
        return [...new Set([...found, ...current])];
      });
      setVision((current) => [...new Set([...current, ...readers])]);
      setVisionSettable((current) => current ? [...new Set([...current, ...readers])] : current);
    } catch (e) {
      setErr(reason(e));
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
        kind: entry.kind,
      });
      setFacts((current) => ({
        ...current,
        [model]: { ...(current[model] ?? { origin: "configured" }), status: got.status, reason: got.reason },
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

  const save = async () => {
    setBusy(`edit:${entry.name}`);
    setErr("");
    try {
      await port.editProvider({
        name: entry.name,
        baseUrl: baseUrl.trim(),
        apiKey: apiKey.trim(),
        models: picked,
        default: picked.includes(def) ? def : picked[0] ?? "",
        vision: vision.filter((m) => picked.includes(m)),
        contextWindow: Number(win.replace(/\D/g, "")) || 0,
        maxOutputTokens: Number(maxOut.replace(/\D/g, "")) || 0,
        reasoningProtocol: think,
        supportedEfforts: levels,
        defaultEffort: levels.includes(defEffort) ? defEffort : "",
        headers: parseHeaders(heads),
        extraBody: parseExtraBody(extra) ?? {},
      });
      onDone();
    } catch (e) {
      setErr(reason(e));
      // Saved but not yet applied: the list has to show what is on file while
      // the form stays open to say why.
      if (e instanceof HttpError && SAVED_NOT_APPLIED.includes(e.reason?.code ?? "")) onSaved?.();
    } finally {
      setBusy("");
    }
  };

  return (
    <div className="addp" data-edit>
      <div className="fields">
        <label className="grow full">
          <span>{t("接口地址")}</span>
          <input value={baseUrl} onChange={(e) => {
            setBaseUrl(e.target.value);
            setFacts(clearModelCheckFacts);
          }} disabled={busy !== "" || checkingModel !== ""} spellCheck={false} />
        </label>
        <label className="grow full">
          <span>{t("API Key（留空就不动它）")}</span>
          <input type="password" value={apiKey} placeholder="········"
            onChange={(e) => {
              setApiKey(e.target.value);
              setFacts(clearModelCheckFacts);
            }} disabled={busy !== "" || checkingModel !== ""} spellCheck={false} />
        </label>
      </div>

      <div className="addp-section compact">
        <div className="addp-section-head">
          <div><strong>{t("模型限制")}</strong><span>{t("不要依赖接口猜测，请按模型文档填写")}</span></div>
        </div>
        <div className="fields token-limits">
          <label className="grow">
            <span>{t("上下文窗口")}</span>
            <span className="unit-field"><input aria-label={t("上下文窗口")} inputMode="numeric" value={win} placeholder={t("未声明")} onChange={(e) => setWin(e.target.value.replace(/\D/g, ""))} /><i>tokens</i></span>
            <i className="tip">{t("模型可承载的输入、工具结果与输出总量。")}</i>
          </label>
          <label className="grow">
            <span>{t("最大输出")}</span>
            <span className="unit-field"><input aria-label={t("最大输出")} inputMode="numeric" value={maxOut} placeholder={t("自动")} onChange={(e) => setMaxOut(e.target.value.replace(/\D/g, ""))} /><i>tokens</i></span>
            <i className="tip">{t("单轮生成上限；留空使用内核的模型默认值。")}</i>
          </label>
        </div>
      </div>

      <div className="mlist">
        <div className="mlhead">
          <span className="ttl">{t("模型")}</span>
          <span className="count">{t("已启用 {on}/{all}", { on: picked.length, all: models.length })}</span>
          <button className="mrefresh" data-action="provider.probe" onClick={refetch} disabled={busy !== "" || checkingModel !== ""}
            title={t("重新向该端点获取模型列表，适用于端点新增或下架模型之后")}>
            {t(refreshing ? "正在刷新…" : "刷新模型目录")}
          </button>
        </div>
        <p className="mguide">
          {t("目录只用于发现，不是白名单。未列出的模型会按原始 ID 保存；验证会发送一次最小请求，可能产生少量 Token 费用。")}
        </p>
        {diff && (diff.added > 0 || diff.missing > 0) && (
          <p className="mdiff" role="status">
            {diff.added > 0 && t("发现 {n} 个新模型。", { n: diff.added })}
            {diff.missing > 0 && t("有 {n} 个已配置模型本次未返回，已为你保留。", { n: diff.missing })}
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
          checkDisabled={busy !== "" || checkingModel !== ""}
          onToggle={(m) => toggle(picked, setPicked, m)}
          onVision={(m) => toggle(vision, setVision, m)}
          onDefault={setDef}
          onAdd={addModel}
        />
      </div>

      {/* Folded, and worth folding: no probe can answer these, and most
          endpoints need none of them. The fold names its contents, because a
          relay's effort levels are declared nowhere else. */}
      <details className="addp-options" open={more} onToggle={(e) => setMore(e.currentTarget.open)}>
        <summary data-action="provider.draft" data-value="compat">
          <span className="tx">
            <strong>{t("思考参数与推理档位")}</strong>
            <small>{t("以及额外请求头、请求体。中转站的推理强度在这里声明")}</small>
          </span>
          <span className="summary-value">{compatSummary(think, heads, extra, levels.length) || t("可选")}</span>
        </summary>
        {more && (
          <div className="fields compat addp-options-body">
            <label className="grow full">
              <span>{t("思考参数")}</span>
              <select ref={reasoning} value={think} onChange={(e) => setThink(e.target.value)}>
                {THINKING.map(([value, label]) => (
                  <option key={value} value={value}>
                    {t(label)}
                  </option>
                ))}
              </select>
              <i className="tip">
                {t("端点控制思考深度的方式。此项无法自动探测：中转站转发的是第三方模型，只有你知道其后端。选择后才能调整推理强度，选择错误会导致请求被端点拒绝。")}
              </i>
            </label>
            <label className="grow">
              <span>{t("推理档位")}</span>
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
                {t(levelsDormant
                  ? "当前思考参数不使用自定义档位；已填写的档位会保留，切换协议后生效。"
                  : "端点接受的推理强度取值，用逗号分隔，按原样发送。填写后会替代思考参数自带的档位；留空则使用思考参数的默认档位。")}
              </i>
            </label>
            <label className="grow">
              <span>{t("默认档位")}</span>
              <select data-action="provider.draft" data-value="default-effort" value={levels.includes(defEffort) ? defEffort : ""} disabled={levelsDormant || levels.length === 0}
                onChange={(e) => setDefEffort(e.target.value)}>
                <option value="">{t("第一个档位")}</option>
                {levels.map((l) => <option key={l} value={l}>{l}</option>)}
              </select>
              <i className="tip">{t("推理强度选「自动」时使用的档位。")}</i>
            </label>
            <label className="grow full">
              <span>{t("额外请求头")}</span>
              <textarea
                rows={3}
                value={heads}
                spellCheck={false}
                placeholder={"HTTP-Referer: https://example.com\nX-Title: Reasonix"}
                onChange={(e) => setHeads(e.target.value)}
              />
              <i className="tip">{t("每行一个「名称: 值」。中转站通常用它识别站点；密钥仍填写在上方。")}</i>
            </label>
            <label className="grow full">
              <span>{t("额外请求体")}</span>
              <textarea
                rows={4}
                value={extra}
                spellCheck={false}
                placeholder={'{\n  "enable_thinking": true\n}'}
                onChange={(e) => setExtra(e.target.value)}
                aria-invalid={extraBad || undefined}
              />
              <i className="tip">
                {t("将合并到请求体的顶层。model、messages、tools、stream 由内核控制，在此填写不会生效。")}
              </i>
            </label>
            {extraBad && <div className="why">{t("这段不是合法的 JSON 对象，保存会被拒绝。")}</div>}
          </div>
        )}
      </details>

      {err && (
        <div className="find" data-lvl="warn">
          <span className="t">{t("没保存成功")}</span>
          <span className="why">{err}</span>
        </div>
      )}

      <div className="acts">
        <button className="act" data-action="provider.save" data-primary onClick={save} disabled={busy !== "" || checkingModel !== "" || picked.length === 0 || extraBad}>
          {t(saving ? "保存中…" : "保存")}
        </button>
        <button className="act" onClick={onDone} disabled={busy !== "" || checkingModel !== ""}>{t("取消")}</button>
      </div>
    </div>
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

function catalogDiff(before: string[], found: string[]) {
  const had = new Set(before);
  const now = new Set(found);
  return {
    added: found.filter((model) => !had.has(model)).length,
    missing: before.filter((model) => !now.has(model)).length,
  };
}

function compatSummary(think: string, heads: string, extra: string, levels: number): string {
  const parts: string[] = [];
  const protocol = THINKING.find(([value]) => value === think);
  if (think && protocol) parts.push(t(protocol[1]));
  if (levels) parts.push(t("{n} 个档位", { n: levels }));
  const headCount = Object.keys(parseHeaders(heads)).length;
  if (headCount) parts.push(t("{n} 个头", { n: headCount }));
  const body = parseExtraBody(extra);
  if (body && Object.keys(body).length) parts.push(t("有请求体"));
  return parts.join(" · ");
}
