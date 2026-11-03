import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import type { HubPort } from "../port/hub";
import type { RemoteHost, RemoteHostEdit, RemoteProbe, WSLDistros } from "../port/remote";
import { say } from "../i18n/kernel";
import { toggleHiddenHost, useHiddenHosts } from "../state/remotehide";
import { RemoteDirs } from "./RemoteDirs";
import { Switch } from "./Switch";

interface Props {
  hub: HubPort;
  onError: (e: unknown) => void;
}

const STATUS_LABEL: Record<string, string> = {
  idle: "common.notConnected",
  connecting: "common.connecting",
  connected: "common.connected2",
  reconnecting: "remotes.reconnecting",
  degraded: "remotes.aForwardDidNotAttach",
  stopped: "common.disconnected",
};

// One machine holds several projects. The kernel folds the two stored fields
// into one default-first list, so a row that only ever set the single workspace
// arrives here as a list of one rather than as a case to handle.
export function workspacesOf(host: RemoteHost | null): string[] {
  if (host?.workspaces?.length) return host.workspaces;
  return host?.workspace ? [host.workspace] : [];
}

// A saved row goes back in full: the endpoint replaces the entry, so a form
// that sent only what it displays would blank the rest.
function draftOf(host: RemoteHost | null): RemoteHostEdit {
  return {
    name: host?.name ?? "",
    kind: host?.kind ?? "",
    distro: host?.distro ?? "",
    host: host?.host ?? "",
    port: host?.port ?? 0,
    user: host?.user ?? "",
    identityFile: host?.identityFile ?? "",
    proxyJump: host?.proxyJump ?? "",
    workspaces: workspacesOf(host),
    serveInstall: host?.serveInstall ?? "",
    provider: host?.provider ?? "",
    useSSHConfig: host?.useSSHConfig ?? false,
    passphraseEnv: host?.passphraseEnv ?? "",
    passwordEnv: host?.passwordEnv ?? "",
    disabled: host?.disabled ?? false,
  };
}

