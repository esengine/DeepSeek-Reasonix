import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, ShellOption, ShellSettings } from "../port/port";
import { reason } from "../i18n/kernel";

// The product name, not the executable: "powershell.exe" is what the file is
// called, "Windows PowerShell" is what the user installed.
const LABEL: Record<string, string> = {
  bash: "bash",
  "git-bash": "Git Bash",
  pwsh: "PowerShell 7",
  powershell: "Windows PowerShell",
};

// What changes for the person reading it. Only the 5.1 line is a warning: it is
// the one interpreter that refuses syntax the model writes by habit.
const NOTE: Record<string, string> = {
  pwsh: "认得 && 和 ||，但语法仍然是 PowerShell，不是 bash",
  powershell: "不认 && 和 ||，链式命令得拆成两条",
};

const label = (o: ShellOption) => LABEL[o.name] ?? o.name;

// A typed-in path still has to say which family it belongs to, because that is
// what decides how the command is spelled — and the file name is the only clue
// on offer before it has been run.
const kindOf = (path: string) => (/pwsh/i.test(path) ? "pwsh" : /powershell/i.test(path) ? "powershell" : "bash");

export function Shell({ port, onChanged }: { port: AgentPort; onChanged?: () => void }) {
  const [s, setS] = useState<ShellSettings | null>(null);
  const [busy, setBusy] = useState("");
  const [failed, setFailed] = useState("");
  const [custom, setCustom] = useState("");

  const load = useCallback(() => {
    port
      .shell()
      .then((v) => {
        setS(v);
        setCustom(v.path ?? "");
      })
      .catch(() => setS(null));
  }, [port]);

  useEffect(load, [load]);

  if (!s) return <div className="empty">{t("读不到 shell 配置。")}</div>;

  const options = s.options ?? [];
  // Two bashes on one machine are two different programs, so picking one has to
  // pin its path. With only one, the name alone survives a PATH that moves.
  const pin = (o: ShellOption) => options.filter((x) => x.prefer === o.prefer).length > 1;
  const save = async (what: string, prefer: string, path: string) => {
    setBusy(what);
    setFailed("");
    try {
      const next = await port.saveShell(prefer, path);
      setS(next);
      setCustom(next.path ?? "");
      onChanged?.();
    } catch (e) {
      setFailed(reason(e));
    } finally {
      setBusy("");
    }
  };

  const auto = s.prefer === "auto" && !s.path;
  const picked = (o: ShellOption) => !auto && s.effective.path === o.path;
  // A Windows host without bash is the case worth naming: the model's POSIX
  // habits are wrong there, and one install is what changes that.
  const noBash = s.platform === "windows" && !options.some((o) => o.prefer === "bash");

  return (
    <div className="shell">
      <div className="kv">
        <span className="k">{t("当前生效")}</span>
        <span className="v">
          {label(s.effective)}
          {s.effective.version ? ` ${s.effective.version}` : ""} · {s.effective.path || "—"}
        </span>
      </div>

      {/* One shape for "pick one of a few", the same as every other such choice
          in this pane. The executable's path is too long for a segment, and it
          is not what you pick by anyway — it rides the 当前生效 row above and
          the note below, where it can be read rather than scanned. */}
      <div className="seg" data-text role="radiogroup" aria-label={t("命令交给谁执行")}>
        <button role="radio" data-action="shell.executor" aria-checked={auto} disabled={!!busy} onClick={() => void save("auto", "auto", "")}>
          {t("自动")}
        </button>
        {options.map((o) => (
          <button key={o.path} role="radio" data-action="shell.executor" aria-checked={picked(o)} disabled={!!busy}
            onClick={() => void save(o.path, o.prefer, pin(o) ? o.path : "")}>
            {label(o)}
            {o.version && <i className="ver">{o.version}</i>}
          </button>
        ))}
      </div>
      <p className="note">
        {auto ? (
          <>
            {t("自己找，优先真 bash。这台机器上会选到")} {label(s.auto)}
          </>
        ) : (
          <>
            <code>{s.effective.path}</code>
            {NOTE[s.effective.name] && <> · {t(NOTE[s.effective.name])}</>}
          </>
        )}
      </p>

      {noBash && (
        <p className="note">
          {t("这台机器上没有 bash，所以命令只能按 PowerShell 写。装一个 Git for Windows就会多出 Git Bash 这一项 —— WSL 里的那个不算，它看到的是 /mnt 下的另一套路径，够不着这个工作目录。")}
        </p>
      )}

      <details className="pinpath">
        <summary>
          <span className="fold">{t("指定一个可执行文件")}</span>
        </summary>
        <p className="note">
          {t("自己编的 bash、MSYS2、装在别处的 pwsh 都填这里。保存前会真的拿它跑一条命令，跑不起来就不会写进配置。")}
        </p>
        <div className="fields">
          <label className="grow full">
            <span>{t("可执行文件路径")}</span>
            <input
              value={custom}
              placeholder={s.auto.path}
              onChange={(e) => setCustom(e.target.value)}
            />
          </label>
        </div>
        <div className="acts">
          {s.path && (
            <button className="act" data-action="shell.custom-path" disabled={!!busy} onClick={() => void save("clear", s.prefer, "")}>
              {t("取消指定")}
            </button>
          )}
          <button className="act" data-action="shell.custom-path" data-primary disabled={!!busy || !custom.trim()}
            onClick={() => void save("custom", kindOf(custom), custom.trim())}>
            {t(busy === "custom" ? "验证中…" : "保存这个路径")}
          </button>
        </div>
      </details>

      {failed && (
        <div className="find" data-lvl="warn" role="alert">
          <span className="t">{t("没换成")}</span>
          <span className="why">{failed}</span>
        </div>
      )}
    </div>
  );
}
