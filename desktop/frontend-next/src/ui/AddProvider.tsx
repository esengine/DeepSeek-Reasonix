import { useEffect, useState } from "react";
import { t } from "../i18n";
import type { Protocol, ProviderEntry, ProviderProbe } from "../port/port";
import { ModelChoice } from "./ModelChoice";
import { KIND_LABEL, hostOf, nameFrom, vendorLabel } from "./vendors";
import type { Port } from "./Providers";
import { reason } from "../i18n/kernel";

// Adding a connection asks two questions — where, and with what key. Everything
// else is knowable by asking the endpoint, so it is asked rather than typed.
export function AddProvider({
  port, taken, known, onDone, onCancel,
}: {
  port: Port; taken: string[]; known: ProviderEntry[]; onDone: () => void; onCancel: () => void;
}) {
  const [baseUrl, setBaseUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [probe, setProbe] = useState<ProviderProbe | null>(null);
  const [catalog, setCatalog] = useState<Protocol[]>([]);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  // The endpoint says which wires are on the table and the kernel says what
  // each one is, so neither list is written here.
  useEffect(() => {
    port.protocols().then(setCatalog).catch(() => setCatalog([]));
  }, [port]);
  const choices = probe ? (probe.kinds?.length ? probe.kinds : [probe.kind]) : [];
  const searchOn = choices.filter((k) => catalog.find((p) => p.kind === k)?.serverWebSearch);
  const searchSplit = searchOn.length > 0 && searchOn.length < choices.length;

  // Everything below is editable after the probe, because every one of these
  // is something the endpoint could not tell us for certain.
  const [name, setName] = useState("");
  const [kind, setKind] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  // What the probe reported, plus anything typed in. Kept apart from `probe` so
  // an added name survives without pretending the endpoint reported it.
  const [models, setModels] = useState<string[]>([]);

  // A source already at this host changes what a blank key means: another door
  // onto that account rather than an account with no credential.
  const sibling = known.find((p) => hostOf(p.baseUrl) === hostOf(baseUrl) && baseUrl.trim() !== "");

  const connect = async () => {
    setBusy(true);
    setErr("");
    try {
      const got = await port.probeProvider(baseUrl.trim(), apiKey.trim());
      setProbe(got);
      setKind(got.kind);
      setModels(got.models);
      setPicked(got.models.slice(0, 8));
      setName(uniqueName(nameFrom(baseUrl), taken));
    } catch (e) {
      setProbe(null);
      setErr(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const save = async () => {
    if (!probe) return;
    setBusy(true);
    setErr("");
    try {
      await port.saveProvider({
        name: name.trim(),
        kind,
        baseUrl: baseUrl.trim(),
        apiKey: apiKey.trim(),
        models: picked,
        default: picked[0] ?? "",
        authHeader: probe.authHeader,
        noProxy: probe.noProxy,
        effort: "",
        vision: probe.vision.filter((m) => picked.includes(m)),
      });
      onDone();
    } catch (e) {
      setErr(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const toggle = (m: string) =>
    setPicked((cur) => (cur.includes(m) ? cur.filter((x) => x !== m) : [...cur, m]));

  const addModel = (m: string) => {
    setModels((cur) => (cur.includes(m) ? cur : [m, ...cur]));
    setPicked((cur) => (cur.includes(m) ? cur : [...cur, m]));
  };

  return (
    <div className="addp">
      <div className="fields">
        <label className="grow full">
          <span>{t("接口地址")}</span>
          <input
            value={baseUrl}
            placeholder="https://api.moonshot.cn/v1"
            onChange={(e) => setBaseUrl(e.target.value)}
            spellCheck={false}
          />
        </label>
        <label className="grow full">
          <span>API Key{t(sibling ? "（留空就用现有那个来源的 key）" : "")}</span>
          <input type="password" value={apiKey} placeholder={sibling ? "········" : ""}
            onChange={(e) => setApiKey(e.target.value)} spellCheck={false} />
        </label>
        {sibling && (
          <p className="acct-note">
            这个地址上已经有「{vendorLabel(hostOf(sibling.baseUrl))}」了。留空 key
            {t("这会为该来源添加另一种接入方式，两者合并为同一个来源，通过「接入方式」切换；若填写新的 key，则视为本机的另一个账号，用量分别计算。")}
          </p>
        )}
      </div>

      <div className="acts">
        <button className="act" data-primary onClick={connect} disabled={busy || baseUrl.trim() === ""}>
          {t(busy && !probe ? "连接中…" : "连一下试试")}
        </button>
        <button className="act" onClick={onCancel} disabled={busy}>
          {t("取消")}
        </button>
      </div>

      {err && (
        <div className="find" data-lvl="warn">
          <span className="t">{t("连不上")}</span>
          <span className="why">{err}</span>
        </div>
      )}

      {probe && (
        <>
          {/* The heading says these are guesses, so no row has to repeat it. */}
          <p className="acct-note">{t("探到了下面这些。都是猜的，不对就改。")}</p>

          <div className="fields">
            <label className="grow">
              <span>{t("名字")}</span>
              <input value={name} onChange={(e) => setName(e.target.value)} spellCheck={false} />
            </label>
            <label className="grow">
              <span>{t("接入方式")}</span>
              <select value={kind} onChange={(e) => setKind(e.target.value)}>
                {choices.map((k) => (
                  <option key={k} value={k}>{t(KIND_LABEL[k] ?? k)}</option>
                ))}
              </select>
            </label>
          </div>
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

          <div className="mlist">
            <span className="mlb">
              {t("模型 · 已启用 {on}/{all}", { on: picked.length, all: models.length })}
            </span>
            <ModelChoice
              models={models}
              picked={picked}
              vision={probe.vision}
              onToggle={toggle}
              onAdd={addModel}
            />
          </div>

          <div className="acts">
            <button className="act" data-primary onClick={save} disabled={busy || picked.length === 0 || name.trim() === ""}>
              {t(busy ? "保存中…" : "添加")}
            </button>
          </div>
        </>
      )}
    </div>
  );
}

// A second provider from the same vendor must not overwrite the first.

// A second provider from the same vendor must not overwrite the first.
function uniqueName(base: string, taken: string[]): string {
  if (!taken.includes(base)) return base;
  for (let i = 2; i < 100; i++) {
    if (!taken.includes(`${base}-${i}`)) return `${base}-${i}`;
  }
  return base;
}

// The three compatibility fields move between a config object and the text the
// user types. Headers are one "name: value" per line because that is how the
// gateway's own documentation writes them.
