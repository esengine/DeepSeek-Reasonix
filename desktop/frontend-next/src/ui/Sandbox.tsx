import { useEffect, useId, useState } from "react";
import { t } from "../i18n";
import { reason, say } from "../i18n/kernel";
import type { AgentPort, SandboxSettings } from "../port/port";
import { Switch } from "./Switch";
import { WorkspaceFolders } from "./WorkspaceFolders";

const MODES: [string, string][] = [
  ["enforce", "safety.jailed"],
  ["off", "safety.unconfined"],
];

// Two questions, in the order they matter: how far a write reaches, and whether
// the command that makes it runs jailed. The first is always in force; the
// second needs an OS sandbox and says so where there is none.
export function Sandbox({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [box, setBox] = useState<SandboxSettings | null>(null);
  const [root, setRoot] = useState("");
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    port
      .sandbox()
      .then((s) => {
        setBox(s);
        setRoot(s.workspaceRoot);
      })
      .catch(() => setBox(null));
  }, [port]);

  if (!box) return <div className="empty">{t("safety.cannot_read_the_sandbox_config")}</div>;

  const save = async (what: string, next: SandboxSettings) => {
    setBusy(what);
    setError("");
    try {
      const saved = await port.saveSandbox(next);
      setBox(saved);
      setRoot(saved.workspaceRoot);
      onChanged();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  const pick = async () => {
    const path = await port.pickFolder().catch(() => null);
    if (path) void save("root", { ...box, workspaceRoot: path });
  };

  return (
    <div className="box">
      {box.shadowedBy && (
        <div className="find" data-lvl="warn" role="status">
          <span className="t">{t("safety.this_project_ships_its_own_2")}</span>
          <span className="why">
            {t("safety.path_declares_a_sandbox_too", { path: box.shadowedBy })}
          </span>
        </div>
      )}

      <div className="sec">
        <h3>{t("safety.where_writes_may_land")}</h3>
        <p className="note">
          {t("safety.even_an_approved_write_can")}
        </p>
        <div className="fields">
          <label className="grow">
            <span>{t("safety.workspace_root")}</span>
            <input
              value={root}
                data-action-keydown="sandbox.workspace-root"
              placeholder={t("safety.empty_the_sessions_own_directory")}
              onChange={(e) => setRoot(e.target.value)}
              onBlur={() => root !== box.workspaceRoot && void save("root", { ...box, workspaceRoot: root })}
              onKeyDown={(e) => e.key === "Enter" && void save("root", { ...box, workspaceRoot: root })}
            />
          </label>
          <button className="act" data-action="sandbox.workspace-root" data-value="browse" disabled={!!busy} onClick={() => void pick()}>
            {t("common.choose_a_folder")}
          </button>
        </div>

        <div className="extra">
          <div className="sublb">{t("safety.also_writable")}</div>
          {box.allowWrite.map((p) => (
            <div className="prule" key={p}>
              <code>{p}</code>
              <button
                className="act ghost"
                data-action="sandbox.remove-write-root"
                data-target={p}
                disabled={!!busy}
                aria-label={t("safety.stop_allowing_writes_to_path", { path: p })}
                onClick={() => void save("extra", { ...box, allowWrite: box.allowWrite.filter((x) => x !== p) })}
              >
                {t("common.delete")}
              </button>
            </div>
          ))}
          {/* Same row shape as the entries above it: one thing, then what you
              can do to it. A differently-built add box read as a third kind of
              control for what is the same list. */}
          <div className="prule" data-add="">
            <input
              value={draft}
                data-action-keydown="sandbox.add-write-root"
              placeholder={t("safety.open_one_more_writable_directory")}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key !== "Enter" || !draft.trim()) return;
                void save("extra", { ...box, allowWrite: [...box.allowWrite, draft.trim()] });
                setDraft("");
              }}
            />
            <button
              className="act"
              data-action="sandbox.add-write-root"
              disabled={!!busy || !draft.trim()}
              onClick={() => {
                void save("extra", { ...box, allowWrite: [...box.allowWrite, draft.trim()] });
                setDraft("");
              }}
            >
              {t("common.add")}
            </button>
          </div>
        </div>

        {/* The expansion, not the spelling: an empty root above is still a real
            directory down here, and ${VAR} in a path is answered rather than
            echoed back. */}
        <div className="kv">
          <span className="k">{t("safety.actually_writable")}</span>
          <span className="v">
            {box.effectiveWriteRoots.length ? box.effectiveWriteRoots.map((r) => <code key={r}>{r}</code>) : "—"}
          </span>
        </div>

        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("safety.protect_changed_files")}</span>
            <span className="ds">{t("safety.if_a_file_the_model")}</span>
          </span>
          <Switch
            data-action="sandbox.protect-changed-files"
            on={box.protectChangedFiles}
            busy={busy === "protect"}
            label={t("safety.protect_changed_files")}
            onClick={() => void save("protect", { ...box, protectChangedFiles: !box.protectChangedFiles })}
          />
        </div>
      </div>

      <WorkspaceFolders port={port} onChanged={onChanged} />

      <CommandMode
        box={box}
        busy={busy}
        onPick={(bash) => void save("bash", { ...box, bash })}
        onNetwork={() => void save("net", { ...box, network: !box.network })}
      />
      {error && <div className="why">{error}</div>}
    </div>
  );
}

