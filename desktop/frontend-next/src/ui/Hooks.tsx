import { useEffect, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, HookCatalog, HookDryRun, HookEntry } from "../port/port";
import { reason } from "../i18n/kernel";

// Nobody arrives wanting a "PreToolUse hook with an anchored matcher". They
// arrive wanting the agent to run their formatter. The recipe is the product;
// the event, the matcher and the exit-code contract are its implementation.
interface Recipe {
  id: string;
  title: string;
  desc: string;
  event: string;
  match?: string;
  command: string;
}

const RECIPES: Recipe[] = [
  {
    id: "format",
    title: "改完文件自动格式化",
    desc: "每次写入之后跑一遍格式化命令，失败了只提醒，不打断",
    event: "PostToolUse",
    match: "edit_file|write_file|multi_edit",
    command: "gofmt -w . 2>/dev/null || true",
  },
  {
    id: "guard-secrets",
    title: "写密钥文件前问我一声",
    desc: "碰 .env 之类的路径时挡下来，由你决定要不要放行",
    event: "PreToolUse",
    match: "edit_file|write_file",
    command: `grep -q '"\\.env' <<< "$REASONIX_HOOK_PAYLOAD" && exit 2 || exit 0`,
  },
  {
    id: "test-before-stop",
    title: "收工前跑一遍测试",
    desc: "一轮结束时跑测试，红了会作为提醒显示出来",
    event: "Stop",
    command: "go test ./... 2>&1 | tail -5",
  },
];

const SCOPE_LABEL = { user: "我的", project: "这个项目" } as const;

interface Props {
  port: AgentPort;
  onChanged: () => void;
}

