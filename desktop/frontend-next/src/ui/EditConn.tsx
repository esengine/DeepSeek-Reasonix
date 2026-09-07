import { useState } from "react";
import { t } from "../i18n";
import type { ProviderEntry } from "../port/port";
import { ModelChoice } from "./ModelChoice";
import type { Port } from "./Providers";
import { reason } from "../i18n/kernel";

// The reasoning vocabularies the kernel knows, in config's own spelling. Auto
// is the absence of a declaration, not a seventh shape.
const THINKING: [string, string][] = [
  ["", "自动 · 按模型和地址推断"],
  ["openai", "OpenAI reasoning_effort"],
  ["anthropic", "Anthropic thinking"],
  ["deepseek", "DeepSeek"],
  ["glm", "GLM enable_thinking"],
  ["kimi-k3", "Kimi K3"],
  ["none", "不发思考参数"],
];

// Editing a saved connection. Headers and the extra body are typed as text and
// parsed here, because what a relay needs is not a shape this side can know.
export // Editing a saved source. Only what this form owns is sent: the entry keeps its
// prices, effort vocabularies and everything else the panel cannot show.
function EditConn({
  entry, port, busy, setBusy, onDone,
}: {
  entry: ProviderEntry; port: Port; busy: string; setBusy: (b: string) => void; onDone: () => void;
}) {
  const [baseUrl, setBaseUrl] = useState(entry.baseUrl);
  const [apiKey, setApiKey] = useState("");
  const [models, setModels] = useState<string[]>(entry.models);
  const [picked, setPicked] = useState<string[]>(entry.models);
  const [vision, setVision] = useState<string[]>(entry.visionModels ?? []);
  const [def, setDef] = useState(entry.default || entry.models[0] || "");
  const [err, setErr] = useState("");
  const [more, setMore] = useState(false);
  const [win, setWin] = useState(entry.contextWindow ? String(entry.contextWindow) : "");
  const [think, setThink] = useState(entry.reasoningProtocol ?? "");
  const [heads, setHeads] = useState(headerLines(entry.headers));
  const [extra, setExtra] = useState(entry.extraBody ? JSON.stringify(entry.extraBody, null, 2) : "");
  const saving = busy === `edit:${entry.name}`;
  const extraBad = extra.trim() !== "" && parseExtraBody(extra) === null;

  const toggle = (list: string[], set: (v: string[]) => void, m: string) =>
    set(list.includes(m) ? list.filter((x) => x !== m) : [...list, m]);

  // Which rows may take images. The kernel answers per model now — one endpoint
  // serves an image-taking model beside text-only ones — so an older kernel that
  // only sends the connection-wide boolean still gets the old answer.
  const visionLocked = (m: string) =>
    entry.visionSettable ? !entry.visionSettable.includes(m) : entry.canSetVision === false;

  // A name the endpoint never reported. It lands ticked because typing it out
  // is already the answer to "do you want this one", and at the head of the
  // list because a new row three hundred names down reads as nothing happening.
  const addModel = (m: string) => {
    setModels((cur) => (cur.includes(m) ? cur : [m, ...cur]));
    setPicked((cur) => (cur.includes(m) ? cur : [...cur, m]));
  };

  // Re-asking the endpoint is how a source that gained models catches up; the
  // ticks the user already made survive it.
  // A blank key field means "keep the stored one", so re-probing has to go
  // through the saved source. Sending the empty field instead probes as a
  // provider with no credential at all, which fails before it reaches the host.
  const refetch = async () => {
    setBusy(`edit:${entry.name}`);
    setErr("");
    try {
      const found = apiKey.trim()
        ? (await port.probeProvider(baseUrl.trim(), apiKey.trim())).models
        : (await port.checkProvider(entry.name)).models ?? [];
      if (found.length === 0) throw new Error("这个端点没报出任何聊天模型");
      setModels([...new Set([...found, ...picked])]);
    } catch (e) {
      setErr(reason(e));
    } finally {
      setBusy("");
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
        reasoningProtocol: think,
        headers: parseHeaders(heads),
        extraBody: parseExtraBody(extra) ?? {},
      });
      onDone();
    } catch (e) {
      setErr(reason(e));
    } finally {
      setBusy("");
    }
  };

  return (
    <div className="addp" data-edit>
      <div className="fields">
        <label className="grow full">
          <span>{t("接口地址")}</span>
          <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} spellCheck={false} />
        </label>
        <label className="grow full">
          <span>{t("API Key（留空就不动它）")}</span>
          <input type="password" value={apiKey} placeholder="········"
            onChange={(e) => setApiKey(e.target.value)} spellCheck={false} />
        </label>
      </div>

      <div className="mlist">
        <span className="mlb">
          {t("模型 · 已启用 {on}/{all}", { on: picked.length, all: models.length })}{" · "}
          {t(entry.canSetVision === false ? "这个端点不接受图片输入，勾了也不会生效" : "勾「读图」的才会收到图片")}
        </span>
        <ModelChoice
          models={models}
          picked={picked}
          vision={vision}
          def={def}
          visionLocked={visionLocked}
          onToggle={(m) => toggle(picked, setPicked, m)}
          onVision={(m) => toggle(vision, setVision, m)}
          onDefault={setDef}
          onAdd={addModel}
        />
      </div>

      {/* Folded, and worth folding: these three are the ones no probe can
          answer, and most endpoints need none of them. */}
      <button className="more" aria-expanded={more} onClick={() => setMore((v) => !v)}>
        {t(more ? "收起" : "端点要求的额外设置")}
        <span className="c">{compatSummary(win, heads, extra)}</span>
      </button>

      {more && (
        <div className="fields compat">
          <label className="grow full">
            <span>{t("上下文窗口（tokens）")}</span>
            <input
              inputMode="numeric"
              value={win}
              placeholder={t("留空表示使用内置的已知值；自行添加的来源没有内置值，将不进行压缩")}
              onChange={(e) => setWin(e.target.value.replace(/\D/g, ""))}
            />
            <i className="tip">
              {t("填模型文档写的上下文上限，不是最大输出。填小了会一直压缩，填大了会在真到上限时被端点拒绝。")}
            </i>
          </label>
          <label className="grow full">
            <span>{t("思考参数")}</span>
            <select value={think} onChange={(e) => setThink(e.target.value)}>
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

      {err && (
        <div className="find" data-lvl="warn">
          <span className="t">{t("没保存成功")}</span>
          <span className="why">{err}</span>
        </div>
      )}

      <div className="acts">
        <button className="act" data-action="provider.save" data-primary onClick={save} disabled={busy !== "" || picked.length === 0 || extraBad}>
          {t(saving ? "保存中…" : "保存")}
        </button>
        <button className="act" data-action="provider.probe" onClick={refetch} disabled={busy !== ""}
          title={t("重新向该端点获取模型列表，适用于端点新增或下架模型之后")}>
          {t("重新问一次有哪些模型")}
        </button>
        <button className="act" onClick={onDone} disabled={busy !== ""}>{t("取消")}</button>
      </div>
    </div>
  );
}

