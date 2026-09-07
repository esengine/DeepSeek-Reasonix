import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import type { HubPort } from "../port/hub";
import type { RemoteHost, RemoteHostEdit, RemoteProbe } from "../port/remote";
import { say } from "../i18n/kernel";
import { RemoteDirs } from "./RemoteDirs";

interface Props {
  hub: HubPort;
  onError: (e: unknown) => void;
}

const STATUS_LABEL: Record<string, string> = {
  idle: "未连接",
  connecting: "连接中",
  connected: "已连上",
  reconnecting: "重连中",
  degraded: "有转发没挂上",
  stopped: "已断开",
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
  };
}

export function Remotes({ hub, onError }: Props) {
  const [hosts, setHosts] = useState<RemoteHost[]>([]);
  const [candidates, setCandidates] = useState<string[]>([]);
  const [draft, setDraft] = useState<RemoteHostEdit | null>(null);
  // The folder being typed, kept out of the draft: it is not part of the row
  // until it is added, and a save must not smuggle a half-typed path in.
  const [dir, setDir] = useState("");
  // The name a draft is editing, empty for a new one. Kept apart from the
  // draft's own name so renaming stays possible later without losing the row.
  const [editing, setEditing] = useState("");
  const [confirm, setConfirm] = useState("");
  const [busy, setBusy] = useState("");
  // What each machine answered when asked. Kept per host so a second probe
  // does not blank the first one's answer while it runs.
  const [probes, setProbes] = useState<Record<string, RemoteProbe>>({});
  const [probing, setProbing] = useState("");
  // Browsing dials, and only a row already in the book has an address to dial.
  // A draft being typed has nowhere to go yet, so it types the path instead.
  const [picking, setPicking] = useState(false);

  const reload = useCallback(async () => {
    try {
      const [book, aliases] = await Promise.all([hub.remoteHosts(), hub.remoteCandidates().catch(() => [])]);
      setHosts(book ?? []);
      setCandidates(aliases);
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

  const field = (key: keyof RemoteHostEdit, label: string, placeholder?: string) => (
    <label className="rmtf">
      <span>{t(label)}</span>
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
              {t(probing === host.name ? "问着…" : "测一下")}
            </button>
            <button
              className="rmtlnk"
              onClick={() => {
                setEditing(host.name);
                setDraft(draftOf(host));
              }}
            >
              {t("编辑")}
            </button>
            <button className="rmtlnk" data-danger="" disabled={!!busy} onClick={() => setConfirm(host.name)}>
              {t("移除")}
            </button>
          </div>
          <div className="sub">
            {host.workspace ? <span dir="ltr">{host.workspace}</span> : <span className="dim">{t("没设默认工作区")}</span>}
            {workspacesOf(host).length > 1 ? (
              <span className="tag">{t("还有 {n} 个项目", { n: workspacesOf(host).length - 1 })}</span>
            ) : null}
            {host.useSSHConfig ? <span className="tag">{t("跟随 ssh_config")}</span> : null}
            {host.forwards ? <span className="tag">{t("{n} 条转发", { n: host.forwards })}</span> : null}
            {host.panes ? <span className="tag">{t("{n} 个面板", { n: host.panes })}</span> : null}
          </div>
          {probes[host.name] ? <ProbeCard probe={probes[host.name]} host={host.name} /> : null}
          {confirm === host.name && (
            <div className="rmtconfirm" role="alertdialog">
              <span>{t("从列表移除「{name}」？远端什么都不会删。", { name: host.name })}</span>
              <button onClick={() => setConfirm("")}>{t("取消")}</button>
              <button data-danger="" data-action="remotes.remove" data-target={host.name} autoFocus onClick={() => void drop(host.name)}>
                {t("移除")}
              </button>
            </div>
          )}
        </div>
      ))}

      {!hosts.length && !draft && <p className="rmtempty">{t("还没有远程机器。加一台，它的工作区就和本地的并排出现在左栏。")}</p>}

      {/* Importing beats typing: on a machine that already uses ssh, the
          addresses are written down next door and this only borrows the name. */}
      {candidates.length > 0 && (
        <div className="rmtcands">
          <span className="cap">{t("~/.ssh/config 里还有")}</span>
          {candidates.map((alias) => (
            <button
              key={alias}
              data-action="remotes.add"
              className="rmtcand"
              disabled={!!busy}
              title={t("按 ssh_config 里的设置加进来")}
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
          {field("name", "名字", "gpu-box")}
          {field("host", "地址", "10.0.0.4")}
          {field("user", "用户", "ada")}
          <label className="rmtf">
            <span>{t("端口")}</span>
            <input
              value={draft.port ? String(draft.port) : ""}
              placeholder="22"
              inputMode="numeric"
              onChange={(ev) => setDraft((d) => (d ? { ...d, port: Number(ev.target.value.replace(/\D/g, "")) || 0 } : d))}
            />
          </label>
          {field("proxyJump", "跳板机", "bastion.example.com")}
          {/* Same shape the sandbox's writable list uses: one row per thing,
              then what you can do to it. The head carries the default badge
              rather than a separate field, because it is the same folder. */}
          <div className="rmtdirs">
            <div className="sublb">{t("这台机器上的项目")}</div>
            {(draft.workspaces ?? []).map((path, i) => (
              <div className="prule" key={path}>
                <code dir="ltr">{path}</code>
                {i === 0 ? (
                  <span className="tag">{t("默认")}</span>
                ) : (
                  <button
                    className="act ghost"
                    aria-label={t("把 {path} 设成默认", { path })}
                    onClick={() => setDirs([path, ...(draft.workspaces ?? []).filter((x) => x !== path)])}
                  >
                    {t("设为默认")}
                  </button>
                )}
                <button
                  className="act ghost"
                  aria-label={t("不再列出 {path}", { path })}
                  onClick={() => setDirs((draft.workspaces ?? []).filter((x) => x !== path))}
                >
                  {t("删掉")}
                </button>
              </div>
            ))}
            <div className="prule" data-add="">
              <input
                value={dir}
                placeholder={t("远端的项目目录，例如 /srv/training")}
                onChange={(ev) => setDir(ev.target.value)}
                onKeyDown={(ev) => ev.key === "Enter" && addDir()}
              />
              <button className="act" disabled={!dir.trim()} onClick={addDir}>
                {t("加上")}
              </button>
              {editing ? (
                <button className="act ghost" onClick={() => setPicking(true)}>
                  {t("浏览…")}
                </button>
              ) : null}
            </div>
          </div>
          {field("identityFile", "私钥文件", "~/.ssh/id_ed25519")}
          {/* Named, not carried: this is the variable to read, never the secret
              itself, so nothing typed here is a password on its way anywhere. */}
          {field("passphraseEnv", "私钥口令的环境变量名", "GPU_BOX_PASSPHRASE")}
          {field("passwordEnv", "登录密码的环境变量名", "GPU_BOX_PASSWORD")}
          {/* 装不上时内核会点名让人改这一项，所以它必须在这里 —— 一条说「改成
              npm 装」的错误，配上一个改不了的设置，等于没说。 */}
          <label className="rmtf">
            <span>{t("安装方式")}</span>
            <select
              value={draft.serveInstall || "auto"}
              title={t("第一次连接要在那台机器上装一个 reasonix")}
              onChange={(ev) => setDraft((d) => (d ? { ...d, serveInstall: ev.target.value } : d))}
            >
              <option value="auto">{t("自动挑一种")}</option>
              <option value="npm">{t("用远端的 npm")}</option>
              <option value="upload">{t("传本机这个过去")}</option>
              <option value="never">{t("不装，我自己装好了")}</option>
            </select>
          </label>
          {/* 远端跑的内核用哪台机器的 Key。默认用本机的，经隧道回来 —— 那台
              机器就不用再配一遍 Key，也不用能访问模型 API。 */}
          <label className="rmtf">
            <span>{t("模型凭据")}</span>
            <select
              value={draft.provider || "local"}
              title={t("远端会话用哪台机器上配置的 Provider 和 Key")}
              onChange={(ev) => setDraft((d) => (d ? { ...d, provider: ev.target.value } : d))}
            >
              <option value="local">{t("用本机的，经隧道过去")}</option>
              <option value="remote">{t("用那台机器自己配的")}</option>
            </select>
          </label>
          <label className="rmtf rmtck">
            <input
              type="checkbox"
              checked={!!draft.useSSHConfig}
              onChange={(ev) => setDraft((d) => (d ? { ...d, useSSHConfig: ev.target.checked } : d))}
            />
            <span>{t("空着的项去 ~/.ssh/config 里找")}</span>
          </label>
          <div className="rmtact">
            <button
              onClick={() => {
                setDraft(null);
                setEditing("");
                setDir("");
              }}
            >
              {t("取消")}
            </button>
            <button data-go="" data-action={editing ? "remotes.save" : "remotes.add"} disabled={!draft.name.trim() || !!busy} onClick={() => void save(draft)}>
              {editing ? t("保存") : t("添加")}
            </button>
          </div>
        </div>
      ) : (
        <button className="rmtadd" onClick={() => setDraft(draftOf(null))}>
          + {t("加一台机器")}
        </button>
      )}
    </div>
  );
}

const ROUTE_LABEL: Record<string, string> = { npm: "npm", upload: "传过去", download: "下载" };

/** What one machine answered. A connect stops at the first missing piece, so
 *  the value here is seeing all of them together — and each closed route is
 *  worded by the code a real failure would have carried, not by a second set
 *  of sentences that could drift from it. */
function ProbeCard({ probe, host }: { probe: RemoteProbe; host: string }) {
  const closed = probe.routes.filter((r) => !r.ok);
  return (
    <div className="rmtprobe" data-ok={probe.ready ? "" : undefined}>
      <div className="rmtprobe-r">
        <span className="k">{t("机器")}</span>
        <span className="v">{probe.os}/{probe.arch} · {probe.home}</span>
      </div>
      <div className="rmtprobe-r">
        <span className="k">reasonix</span>
        <span className="v">
          {probe.kernel
            ? `${probe.kernel}${probe.version ? " " + probe.version : ""}`
            : probe.outdated
              ? t("那边是 {v}，太旧了，连的时候会换掉", { v: probe.outdated })
              : t("还没有")}
        </span>
      </div>
      <div className="rmtprobe-r">
        <span className="k">npm</span>
        <span className="v">{probe.npm || t("跑不了")}</span>
      </div>
      {closed.map((r) => (
        <p className="rmtprobe-why" key={r.name}>
          {say({ code: r.code, params: { host } }, t("{name} 这条路走不通", { name: t(ROUTE_LABEL[r.name] ?? r.name) }))}
        </p>
      ))}
      <div className="rmtprobe-end">
        {probe.ready ? t("能连上 —— 那边有内核，或者装得上一个") : t("连不上 —— 上面任意一条解决掉就行")}
      </div>
    </div>
  );
}
