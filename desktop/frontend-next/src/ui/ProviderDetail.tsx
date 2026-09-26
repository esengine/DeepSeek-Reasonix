import { useState } from "react";
import { t } from "../i18n";
import type { ProviderCheck } from "../port/port";
import { EditConn } from "./EditConn";
import type { Account, Port } from "./Providers";
import { KIND_LABEL } from "./vendors";
import { PROVIDER_EDIT_DISABLED, reason } from "../i18n/kernel";
import { HttpError } from "../port/http_error";

// How a turn's context reaches the next one. Auto is vendor detection, which is
// the only honest answer for an endpoint nobody has characterised; the other two
// are what a reader picks when the endpoint has already contradicted it.
const CONTINUATIONS: ReadonlyArray<readonly [string, string]> = [
  ["", "自动"],
  ["stateful", "引用上一轮"],
  ["stateless", "每轮完整发送"],
];

const CONTINUATION_WHY: Record<string, string> = {
  "": "按端点厂商判断。中转站若回报 previous_response_id 不受支持，改为「每轮完整发送」。",
  stateful: "只发送新的一轮，历史由端点自己保存 —— 前缀缓存命中率最高，但要求端点真的存了。",
  stateless: "每轮重发完整历史。中转站只转发、不保存状态时用这一档。",
};

