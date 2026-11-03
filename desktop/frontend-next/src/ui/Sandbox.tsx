import { useEffect, useId, useState } from "react";
import { t } from "../i18n";
import { reason, say } from "../i18n/kernel";
import type { AgentPort, SandboxSettings } from "../port/port";
import { Switch } from "./Switch";
import { WorkspaceFolders } from "./WorkspaceFolders";

const MODES: [string, string][] = [
  ["enforce", "sandbox.jailed"],
  ["off", "sandbox.unconfined"],
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

  if (!box) return <div className="empty">{t("sandbox.cannotReadTheSandboxConfig")}</div>;

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
          <span className="t">{t("sandbox.thisProjectShipsItsOwn")}</span>
          <span className="why">
            {t("sandbox.declaresASandboxTooAnd", { path: box.shadowedBy })}
          </span>
        </div>
      )}

      <div className="sec">
        <h3>{t("sandbox.whereWritesMayLand")}</h3>
        <p className="note">
          {t("sandbox.evenAnApprovedWriteCan")}
        </p>
        <div className="fields">
          <label className="grow">
            <span>{t("sandbox.workspaceRoot")}</span>
            <input
              value={root}
                data-action-keydown="sandbox.workspace-root"
              placeholder={t("sandbox.emptyTheSessionSOwn")}
              onChange={(e) => setRoot(e.target.value)}
              onBlur={() => root !== box.workspaceRoot && void save("root", { ...box, workspaceRoot: root })}
              onKeyDown={(e) => e.key === "Enter" && void save("root", { ...box, workspaceRoot: root })}
            />
          </label>
          <button className="act" data-action="sandbox.workspace-root" data-value="browse" disabled={!!busy} onClick={() => void pick()}>
            {t("common.chooseAFolder")}
          </button>
        </div>

        <div className="extra">
          <div className="sublb">{t("sandbox.alsoWritable")}</div>
          {box.allowWrite.map((p) => (
            <div className="prule" key={p}>
              <code>{p}</code>
              <button
                className="act ghost"
                data-action="sandbox.remove-write-root"
                data-target={p}
                disabled={!!busy}
                aria-label={t("sandbox.stopAllowingWritesTo", { path: p })}
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
              placeholder={t("sandbox.openOneMoreWritableDirectory")}
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
          <span className="k">{t("sandbox.actuallyWritable")}</span>
          <span className="v">
            {box.effectiveWriteRoots.length ? box.effectiveWriteRoots.map((r) => <code key={r}>{r}</code>) : "—"}
          </span>
        </div>

        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("sandbox.protectChangedFiles")}</span>
            <span className="ds">{t("sandbox.ifAFileTheModel")}</span>
          </span>
          <Switch
            data-action="sandbox.protect-changed-files"
            on={box.protectChangedFiles}
            busy={busy === "protect"}
            label={t("sandbox.protectChangedFiles")}
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
      <h3>{t("sandbox.howCommandsRun")}</h3>
      {box.available && box.platform === "windows" ? (
        <p className="note">
          {t("sandbox.onWindowsOnceOnCommands")}
        </p>
      ) : box.available ? (
        <p className="note">
          {t("sandbox.insideTheSandboxACommand")}
        </p>
      ) : (
        <div className="find" data-lvl="warn" role="status" id={whyId}>
          <span className="t">{t("sandbox.thisMachineHasNoUsable")}</span>
          <span className="why">
            {why || t("sandbox.commandsRunUnconfinedTheWritable")}
          </span>
        </div>
      )}
      {/* A host that can never jail still shows the option, because this is the
          only place a reader learns the mode exists and why it is out. The
          reason hangs on the option itself: a card above a live-looking button
          reads as commentary rather than as the cause. */}
      <div className="seg" data-text role="radiogroup" aria-label={t("sandbox.howCommandsRun")}>
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
          <span className="k">{t("common.actuallyInForce")}</span>
          <span className="v">
            {t("sandbox.configuredAsRunningAsOn", { want: label(written), now: label(mode) })}
          </span>
        </div>
      )}
      {mode === "enforce" && (
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("sandbox.allowNetworkInsideTheSandbox")}</span>
            <span className="ds">{t("sandbox.withThisOffInstallingDependencies")}</span>
          </span>
          <Switch data-action="sandbox.network" on={box.network} busy={busy === "net"} label={t("sandbox.allowNetworkInsideTheSandbox")} onClick={onNetwork} />
        </div>
      )}
    </div>
  );
}