// The three compatibility fields move between a config object and the text the
// user types. Headers are one "name: value" per line because that is how the
// gateway's own documentation writes them.
function headerLines(headers?: Record<string, string>): string {
  return Object.entries(headers ?? {})
    .map(([k, v]) => `${k}: ${v}`)
    .join("\n");
}

function parseHeaders(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const at = line.indexOf(":");
    if (at <= 0) continue;
    const name = line.slice(0, at).trim();
    const value = line.slice(at + 1).trim();
    if (name && value) out[name] = value;
  }
  return out;
}

// null means "typed but not valid JSON yet", which is different from an empty
// object — the save button reads the difference rather than sending garbage.

// null means "typed but not valid JSON yet", which is different from an empty
// object — the save button reads the difference rather than sending garbage.
function parseExtraBody(text: string): Record<string, unknown> | null {
  if (!text.trim()) return {};
  try {
    const parsed: unknown = JSON.parse(text);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    return parsed as Record<string, unknown>;
  } catch {
    return null;
  }
}

function compatSummary(win: string, heads: string, extra: string): string {
  const parts: string[] = [];
  if (win.trim()) parts.push(win === "0" ? t("不压缩") : `${Number(win) / 1000}k`);
  const headCount = Object.keys(parseHeaders(heads)).length;
  if (headCount) parts.push(t("{n} 个头", { n: headCount }));
  const body = parseExtraBody(extra);
  if (body && Object.keys(body).length) parts.push(t("有请求体"));
  return parts.join(" · ");
}