export function Remotes({ hub, onError }: Props) {
  const [hosts, setHosts] = useState<RemoteHost[]>([]);
  const [candidates, setCandidates] = useState<string[]>([]);
  // The distributions on this machine, or nothing to choose from: a machine
  // with no WSL never shows the choice, so the form it has always had is unchanged.
  const [wsl, setWSL] = useState<WSLDistros | null>(null);
  const [draft, setDraft] = useState<RemoteHostEdit | null>(null);
  // The folder being typed, kept out of the draft: it is not part of the row
  // until it is added, and a save must not smuggle a half-typed path in.
  const [dir, setDir] = useState("");
  // The name a draft is editing, empty for a new one. Kept apart from the
  // draft's own name so renaming stays possible later without losing the row.
  const [editing, setEditing] = useState("");
  const [confirm, setConfirm] = useState("");
  const [busy, setBusy] = useState("");
  const hidden = useHiddenHosts();
  // What each machine answered when asked. Kept per host so a second probe
  // does not blank the first one's answer while it runs.
  const [probes, setProbes] = useState<Record<string, RemoteProbe>>({});
  const [probing, setProbing] = useState("");
  // Browsing dials, and only a row already in the book has an address to dial.
  // A draft being typed has nowhere to go yet, so it types the path instead.
  const [picking, setPicking] = useState(false);

  const reload = useCallback(async () => {
    try {
      const [book, aliases, distros] = await Promise.all([hub.remoteHosts(), hub.remoteCandidates().catch(() => []), hub.wslDistros().catch(() => null)]);
      setHosts(book ?? []);
      setCandidates(aliases);
      setWSL(distros?.available ? distros : null);
    } catch (e) {
      onError(e);
    }
  }, [hub, onError]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const save = async (entry: RemoteHostEdit) => {
    setBusy(entry.name);
    try {
      await hub.saveRemoteHost(entry);
      setDraft(null);
      setEditing("");
      setDir("");
      await reload();
    } catch (e) {
      onError(e);
    } finally {
      setBusy("");
    }
  };

  const drop = async (name: string) => {
    setConfirm("");
    setBusy(name);
    try {
      await hub.removeRemoteHost(name);
      await reload();
    } catch (e) {
      onError(e);
    } finally {
      setBusy("");
    }
  };

  // The list is edited whole and saved with the row. Head = default, so
  // promoting one is a move rather than a second field that can disagree.
  const setDirs = (next: string[]) => setDraft((d) => (d ? { ...d, workspaces: next } : d));
  const addDir = () => {
    const path = dir.trim();
    if (!path || !draft) return;
    if (!(draft.workspaces ?? []).includes(path)) setDirs([...(draft.workspaces ?? []), path]);
    setDir("");
  };

  const isWSL = draft?.kind === "wsl";

  const field = (key: keyof RemoteHostEdit, label: string, placeholder?: string) => (
    <label className="rmtf">
      <span>{label}</span>
      <input
        value={String(draft?.[key] ?? "")}
        placeholder={placeholder ? t(placeholder) : undefined}
        onChange={(ev) => setDraft((d) => (d ? { ...d, [key]: ev.target.value } : d))}
      />
    </label>
  );

  return (
    <div className="rmtbook">
      {hosts.map((host) => (
        <div className="rmtrow" key={host.name} data-state={host.status}>
          <div className="hd">
            <i className="rmtpip" aria-hidden="true" />
            <b>{host.name}</b>
            <span className="tg">{host.target}</span>
            <span className="st">{t(STATUS_LABEL[host.status] ?? host.status)}</span>
            <button
              className="rmtlnk"
              data-action="remote.probe"
              data-target={host.name}
              disabled={probing === host.name}
              onClick={() => {
                setProbing(host.name);
                hub
                  .probeRemote(host.name)
                  .then((rep) => setProbes((p) => ({ ...p, [host.name]: rep })))
                  .catch(onError)
                  .finally(() => setProbing(""));
              }}
            >
              {t(probing === host.name ? "remotes.asking" : "common.testIt")}
            </button>
            <button
              className="rmtlnk"
              onClick={() => {
                setEditing(host.name);
                setDraft(draftOf(host));
              }}
            >
              {t("common.edit")}
            </button>
            <button className="rmtlnk" data-action="remote.hide" data-target={host.name} onClick={() => toggleHiddenHost(host.name)}>
              {t(hidden.includes(host.name) ? "remotes.showInTheRail" : "remotes.hideFromTheRail")}
            </button>
            {host.disabled ? <span className="st">{t("common.suspended")}</span> : null}
            <Switch
              data-action="remote.enable"
              data-target={host.name}
              on={!host.disabled}
              busy={busy === host.name}
              label={t(host.disabled ? "remotes.turnThisMachineOn" : "remotes.turnThisMachineOff")}
              onClick={() => void save({ ...draftOf(host), disabled: !host.disabled })}
            />
            <button className="rmtlnk" data-danger="" disabled={!!busy} onClick={() => setConfirm(host.name)}>
              {t("common.remove")}
            </button>
          </div>
          <div className="sub">
            {host.workspace ? <span dir="ltr">{host.workspace}</span> : <span className="dim">{t("common.noDefaultWorkspace")}</span>}
            {workspacesOf(host).length > 1 ? (
              <span className="tag">{t("remotes.moreProjects", { n: workspacesOf(host).length - 1 })}</span>
            ) : null}
            {host.kind === "wsl" ? <span className="tag">{t("remotes.wSLDistribution")}</span> : null}
            {host.useSSHConfig ? <span className="tag">{t("remotes.followsSshConfig")}</span> : null}
            {host.forwards ? <span className="tag">{t("remotes.forwards", { n: host.forwards })}</span> : null}
            {host.panes ? <span className="tag">{t("remotes.panes", { n: host.panes })}</span> : null}
          </div>
          {probes[host.name] ? <ProbeCard probe={probes[host.name]} host={host.name} /> : null}
          {confirm === host.name && (
            <div className="rmtconfirm" role="alertdialog">
              <span>{t("remotes.removeFromTheListNothing", { name: host.name })}</span>
              <button onClick={() => setConfirm("")}>{t("common.cancel")}</button>
              <button data-danger="" data-action="remotes.remove" data-target={host.name} autoFocus onClick={() => void drop(host.name)}>
                {t("common.remove")}
              </button>
            </div>
          )}
        </div>
      ))}

      {!hosts.length && !draft && <p className="rmtempty">{t("remotes.noRemoteMachinesYetAdd")}</p>}

      {/* Importing beats typing: on a machine that already uses ssh, the
          addresses are written down next door and this only borrows the name. */}
      {candidates.length > 0 && (
        <div className="rmtcands">
          <span className="cap">{t("remotes.alsoInSshConfig")}</span>
          {candidates.map((alias) => (
            <button
              key={alias}
              data-action="remotes.add"
              className="rmtcand"
              disabled={!!busy}
              title={t("remotes.addItWithTheSettings")}
              onClick={() => void save({ name: alias, useSSHConfig: true })}
            >
              + {alias}
            </button>
          ))}
        </div>
      )}

      {picking && editing ? (
        <RemoteDirs
          hub={hub}
          host={editing}
          start={(draft?.workspaces ?? [])[0]}
          onClose={() => setPicking(false)}
          onPick={(path) => {
            setPicking(false);
            if (!(draft?.workspaces ?? []).includes(path)) setDirs([...(draft?.workspaces ?? []), path]);
          }}
        />
      ) : null}

      {draft ? (
        <div className="rmtform">
          {field("name", t("common.name"), "gpu-box")}
          {/* A saved machine keeps the way it is reached: the two kinds share no
              field, so turning one into the other is a new machine, not an edit. */}
          {wsl && !editing ? (
            <div className="rmtf" role="radiogroup" aria-label={t("remotes.connectionType")}>
              <span>{t("remotes.connectionType")}</span>
              <div className="rmtkinds">
                {(["", "wsl"] as const).map((k) => (
                  <button
                    key={k || "ssh"}
                    role="radio"
                    aria-checked={(draft.kind ?? "") === k}
                    data-action="remotes.kind"
                    data-value={k || "ssh"}
                    onClick={() => setDraft((d) => (d ? { ...d, kind: k, distro: k ? d.distro || wsl.distros.find((x) => x.version === 2)?.name || "" : "" } : d))}
                  >
                    {k ? t("remotes.wSLDistribution") : t("remotes.sSHHost")}
                  </button>
                ))}
              </div>
            </div>
          ) : null}
          {isWSL ? (
            <label className="rmtf">
              <span>{t("remotes.distribution")}</span>
              <select data-action-change="remotes.distro" value={draft.distro ?? ""} disabled={!!editing} onChange={(ev) => setDraft((d) => (d ? { ...d, distro: ev.target.value } : d))}>
                {editing && !(wsl?.distros ?? []).some((x) => x.name === draft.distro) ? <option value={draft.distro}>{draft.distro}</option> : null}
                {(wsl?.distros ?? []).map((x) => (
                  <option key={x.name} value={x.name} disabled={x.version !== 2}>
                    {x.version === 2 ? x.name : t("remotes.wSL1NotSupported", { name: x.name })}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          {!isWSL && field("host", t("common.address"), "10.0.0.4")}
          {!isWSL && field("user", t("common.user"), "ada")}
          {!isWSL && (
              <label className="rmtf">
                <span>{t("common.port")}</span>
                <input
                  value={draft.port ? String(draft.port) : ""}
                  placeholder="22"
                  inputMode="numeric"
                  onChange={(ev) => setDraft((d) => (d ? { ...d, port: Number(ev.target.value.replace(/\D/g, "")) || 0 } : d))}
                />
              </label>
          )}
          {!isWSL && field("proxyJump", t("remotes.jumpHost"), "bastion.example.com")}
          {isWSL ? (
            <p className="rmthint">
              {t("remotes.theKernelRunsInA")}
            </p>
          ) : null}
          {/* Same shape the sandbox's writable list uses: one row per thing,
              then what you can do to it. The head carries the default badge
              rather than a separate field, because it is the same folder. */}
          <div className="rmtdirs">
            <div className="sublb">{t("remotes.projectsOnThisMachine")}</div>
            {(draft.workspaces ?? []).map((path, i) => (
              <div className="prule" key={path}>
                <code dir="ltr">{path}</code>
                {i === 0 ? (
                  <span className="tag">{t("common.default")}</span>
                ) : (
                  <button
                    className="act ghost"
                    aria-label={t("remotes.makeTheDefault", { path })}
                    onClick={() => setDirs([path, ...(draft.workspaces ?? []).filter((x) => x !== path)])}
                  >
                    {t("common.makeDefault")}
                  </button>
                )}
                <button
                  className="act ghost"
                  aria-label={t("remotes.stopListing", { path })}
                  onClick={() => setDirs((draft.workspaces ?? []).filter((x) => x !== path))}
                >
                  {t("common.delete")}
                </button>
              </div>
            ))}
            <div className="prule" data-add="">
              <input
                value={dir}
                placeholder={t("remotes.aProjectDirectoryOverThere")}
                onChange={(ev) => setDir(ev.target.value)}
                onKeyDown={(ev) => ev.key === "Enter" && addDir()}
              />
              <button className="act" disabled={!dir.trim()} onClick={addDir}>
                {t("common.add")}
              </button>
              {editing ? (
                <button className="act ghost" onClick={() => setPicking(true)}>
                  {t("remotes.browse")}
                </button>
              ) : null}
            </div>
          </div>
          {!isWSL && field("identityFile", t("remotes.privateKeyFile"), "~/.ssh/id_ed25519")}
          {/* What an empty form actually does is the kernel's to say. Leaving
              these blank is not "no authentication", and a reader who has to
              guess between agent, default keys and a password prompt is one
              who finds out on the first connect. */}
          {!isWSL && <p className="rmthint">{t("remotes.leftBlankItTriesIn")}</p>}
          {/* Named, not carried: this is the variable to read, never the secret
              itself, so nothing typed here is a password on its way anywhere. */}
          {!isWSL && field("passphraseEnv", t("remotes.envVarHoldingTheKey"), "GPU_BOX_PASSPHRASE")}
          {!isWSL && field("passwordEnv", t("remotes.envVarWithThePassword"), "GPU_BOX_PASSWORD")}
          {/* The kernel names this setting when an install fails, so it has to
              be here: an error saying "install it with npm instead" beside a
              setting nobody can reach says nothing. */}
          <label className="rmtf">
            <span>{t("remotes.installing")}</span>
            <select
              value={draft.serveInstall || "auto"}
              title={t("remotes.aFirstConnectInstallsA")}
              onChange={(ev) => setDraft((d) => (d ? { ...d, serveInstall: ev.target.value } : d))}
            >
              <option value="auto">{t("common.pickOneAutomatically")}</option>
              <option value="npm">{t("remotes.useTheRemoteSNpm")}</option>
              <option value="upload">{t("remotes.uploadThisMachineS")}</option>
              <option value="never">{t("remotes.doNotInstallThereIs")}</option>
            </select>
          </label>
          {/* Which machine's key the far-side kernel answers to. This one's by
              default, tunnelled back, so the other machine needs no key of its
              own and no route to the model API. */}
          {!isWSL && (
            <label className="rmtf">
              <span>{t("remotes.modelCredentials")}</span>
              <select
                value={draft.provider || "local"}
                title={t("remotes.whichMachineSProvidersAnd")}
                onChange={(ev) => setDraft((d) => (d ? { ...d, provider: ev.target.value } : d))}
              >
                <option value="local">{t("remotes.thisMachineSOverThe")}</option>
                <option value="remote">{t("remotes.thatMachineSOwn")}</option>
              </select>
            </label>
          )}
          {!isWSL && (
            <label className="rmtf rmtck">
              <input
                type="checkbox"
                checked={!!draft.useSSHConfig}
                onChange={(ev) => setDraft((d) => (d ? { ...d, useSSHConfig: ev.target.checked } : d))}
              />
              <span>{t("remotes.takeAnythingLeftBlankFrom")}</span>
            </label>
          )}
          <div className="rmtact">
            <button
              onClick={() => {
                setDraft(null);
                setEditing("");
                setDir("");
              }}
            >
              {t("common.cancel")}
            </button>
            <button data-go="" data-action={editing ? "remotes.save" : "remotes.add"} disabled={!draft.name.trim() || !!busy || (isWSL && !draft.distro)} onClick={() => void save(draft)}>
              {editing ? t("common.save") : t("common.add")}
            </button>
          </div>
        </div>
      ) : (
        <button className="rmtadd" onClick={() => setDraft(draftOf(null))}>
          + {t("remotes.addAMachine")}
        </button>
      )}
    </div>
  );
}

const ROUTE_LABEL: Record<string, string> = { npm: "npm", upload: "common.upload", download: "common.download" };

/** What one machine answered. A connect stops at the first missing piece, so
 *  the value here is seeing all of them together — and each closed route is
 *  worded by the code a real failure would have carried, not by a second set
 *  of sentences that could drift from it. */
function ProbeCard({ probe, host }: { probe: RemoteProbe; host: string }) {
  const closed = probe.routes.filter((r) => !r.ok);
  return (
    <div className="rmtprobe" data-ok={probe.ready ? "" : undefined}>
      <div className="rmtprobe-r">
        <span className="k">{t("common.machine")}</span>
        <span className="v">{probe.os}/{probe.arch} · {probe.home}</span>
      </div>
      <div className="rmtprobe-r">
        <span className="k">reasonix</span>
        <span className="v">
          {probe.kernel
            ? `${probe.kernel}${probe.version ? " " + probe.version : ""}`
            : probe.outdated
              ? t("remotes.itHasThereTooOld", { v: probe.outdated })
              : t("remotes.noneYet")}
        </span>
      </div>
      <div className="rmtprobe-r">
        <span className="k">npm</span>
        <span className="v">{probe.npm || t("remotes.willNotRun")}</span>
      </div>
      {closed.map((r) => (
        <p className="rmtprobe-why" key={r.name}>
          {say({ code: r.code, params: { host } }, t("remotes.theRouteIsClosed", { name: t(ROUTE_LABEL[r.name] ?? r.name) }))}
        </p>
      ))}
      <div className="rmtprobe-end">
        {probe.ready ? t("remotes.readyThereIsAKernel") : t("remotes.notReadyClearingAnyOne")}
      </div>
    </div>
  );
}