// How commands run. Rendered from what the confiner will actually do rather
// than from the word in the config file: an unset mode enforces everywhere but
// Windows, which runs unconfined however the file reads. A pane drawing the
// file would put the wrong posture on the one screen that reports it.
export function CommandMode({
  box,
  busy,
  onPick,
  onNetwork,
}: {
  box: SandboxSettings;
  busy: string;
  onPick: (bash: string) => void;
  onNetwork: () => void;
}) {
  const whyId = useId();
  const mode = box.effectiveBash || box.bash || "off";
  const written = box.bash.trim();
  const overruled = written !== "" && written !== mode;
  const label = (id: string) => t(MODES.find(([m]) => m === id)?.[1] ?? id);
  const why = say({ code: box.whyCode, error: box.why }, "");

  return (
    <div className="sec">
      <h3>{t("safety.how_commands_run")}</h3>
      {box.available && box.platform === "windows" ? (
        <p className="note">
          {t("safety.on_windows_once_on_commands")}
        </p>
      ) : box.available ? (
        <p className="note">
          {t("safety.inside_the_sandbox_a_command")}
        </p>
      ) : (
        <div className="find" data-lvl="warn" role="status" id={whyId}>
          <span className="t">{t("safety.this_machine_has_no_usable")}</span>
          <span className="why">
            {why || t("safety.commands_run_unconfined_the_writable")}
          </span>
        </div>
      )}
      {/* A host that can never jail still shows the option, because this is the
          only place a reader learns the mode exists and why it is out. The
          reason hangs on the option itself: a card above a live-looking button
          reads as commentary rather than as the cause. */}
      <div className="seg" data-text role="radiogroup" aria-label={t("safety.how_commands_run")}>
        {MODES.map(([id, name]) => {
          const locked = id === "enforce" && !box.available;
          return (
            <button
              key={id}
              role="radio"
              data-action="sandbox.mode"
              data-value={id}
              aria-checked={mode === id}
              disabled={!!busy || locked}
              data-locked={locked ? "" : undefined}
              aria-describedby={locked ? whyId : undefined}
              title={locked ? why : undefined}
              onClick={() => onPick(id)}
            >
              {t(name)}
            </button>
          );
        })}
      </div>
      {overruled && (
        <div className="kv">
          <span className="k">{t("common.actually_in_force")}</span>
          <span className="v">
            {t("safety.configured_as_want_running_as", { want: label(written), now: label(mode) })}
          </span>
        </div>
      )}
      {mode === "enforce" && (
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("safety.allow_network_inside_the_sandbox")}</span>
            <span className="ds">{t("safety.with_this_off_installing_dependencies")}</span>
          </span>
          <Switch data-action="sandbox.network" on={box.network} busy={busy === "net"} label={t("safety.allow_network_inside_the_sandbox")} onClick={onNetwork} />
        </div>
      )}
    </div>
  );
}
