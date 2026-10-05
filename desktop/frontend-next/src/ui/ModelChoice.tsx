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
  facts?: Record<string, ModelFact>;
  onCheck?: (m: string) => void;
  checkDisabled?: boolean;
  onToggle: (m: string) => void;
  onAdd: (m: string) => void;
}

export function ModelChoice({
  models, picked, vision, def, onDefault, onVision, visionLocked, facts, onCheck, checkDisabled, onToggle, onAdd,
}: Props) {
  const [q, setQ] = useState("");
  const query = q.trim().toLowerCase();
  const on = useMemo(() => new Set(picked), [picked]);

  // Enabled first, and deliberately keyed on the list alone: re-sorting on every
  // tick throws the row out from under the pointer that just hit it. Menu.tsx
  // keeps group order over relevance for the same reason.
  const order = useMemo(
    () => [...models].sort((a, b) => Number(picked.includes(b)) - Number(picked.includes(a))),
    [models],
  );

  const hits = query ? order.filter((m) => m.toLowerCase().includes(query)) : order;
  const [shown, hidden] = cap(hits, on);
  // Only when nothing already carries the name: offering to add a model that is
  // sitting three rows down is how a list grows two of everything.
  const naming = q.trim() !== "" && !models.some((m) => m.toLowerCase() === query);

  const add = () => {
    onAdd(q.trim());
    setQ("");
  };

  return (
    <>
      <div className="msearch">
        <input
          type="search"
          value={q}
          spellCheck={false}
          placeholder={t("搜索或输入完整模型 ID…")}
          aria-label={t("搜索或添加模型")}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key !== "Enter" || !naming) return;
            e.preventDefault();
            add();
          }}
        />
        {query && <span className="cnt">{hits.length} / {models.length}</span>}
      </div>

      <div className="mrows">
        {shown.map((m) => (
          <div className="mline" key={m} data-off={on.has(m) ? undefined : ""}
            aria-busy={facts?.[m]?.checking || undefined}>
            <button className="tick" role="checkbox" aria-checked={on.has(m)}
              aria-label={t("选用 {name}", { name: m })} onClick={() => onToggle(m)}>
              <i />
            </button>
            <span className="mid">
              <span className="nm">{m}</span>
              {facts?.[m] && <ModelEvidence fact={facts[m]} />}
            </span>
            <span className="mops">
              {onCheck && (
                <button className="mverify" data-action="provider.model-check" data-target={m}
                  disabled={!on.has(m) || checkDisabled || facts?.[m]?.checking}
                  aria-label={t("验证模型 {name}", { name: m })} onClick={() => onCheck(m)}>
                  {t(facts?.[m]?.checking ? "验证中…" : facts?.[m]?.status ? "重新验证" : "验证")}
                </button>
              )}
              {onVision ? (
                <button className="vtag" aria-pressed={vision.includes(m)}
                  aria-label={t("{name} 的图片输入", { name: m })}
                  disabled={!on.has(m) || visionLocked?.(m)}
                  title={visionLocked?.(m) ? t("当前连接不会向这个模型发送图片") : t(vision.includes(m) ? "已启用图片输入" : "图片能力未确认；点击后按手动声明启用")}
                  onClick={() => onVision(m)}>
                  {t(vision.includes(m) ? "读图" : visionLocked?.(m) ? "不发送图片" : "读图？")}
                </button>
              ) : (
                vision.includes(m) && <span className="vtag" data-flat>{t("读图")}</span>
              )}
              {onDefault && (
                <button className="dtag" aria-pressed={def === m} disabled={!on.has(m)}
                  aria-label={t("将 {name} 设为默认模型", { name: m })}
                  onClick={() => onDefault(m)}>
                  {t("默认")}
                </button>
              )}
            </span>
          </div>
        ))}
      </div>

      {hidden > 0 && <span className="mmore">{t("另有 {n} 个未列出，可通过搜索找到", { n: hidden })}</span>}
      {shown.length === 0 && !naming && <div className="empty">{t("没有匹配的模型。")}</div>}

      {/* Under the list, never over it: a "create" offer sitting above seven
          real matches is how you add a model when you meant to pick one. */}
      {naming && (
        <button className="mnew" onClick={add}>
          <span className="k">+</span>
          <span className="tx">
            <span className="lb">{t("添加未列出的模型「{name}」", { name: q.trim() })}</span>
            <span className="ds">{t("按原始模型 ID 保存；添加后可以验证，不会替换成相似名称")}</span>
          </span>
        </button>
      )}
    </>
  );
}

function ModelEvidence({ fact }: { fact: ModelFact }) {
  const origin = {
    configured: "已配置",
    endpoint: "现场探测",
    manual: "用户添加",
    missing: "本次未返回",
  }[fact.origin];
  const state = fact.checking
    ? "验证中…"
    : fact.status === "available"
      ? "已验证可用"
      : fact.status === "unavailable"
        ? unavailableReason(fact.reason)
        : fact.status === "unknown"
          ? checkReason(fact.reason)
          : "待验证";
  return (
    <span className="mevidence" role={fact.status === "unavailable" ? "alert" : "status"} aria-live="polite">
      <span>{t(origin)}</span>
      {state && <><i aria-hidden="true" data-state={fact.checking ? "checking" : fact.status ?? "unverified"} />{t(state)}</>}
      {!fact.checking && fact.httpStatus ? <code className="mhttp">{`HTTP ${fact.httpStatus}`}</code> : null}
      {!fact.checking && fact.detail ? <span className="mdetail">{fact.detail}</span> : null}
    </span>
  );
}

function unavailableReason(reason?: ProviderModelCheckReason): string {
  switch (reason) {
    case "not_found": return "模型不存在或账号未开放";
    case "rejected": return "端点不接受这个模型";
    case "tools": return "能对话但不接受工具调用，用不了";
    default: return "当前不可用";
  }
}

function checkReason(reason?: ProviderModelCheckReason): string {
  switch (reason) {
    case "auth": return "凭据未通过，尚未确认";
    case "rate_limited": return "遇到限流，尚未确认";
    case "network": return "网络异常，尚未确认";
    case "timeout": return "验证超时，尚未确认";
    case "rejected": return "请求被拒绝，尚未确认";
    default: return "尚未确认";
  }
}

function cap(hits: string[], on: Set<string>): [string[], number] {
  if (hits.length <= CAP) return [hits, 0];
  let room = Math.max(CAP - hits.filter((m) => on.has(m)).length, 0);
  const out = hits.filter((m) => on.has(m) || room-- > 0);
  return [out, hits.length - out.length];
}