export function Hooks({ port, onChanged }: Props) {
  const [cat, setCat] = useState<HookCatalog | null>(null);
  const [scope, setScope] = useState<"user" | "project">("user");
  const [expert, setExpert] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [tried, setTried] = useState<Record<string, HookDryRun>>({});

  const reload = () => {
    port
      .hooks()
      .then((c) => {
        setCat(c);
        if (!c.projectPath && scope === "project") setScope("user");
      })
      .catch(() => setCat(null));
  };
  useEffect(reload, [port]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!cat) return <div className="empty">{t("读不到 hooks 配置。")}</div>;

  const mine = cat.hooks.filter((h) => h.scope === (scope === "user" ? "global" : "project"));
  const plugin = cat.hooks.filter((h) => h.scope === "plugin");
  const broken = cat.sources.filter((s) => s.status === "malformed" || s.status === "unreadable");

  const save = async (next: HookEntry[]) => {
    setBusy("save");
    setError("");
    try {
      await port.saveHooks(scope, next);
      reload();
      onChanged();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  const has = (r: Recipe) => mine.some((h) => h.event === r.event && h.command === r.command);
  const toggleRecipe = (r: Recipe) => {
    const on = has(r);
    const next = on
      ? mine.filter((h) => !(h.event === r.event && h.command === r.command))
      : [...mine, { event: r.event, match: r.match, command: r.command, description: r.title, scope }];
    void save(next as HookEntry[]);
  };

  const tryOne = async (h: HookEntry, key: string) => {
    setBusy(key);
    setError("");
    try {
      const res = await port.dryRunHook(h);
      setTried((t) => ({ ...t, [key]: res }));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  return (
    <div className="hooks">
      <div className="scope" role="radiogroup" aria-label={t("这些规则写在哪")}>
        {(["user", "project"] as const).map((s) => (
          <button
            key={s}
            role="radio"
            aria-checked={scope === s}
            disabled={s === "project" && !cat.projectPath}
            onClick={() => setScope(s)}
          >
            {t(SCOPE_LABEL[s])}
            <i>{t(s === "user" ? "只在这台机器上" : "写进仓库，clone 的人也会拿到")}</i>
          </button>
        ))}
      </div>
      <p className="path">{scope === "user" ? cat.globalPath : cat.projectPath || t("没有打开项目")}</p>

      {broken.map((s) => (
        <div className="why" key={s.path}>
          {s.path} {t("读不了：")}{s.parseError || s.status}
        </div>
      ))}

      <div className="recipes">
        {RECIPES.map((r) => (
          <div className="recipe" key={r.id} data-on={has(r) ? "" : undefined}>
            <div className="tx">
              <span className="lb">{t(r.title)}</span>
              <span className="ds">{t(r.desc)}</span>
            </div>
            <button
              className="sw"
                data-action="hooks.recipe"
                data-target={r.id}
              role="switch"
              aria-checked={has(r)}
              aria-label={t(r.title)}
              disabled={!!busy}
              onClick={() => toggleRecipe(r)}
            >
              <i />
            </button>
          </div>
        ))}
      </div>

      <button className="more" aria-expanded={expert} onClick={() => setExpert((v) => !v)}>
        {t(expert ? "收起" : "自己写一条")}
        <span className="c">{t("{n} 条规则", { n: mine.length })}</span>
      </button>

      {expert && (
        <Expert
          cat={cat}
          scope={scope}
          rules={mine}
          plugin={plugin}
          busy={busy}
          tried={tried}
          onSave={save}
          onTry={tryOne}
        />
      )}
      {error && <div className="why">{error}</div>}
    </div>
  );
}

function Expert({
  cat, scope, rules, plugin, busy, tried, onSave, onTry,
}: {
  cat: HookCatalog;
  scope: "user" | "project";
  rules: HookEntry[];
  plugin: HookEntry[];
  busy: string;
  tried: Record<string, HookDryRun>;
  onSave: (next: HookEntry[]) => void;
  onTry: (h: HookEntry, key: string) => void;
}) {
  const [draft, setDraft] = useState<HookEntry[]>(rules);
  useEffect(() => setDraft(rules), [rules]);

  const patch = (i: number, p: Partial<HookEntry>) =>
    setDraft((d) => d.map((h, k) => (k === i ? { ...h, ...p } : h)));
  const info = (event: string) => cat.events.find((e) => e.name === event);

  return (
    <div className="expert">
      {draft.map((h, i) => {
        const meta = info(h.event);
        const key = `${scope}-${i}`;
        const res = tried[key];
        return (
          <div className="rule" key={i} data-blocking={meta?.blocking ? "" : undefined}>
            <div className="line">
              <select value={h.event} onChange={(e) => patch(i, { event: e.target.value })}>
                {cat.events.map((e) => (
                  <option key={e.name} value={e.name}>
                    {e.name}
                  </option>
                ))}
              </select>
              {meta?.usesMatch && (
                <input
                  className="match"
                  value={h.match ?? ""}
                  placeholder={t("匹配哪些工具（正则，留空=全部）")}
                  onChange={(e) => patch(i, { match: e.target.value })}
                />
              )}
              {meta?.blocking && <i className="warn">{t("能挡住 agent")}</i>}
                <button className="act ghost" data-action="hooks.remove" onClick={() => onSave(draft.filter((_, k) => k !== i))}>
                {t("删掉")}
              </button>
            </div>
            <input
              className="cmd"
              value={h.command}
              placeholder={t("要运行的命令")}
              onChange={(e) => patch(i, { command: e.target.value })}
            />
            {h.issues?.map((msg) => (
              <div className="why" key={msg}>
                {msg}
              </div>
            ))}
            <div className="line">
                <button className="act" data-action="hooks.run-once" disabled={busy === key} onClick={() => onTry(h, key)}>
                {t(busy === key ? "运行中…" : "试跑一次")}
              </button>
              {/* The command really runs. Saying so is the difference between a
                  button that checks syntax and one that might delete a file. */}
              <span className="note">{t("会真的执行这条命令")}</span>
            </div>
            {res && <DryRun res={res} blocking={!!meta?.blocking} />}
          </div>
        );
      })}

      <div className="line">
        <button
          className="act"
          onClick={() => setDraft((d) => [...d, { event: "PostToolUse", command: "", scope }])}
        >
          {t("加一条")}
        </button>
            <button className="act" data-action="hooks.save" data-primary disabled={busy === "save"} onClick={() => onSave(draft)}>
          {t(busy === "save" ? "保存中…" : "保存")}
        </button>
      </div>

      {plugin.length > 0 && (
        <div className="fromplugin">
          <span className="lb">{t("插件带来的 {n} 条", { n: plugin.length })}</span>
          {plugin.map((h, i) => (
            <div className="hookrow" key={i}>
              <span className="ev">{h.event}</span>
              <span className="cm">{h.command}</span>
              <span className="sc">{t("只读")}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// The exit code is not the answer; what it does on this event is. A dry run that
// only printed "exit 2" would leave the user exactly as unsure as before.
function DryRun({ res, blocking }: { res: HookDryRun; blocking: boolean }) {
  const verdict = res.timedOut
    ? "超时了，这一步会被当成失败"
    : res.blocks
      ? "这一次它会挡住这一步"
      : res.decision === "block" && !blocking
        ? "这个事件挡不住东西，只会提醒你"
        : res.decision === "warn"
          ? "会作为提醒显示出来"
          : "会放行";
  return (
    <span className="dryrun" data-blocks={res.blocks ? "" : undefined}>
      <b>{verdict}</b>
      <span className="meta">
        exit {res.exitCode} · {res.durationMs}ms
      </span>
      {(res.stdout || res.stderr) && <span className="out">{(res.stdout || res.stderr || "").slice(0, 160)}</span>}
    </span>
  );
}
