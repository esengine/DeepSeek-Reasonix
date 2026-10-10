import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { ProviderCheck, ProviderEntry } from "../port/port";
import { checkedFact, clearModelCheckFacts, ModelChoice, type ModelFact } from "./ModelChoice";
import type { Port } from "./Providers";
import { SAVED_NOT_APPLIED, reason, say } from "../i18n/kernel";
import { checkFailure } from "./provider_check";
import { HttpError } from "../port/port";
import { IDLE_TIMEOUT_MAX, IDLE_TIMEOUT_MIN, THINKING, headerLines, parseEffortLevels, parseExtraBody, parseHeaders, parseIdleTimeout } from "./provider_compat";
import { ModelEfforts } from "./ModelEfforts";
import { EffortShape } from "./EffortShape";
import type { ModelEffort, ModelLimit, ProviderEdit } from "../port/port";
import { ModelLimits, limitTextOf, limitsToSend, type LimitText } from "./ModelLimits";

// Only what this form owns is sent: the entry keeps its prices, effort
// vocabularies and everything else the panel cannot show.
// declare opens the form on the reasoning fields: the composer sends a user
// here when the endpoint reported no effort levels.
export function EditConn({
  entry, initialCheck, port, busy, setBusy, onDone, onRevert, onSaved, onDirty, declare = false, justSaved = false,
}: {
  entry: ProviderEntry; initialCheck?: ProviderCheck; port: Port;
  busy: string; setBusy: (b: string) => void; onDone: () => void | Promise<void>; onRevert: () => void; onSaved?: () => void; onDirty?: (dirty: boolean) => void;
  declare?: boolean; justSaved?: boolean;
}) {
  const seededModels = [...new Set([...entry.models, ...(initialCheck?.models ?? [])])];
  const seededVision = [...new Set([...(entry.visionModels ?? []), ...(initialCheck?.vision ?? [])])];
  const [baseUrl, setBaseUrl] = useState(entry.baseUrl);
  const [completed, setCompleted] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [models, setModels] = useState<string[]>(seededModels);
  const [picked, setPicked] = useState<string[]>(entry.models);
  const [vision, setVision] = useState<string[]>(seededVision);
  const [visionSettable, setVisionSettable] = useState<string[] | undefined>(
    entry.visionSettable ? [...new Set([...entry.visionSettable, ...(initialCheck?.vision ?? [])])] : undefined,
  );
  const [facts, setFacts] = useState<Record<string, ModelFact>>(() => modelFacts(entry.models, initialCheck));
  const [diff, setDiff] = useState(() => initialCheck?.ok ? catalogDiff(entry.models, initialCheck.models ?? [], false) : null);
  const [checkingModel, setCheckingModel] = useState("");
  const [batch, setBatch] = useState<"idle" | "running" | "stopped">("idle");
  const run = useRef<AbortController | null>(null);
  const testing = checkingModel !== "" || batch === "running";
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
  const [think, setThink] = useState(entry.reasoningProtocol ?? "");
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
    setErr(null);
    setRefreshFail("");
    setDiff(null);
    try {
      const refreshed = apiKey.trim()
        ? await port.probeProvider(baseUrl.trim(), apiKey.trim())
        : await port.checkProvider(entry.name);
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
        kind: entry.kind,
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

  const stopBatch = useCallback(() => {
    if (!run.current) return false;
    run.current.abort();
    run.current = null;
    setFacts(stopChecking);
    setBatch("stopped");
    return true;
  }, []);

  const testAll = async () => {
    if (run.current || picked.length === 0) return;
    const ctl = new AbortController();
    run.current = ctl;
    setBatch("running");
    const queue = [...picked];
    const lane = async () => {
      for (let model = queue.shift(); model !== undefined && !ctl.signal.aborted; model = queue.shift()) {
        const m = model;
        setFacts((cur) => ({ ...cur, [m]: { ...(cur[m] ?? { origin: "configured" }), checking: true } }));
        let next: (fact: ModelFact | undefined) => ModelFact;
        try {
          const got = await port.checkProviderModel({ name: entry.name, model: m, baseUrl: baseUrl.trim(), apiKey: apiKey.trim(), kind: entry.kind });
          next = (fact) => ({ ...checkedFact(fact, "configured", got), checking: false });
        } catch {
          next = (fact) => ({ ...(fact ?? { origin: "configured" }), status: "unknown", reason: "network", checking: false });
        }
        if (ctl.signal.aborted) return;
        setFacts((cur) => ({ ...cur, [m]: next(cur[m]) }));
      }
    };
    await Promise.all([lane(), lane()]);
    if (run.current === ctl) {
      run.current = null;
      setBatch("idle");
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

  const draft = (): ProviderEdit => ({
    name: entry.name,
    baseUrl: baseUrl.trim(),
    apiKey: apiKey.trim(),
    models: picked,
    default: picked.includes(def) ? def : picked[0] ?? "",
    vision: vision.filter((m) => picked.includes(m)),
    contextWindow: Number(win.replace(/\D/g, "")) || 0,
    maxOutputTokens: Number(maxOut.replace(/\D/g, "")) || 0,
    idleTimeoutSeconds: idle.ok ? idle.secs : 0,
    reasoningProtocol: think,
    supportedEfforts: levels,
    defaultEffort: levels.includes(defEffort) ? defEffort : "",
    modelEfforts: Object.fromEntries(picked.filter((m) => ownEfforts[m]).map((m) => [m, ownEfforts[m]])),
    modelLimits: limitsToSend(picked, limitText, entry.modelLimits ?? {}),
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
  useEffect(() => {
    if (!stopBatch()) setBatch((b) => (b === "stopped" ? "idle" : b));
  }, [draftKey, stopBatch]);
  useEffect(() => () => run.current?.abort(), []);
  const [edited, setEdited] = useState(false);
  if (dirty && !edited) setEdited(true);

  const canSave = (dirty || err?.kind === "unapplied") && busy === "" && !testing && picked.length > 0 && !extraBad && idle.ok;
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
      const unapplied = e instanceof HttpError && SAVED_NOT_APPLIED.includes(e.reason?.code ?? "");
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
        <label className="grow full">
          <span>{t("接口地址")}</span>
          <input value={baseUrl} onChange={(e) => {
            setBaseUrl(e.target.value);
            setFacts(clearModelCheckFacts);
            setRefreshFail("");
            setDiff(null);
          }} disabled={busy !== "" || checkingModel !== ""} spellCheck={false} />
        </label>
        <label className="grow full">
          <span>{t("API Key（留空就不动它）")}</span>
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
        {(picked.length > 1 || picked.some((m) => entry.modelLimits?.[m])) && (
          <ModelLimits models={picked} value={limitText} onChange={setLimitText} inherited={inheritedLimitsFor} />
        )}
      </div>

      <div className="mlist">
        <div className="mlhead">
          <span className="ttl">{t("模型")}</span>
          <span className="count">{t("已启用 {on}/{all}", { on: picked.length, all: models.length })}</span>
          <button className="mrefresh" data-action="provider.probe" onClick={refetch} disabled={busy !== "" || testing}
            title={t("用已保存或刚填的密钥向服务商读取模型列表，可直接勾选；服务商新增或下架模型后也一样")}>
            {t(refreshing ? "正在从服务商读取…" : "从服务商读取可用模型")}
          </button>
          <button className="mrefresh" data-action="provider.model-check-all" onClick={testAll}
            disabled={busy !== "" || testing || picked.length === 0} aria-busy={batch === "running" || undefined}>
            {t("测试已启用模型（{n}）", { n: picked.length })}
          </button>
        </div>
        <p className="mguide">
          {t("目录只用于发现，不是白名单。未列出的模型会按原始 ID 保存；「测试已启用模型」会给每个已勾选的模型各发送一次小请求，可能产生少量 Token 费用。")}
        </p>
        {completed !== "" && completed === baseUrl.trim() && (
          <p className="mdiff" role="status">{t("接口地址已补全为 {url}", { url: completed })}</p>
        )}
        {batch === "stopped" && <p className="mdiff" role="status">{t("已停止启动新的验证：草稿已改动")}</p>}
        {diff && diff.fresh && diff.added === 0 && diff.missing === 0 && (
          <p className="mdiff" role="status">{t("没有发现新模型")}</p>
        )}
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
          checkDisabled={busy !== "" || testing}
          onToggle={(m) => toggle(picked, setPicked, m)}
          onVision={(m) => toggle(vision, setVision, m)}
          onDefault={setDef}
          onAdd={addModel}
          onFetch={refetch}
          fetching={refreshing}
          fetchDisabled={busy !== "" || testing}
          fetchFail={refreshFail}
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
          <span className="summary-value">
            {compatSummary(think, heads, extra, levels.length, picked.filter((m) => ownEfforts[m]?.supportedEfforts.length).length, idle.ok ? idle.secs : 0) || t("可选")}
          </span>
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
              <EffortShape field={think === (entry.reasoningProtocol ?? "") ? entry.effortField : undefined} level={levels[0]} />
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
                {levelsDormant
                  ? t("当前思考参数不使用自定义档位；已填写的档位会保留，切换协议后生效。")
                  : entry.effortField
                    ? t("端点接受的取值，逗号分隔，按原样作为 {field} 的值发送。填写后会替代思考参数自带的档位；型号没有内置档位时需要在这里手填才能选。", { field: entry.effortField })
                    : t("端点接受的取值，逗号分隔，按原样发送。填写后会替代思考参数自带的档位；型号没有内置档位时需要在这里手填才能选。")}
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
              <span>{t("无响应超时")}</span>
              <span className="unit-field">
                <input
                  aria-label={t("无响应超时")}
                  data-action="provider.draft"
                  data-value="idle-timeout"
                  inputMode="numeric"
                  value={idleText}
                  placeholder={entry.idleTimeoutDefault ? t("默认 {n}", { n: entry.idleTimeoutDefault }) : t("默认")}
                  aria-invalid={!idle.ok || undefined}
                  onChange={(e) => setIdleText(e.target.value)}
                />
                <i>{t("秒")}</i>
              </span>
              <i className="tip">
                {t("等待响应头、以及回包过程中无新内容超过这个时间，就按连接中断处理并重试。本地模型处理长上下文时可能需要调大。")}
              </i>
            </label>
            {!idle.ok && <div className="why">{t("须是 {min} 到 {max} 之间的整数秒；留空使用默认值。", { min: IDLE_TIMEOUT_MIN, max: IDLE_TIMEOUT_MAX })}</div>}
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

      <div className="acts-bar">
        {err && (
          <div className="find" data-lvl={err.kind === "unapplied" ? "warn" : "err"} role={err.kind === "unapplied" ? "status" : "alert"}>
            {err.kind === "save" && <span className="t">{t("保存失败")}</span>}
            <span className="why">{err.text}</span>
          </div>
        )}
        <div className="acts">
          <button className="act" data-action="provider.save" data-primary onClick={save} disabled={!canSave}>
            {t(saving ? "保存中…" : "保存")}
          </button>
          <button className="act" data-action="provider.revert" onClick={onRevert} disabled={!dirty || busy !== "" || testing}>{t("还原")}</button>
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
  if (levels) parts.push(t("{n} 个档位", { n: levels }));
  if (ownModels) parts.push(t("{n} 个模型单独设置", { n: ownModels }));
  if (idleSecs) parts.push(t("{n} 秒无响应超时", { n: idleSecs }));
  const headCount = Object.keys(parseHeaders(heads)).length;
  if (headCount) parts.push(t("{n} 个头", { n: headCount }));
  const body = parseExtraBody(extra);
  if (body && Object.keys(body).length) parts.push(t("有请求体"));
  return parts.join(" · ");
}

type Phase = "clean" | "dirty" | "saving" | "saved" | "unapplied" | "failed";
const PHASE_TEXT: Record<Phase, string> = {
  clean: "没有更改", dirty: "有未保存的更改", failed: "有未保存的更改", saving: "正在保存…",
  saved: "已保存", unapplied: "已保存，尚未生效",
};

function stopChecking(facts: Record<string, ModelFact>): Record<string, ModelFact> {
  return Object.fromEntries(Object.entries(facts).map(([model, fact]) => [model, fact.checking ? { ...fact, checking: false } : fact]));
}
