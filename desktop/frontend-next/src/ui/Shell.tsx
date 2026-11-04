import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, ShellOption, ShellSettings } from "../port/port";
import { reason, savedNotApplied } from "../i18n/kernel";
import { SaveNote } from "./SaveNote";

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
  pwsh: "app.understands_and_but_the_syntax",
  powershell: "app.does_not_accept_or_chained",
};

const label = (o: ShellOption) => LABEL[o.name] ?? o.name;

// A typed-in path still has to say which family it belongs to, because that is
// what decides how the command is spelled — and the file name is the only clue
// on offer before it has been run.
const kindOf = (path: string) => (/pwsh/i.test(path) ? "pwsh" : /powershell/i.test(path) ? "powershell" : "bash");

export function Shell({ port, onChanged }: { port: AgentPort; onChanged?: () => void }) {
  const [s, setS] = useState<ShellSettings | null>(null);
  const [busy, setBusy] = useState("");
  const [note, setNote] = useState<{ text: string; unapplied: boolean } | null>(null);
  const [custom, setCustom] = useState("");
  const [launcher, setLauncher] = useState("");

  const load = useCallback(() => {
    port
      .shell()
      .then((v) => {
        setS(v);
        setCustom(v.path ?? "");
        setLauncher((v.args ?? []).join("\n"));
      })
      .catch(() => setS(null));
  }, [port]);

  useEffect(load, [load]);

  if (!s) return <div className="empty">{t("app.could_not_read_the_shell")}</div>;

  const options = s.options ?? [];
  // Two bashes on one machine are two different programs, so picking one has to
  // pin its path. With only one, the name alone survives a PATH that moves.
  const pin = (o: ShellOption) => options.filter((x) => x.prefer === o.prefer).length > 1;
  const save = async (what: string, prefer: string, path: string, args?: string[]) => {
    setBusy(what);
    setNote(null);
    try {
      const next = await (args && args.length > 0 ? port.saveShell(prefer, path, args) : port.saveShell(prefer, path));
      setS(next);
      setCustom(next.path ?? "");
      setLauncher((next.args ?? []).join("\n"));
      onChanged?.();
    } catch (e) {
      setNote({ text: reason(e), unapplied: savedNotApplied(e) });
    } finally {
      setBusy("");
    }
  };

  const auto = s.prefer === "auto" && !s.path;
  const picked = (o: ShellOption) => !auto && s.effective.path === o.path;
  // A Windows host without bash is the case worth naming: the model's POSIX
  // habits are wrong there, and one install is what changes that.
  const noBash = s.platform === "windows" && !options.some((o) => o.prefer === "bash");
  // One argument per line: an argument may hold spaces (a distro name), and the
  // kernel runs the list as written, with no shell in between.
  const launcherArgs = launcher.split("\n").map((a) => a.trim()).filter((a) => a !== "");
  const fellBack = (s.args?.length ?? 0) > 0 && !s.argv;

  return (
    <div className="shell">
      <div className="kv">
        <span className="k">{t("common.in_effect")}</span>
        <span className="v">
          {label(s.effective)}
          {s.effective.version ? ` ${s.effective.version}` : ""} · {s.effective.path || "—"}
        </span>
      </div>

      {s.argv && (
        <div className="kv">
          <span className="k">{t("app.launch_command")}</span>
          <span className="v"><code>{s.argv.join(" ")} -c …</code></span>
        </div>
      )}
      {fellBack && <p className="note" data-tone="warn">{t("app.the_saved_launcher_arguments_no")}</p>}

      {/* One shape for "pick one of a few", the same as every other such choice
          in this pane. The executable's path is too long for a segment, and it
          is not what you pick by anyway — it rides the 当前生效 row above and
          the note below, where it can be read rather than scanned. */}
      <div className="seg" data-text role="radiogroup" aria-label={t("common.which_program_runs_commands")}>
        <button role="radio" data-action="shell.executor" aria-checked={auto} disabled={!!busy} onClick={() => void save("auto", "auto", "")}>
          {t("common.auto")}
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
            {t("app.finds_one_itself_preferring_a")} {label(s.auto)}
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
          {t("app.there_is_no_bash_on")}
        </p>
      )}

      <details className="pinpath">
        <summary>
          <span className="fold">{t("app.point_at_an_executable")}</span>
        </summary>
        <p className="note">
          {t("app.a_bash_you_built_yourself")}
        </p>
        <div className="fields">
          <label className="grow full">
            <span>{t("app.path_to_the_executable")}</span>
            <input
              value={custom}
              placeholder={s.auto.path}
              onChange={(e) => setCustom(e.target.value)}
            />
          </label>
        </div>
        {s.platform === "windows" && (
          <div className="fields">
            <label className="grow full">
              <span>{t("app.launcher_arguments_optional_one_per")}</span>
              <textarea
                data-action="shell.custom-args"
                rows={4}
                value={launcher}
                placeholder={"-d\nUbuntu\n--\n/bin/bash"}
                onChange={(e) => setLauncher(e.target.value)}
              />
              <small className="note">{t("app.for_example_wsl_exe_needs")}</small>
            </label>
          </div>
        )}
        <div className="acts">
          {s.path && (
            <button className="act" data-action="shell.custom-path" disabled={!!busy} onClick={() => void save("clear", s.prefer, "")}>
              {t("app.clear_it")}
            </button>
          )}
          <button className="act" data-action="shell.custom-path" data-primary disabled={!!busy || !custom.trim()}
            onClick={() => void save("custom", launcherArgs.length > 0 ? "bash" : kindOf(custom), custom.trim(), launcherArgs)}>
            {t(busy === "custom" ? "common.verifying" : "app.save_this_path")}
          </button>
        </div>
      </details>

      <SaveNote note={note} />
    </div>
  );
}