// The selected account, edited in place. The protocol is a switch on it rather
// than a fact on a row, because both entries are the same key at the same host;
// 测试连接 is what turns "which protocol did we record" back into a finding.
export function ProviderDetail({
  a, port, busy, setBusy, kind, onProtocol, onRemove, onEdited, onFailed, declare,
}: {
  a: Account; port: Port; busy: string; setBusy: (b: string) => void;
  kind: string; onProtocol: (kind: string) => void; onRemove: (name: string) => void;
  onEdited: () => void; onFailed: (why: string) => void; declare?: string;
}) {
  const [found, setFound] = useState<ProviderCheck | null>(null);
  // A refusal is not a failed probe. The kernel withholds these routes from a
  // server reachable over the network, because adding a source writes a key
  // into the credential store of the machine running the kernel.
  const [refused, setRefused] = useState("");
  // Cancel and save both hand the form a fresh start from what is stored.
  const [revision, setRevision] = useState(0);
  const entry = a.byKind[kind] ?? a.byKind[a.kinds[0]];
  const checking = busy === `check:${entry.name}`;
  const inUse = a.kinds.some((k) => a.byKind[k].inUse);

  const write = async (tag: string, call: () => Promise<void>) => {
    setBusy(`${tag}:${entry.name}`);
    onFailed("");
    try {
      await call();
      onEdited();
    } catch (e) {
      onFailed(reason(e));
    } finally {
      setBusy("");
    }
  };

  const check = async () => {
    setBusy(`check:${entry.name}`);
    setFound(null);
    setRefused("");
    try {
      setFound(await port.checkProvider(entry.name));
    } catch (e) {
      // Read off the code the kernel sent, never the status: 403 is also what a
      // gateway in front of it answers, and that is a different thing to do next.
      if (e instanceof HttpError && e.reason?.code === PROVIDER_EDIT_DISABLED) setRefused(reason(e));
      else setFound({ ok: false, error: reason(e) });
    } finally {
      setBusy("");
    }
  };

  // What the endpoint answered with that this connection does not list. The
  // vendor adds models to endpoints we already know; a stored list never finds
  // out on its own, and the probe is the one place that already has the answer.
  const unlisted = (found?.models ?? []).filter((m) => !entry.models.includes(m));
  const options = a.kinds.length > 1 || entry.canWebSearch || entry.canSetThinking || entry.canSetContinuation;
  return (
    <section className="pdetail" aria-label={a.label}>
      <header className="pdetail-hd">
        <span className="tx">
          <h3>{a.label}</h3>
          <small>{t(KIND_LABEL[entry.kind] ?? entry.kind)} · {a.host}</small>
        </span>
        {inUse && <i className="pdetail-use">{t("正在用")}</i>}
        <button className="act" data-action="provider.probe" onClick={check} disabled={busy !== ""}>
          {t(checking ? "测试中…" : "测试连接")}
        </button>
        <button className="act" data-danger data-action="provider.remove" data-target={entry.name} onClick={() => onRemove(entry.name)} disabled={busy !== ""}>
          {t("删除")}
        </button>
      </header>
      {refused && (
        <div className="find" data-lvl="warn" role="status">
          <span className="t">{refused}</span>
          <span className="why">{t("模型来源要在运行内核的那台机器上配置。")}</span>
        </div>
      )}
      {found && (
        <div className="find" data-lvl={found.ok ? "ok" : "warn"} role="status">
          <span className="t">
            {found.ok
              ? `${t("连上了")} · ${t(KIND_LABEL[found.kind ?? ""] ?? found.kind ?? "")} · ${t("{n} 个模型", { n: found.models?.length ?? 0 })}`
              : t("无法连接")}
          </span>
          <span className="why">
            {!found.ok && found.error}
            {found.ok && found.matches === false &&
              t("记的是 {had}，但它答的是 {got}。", { had: t(KIND_LABEL[entry.kind] ?? entry.kind), got: t(KIND_LABEL[found.kind ?? ""] ?? found.kind ?? "") })}
            {found.ok && found.matches !== false && t("key 有效，协议也对得上。")}
            {found.ok && found.noProxy && " " + t("走代理连不上、直连可以。")}
            {found.ok && unlisted.length > 0 &&
              " " + t("这个端点还有 {n} 个模型不在列表里：{names}。点「刷新模型目录」把它们加进来。", {
                n: unlisted.length,
                names: unlisted.slice(0, 3).join("、") + (unlisted.length > 3 ? "…" : ""),
              })}
          </span>
        </div>
      )}
      {options && (
        <div className="pdetail-options">
          {a.kinds.length > 1 && (
            <div className="vway">
              <span className="lb">{t("接入方式")}</span>
              <div className="seg" role="group" aria-label={t("{name} 的接入方式", { name: a.label })}>
                {a.kinds.map((k) => (
                  <button key={k} data-action="provider.protocol" data-target={entry.name} data-value={k} aria-pressed={k === kind} disabled={busy !== ""} onClick={() => onProtocol(k)}>
                    {t(KIND_LABEL[k] ?? k)}
                    {/* A door that carries a capability the other lacks has to say
                        so on itself: switching is otherwise a silent downgrade. */}
                    {a.byKind[k].canWebSearch && <i className="perk">{t("联网搜索")}</i>}
                  </button>
                ))}
              </div>
              <span className="why">
                {t(a.kinds.some((k) => a.byKind[k].canWebSearch) && !entry.canWebSearch ? "同一账号的两种接入方式。当前这一种不支持联网搜索；这是协议差异，不是可配置项。" : "同一账号的两种接入方式。切换后下方的模型列表随之改变。")}
              </span>
            </div>
          )}
          {entry.canWebSearch && (
            <div className="vway">
              <span className="lb">{t("联网搜索")}</span>
              <div className="seg" role="group" aria-label={t("{name} 的联网搜索", { name: a.label })}>
                {[true, false].map((on) => (
                  <button key={String(on)} data-action="provider.web-search" data-target={entry.name} data-value={String(on)}
                    aria-pressed={entry.webSearch === on} disabled={busy !== ""}
                    onClick={() => write("search", () => port.setProviderWebSearch(entry.name, on))}>
                    {t(on ? "开" : "关")}
                  </button>
                ))}
              </div>
              <span className="why">{t("端点自己执行的搜索，不占本地工具。")}</span>
            </div>
          )}
          {entry.canSetThinking && (
            <div className="vway">
              <span className="lb">{t("思考参数")}</span>
              <div className="seg" role="group" aria-label={t("{name} 的思考参数", { name: a.label })}>
                {[true, false].map((on) => (
                  <button key={String(on)} data-action="provider.thinking" data-target={entry.name} data-value={String(on)}
                    aria-pressed={(entry.sendsThinking ?? true) === on} disabled={busy !== ""}
                    onClick={() => write("thinking", () => port.setProviderThinking(entry.name, on))}>
                    {t(on ? "自动" : "不发送")}
                  </button>
                ))}
              </div>
              <span className="why">
                {t(entry.sendsThinking === false ? "只发送常规聊天参数，不再指定思考深度；模型自身的推理行为不受影响。" : "部分中转站不支持 thinking 字段，会拒绝整个请求。遇到这种情况请切换为「不发送」。")}
              </span>
            </div>
          )}
          {entry.canSetContinuation && (
            <div className="vway">
              <span className="lb">{t("上下文续接")}</span>
              <div className="seg" role="group" aria-label={t("{name} 的上下文续接方式", { name: a.label })}>
                {CONTINUATIONS.map(([mode, label]) => (
                  <button key={mode} data-action="provider.continuation" data-target={entry.name} data-value={mode}
                    aria-pressed={(entry.continuation ?? "") === mode} disabled={busy !== ""}
                    onClick={() => write("continuation", () => port.setProviderContinuation(entry.name, mode))}>
                    {t(label)}
                  </button>
                ))}
              </div>
              <span className="why">{t(CONTINUATION_WHY[entry.continuation ?? ""] ?? CONTINUATION_WHY[""])}</span>
            </div>
          )}
        </div>
      )}
      <EditConn
        key={`${entry.name}:${revision}`}
        entry={entry}
        initialCheck={found?.ok ? found : undefined}
        port={port}
        busy={busy}
        setBusy={setBusy}
        declare={!!declare && entry.name === declare}
        onDone={() => {
          setRevision((r) => r + 1);
          onEdited();
        }}
        onSaved={onEdited}
      />
    </section>
  );
}
