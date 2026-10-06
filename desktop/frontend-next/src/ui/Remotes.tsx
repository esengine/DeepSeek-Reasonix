import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import type { HubPort } from "../port/hub";
import type { RemoteHost, RemoteHostEdit, RemoteProbe } from "../port/remote";
import { say } from "../i18n/kernel";
import { toggleHiddenHost, useHiddenHosts } from "../state/remotehide";
import { RemoteDirs } from "./RemoteDirs";
import { Switch } from "./Switch";

interface Props {
  hub: HubPort;
  onError: (e: unknown) => void;
}

const STATUS_LABEL: Record<string, string> = {
  idle: "未连接",
  connecting: "连接中",
  connected: "已连接",
  reconnecting: "重连中",
  degraded: "部分转发未建立",
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
    disabled: host?.disabled ?? false,
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
              {t(probing === host.name ? "询问中…" : "测试连接")}
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
            {host.disabled ? <span className="st">{t("已停用")}</span> : null}
            <Switch
              data-action="remote.enable"
              data-target={host.name}
              on={!host.disabled}
              busy={busy === host.name}
              label={t(host.disabled ? "启用这台主机" : "停用这台主机")}
              onClick={() => void save({ ...draftOf(host), disabled: !host.disabled })}
            />
            <button className="rmtlnk" data-danger="" disabled={!!busy} onClick={() => setConfirm(host.name)}>
              {t("移除")}
            </button>
          </div>
          <div className="sub">
            {host.workspace ? <span dir="ltr">{host.workspace}</span> : <span className="dim">{t("未设置默认工作区")}</span>}
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
              <span>{t("从列表中移除「{name}」？远端数据不会被删除。", { name: host.name })}</span>
              <button onClick={() => setConfirm("")}>{t("取消")}</button>
              <button data-danger="" data-action="remotes.remove" data-target={host.name} autoFocus onClick={() => void drop(host.name)}>
                {t("移除")}
              </button>
            </div>
          )}
        </div>
      ))}

      {!hosts.length && !draft && <p className="rmtempty">{t("尚未添加远程机器。添加后，其工作区将与本地工作区并列显示在左栏。")}</p>}

      {/* Importing beats typing: on a machine that already uses ssh, the
          addresses are written down next door and this only borrows the name. */}
      {candidates.length > 0 && (
        <div className="rmtcands">
          <span className="cap">{t("~/.ssh/config 中还有")}</span>
          {candidates.map((alias) => (
            <button
              key={alias}
              data-action="remotes.add"
              className="rmtcand"
              disabled={!!busy}
              title={t("按 ssh_config 中的配置添加")}
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
          {field("name", t("名称"), "gpu-box")}
          {field("host", t("地址"), "10.0.0.4")}
          {field("user", t("用户"), "ada")}
          <label className="rmtf">
            <span>{t("端口")}</span>
            <input
              value={draft.port ? String(draft.port) : ""}
              placeholder="22"
              inputMode="numeric"
              onChange={(ev) => setDraft((d) => (d ? { ...d, port: Number(ev.target.value.replace(/\D/g, "")) || 0 } : d))}
            />
          </label>
          {field("proxyJump", t("跳板机"), "bastion.example.com")}
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
                    aria-label={t("将 {path} 设为默认", { path })}
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
                  {t("删除")}
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
                {t("添加")}
              </button>
              {editing ? (
                <button className="act ghost" onClick={() => setPicking(true)}>
                  {t("浏览…")}
                </button>
              ) : null}
            </div>
          </div>
          {field("identityFile", t("私钥文件"), "~/.ssh/id_ed25519")}
          {/* What an empty form actually does is the kernel's to say. Leaving
              these blank is not "no authentication", and a reader who has to
              guess between agent, default keys and a password prompt is one
              who finds out on the first connect. */}
          <p className="rmthint">{t("留空时按这个顺序尝试：先 ssh-agent 持有的密钥，再 ~/.ssh 下的默认密钥；只有填了下面的环境变量或该机器要求交互时，才会走密码。")}</p>
          {/* Named, not carried: this is the variable to read, never the secret
              itself, so nothing typed here is a password on its way anywhere. */}
          {field("passphraseEnv", t("私钥口令的环境变量名"), "GPU_BOX_PASSPHRASE")}
          {field("passwordEnv", t("登录密码的环境变量名"), "GPU_BOX_PASSWORD")}
          {/* The kernel names this setting when an install fails, so it has to
              be here: an error saying "install it with npm instead" beside a
              setting nobody can reach says nothing. */}
          <label className="rmtf">
            <span>{t("安装方式")}</span>
            <select
              value={draft.serveInstall || "auto"}
              title={t("首次连接需在该机器上安装 reasonix")}
              onChange={(ev) => setDraft((d) => (d ? { ...d, serveInstall: ev.target.value } : d))}
            >
              <option value="auto">{t("自动选择")}</option>
              <option value="npm">{t("使用远端的 npm")}</option>
              <option value="upload">{t("上传本机的副本")}</option>
              <option value="never">{t("不安装，已自行安装")}</option>
            </select>
          </label>
          {/* Whether a machine is listed is this window's preference, not the
              machine's own setting: it sits with the row's other details rather
              than beside the switch that decides whether it is dialed at all. */}
          {editing ? (
            <label className="rmtf">
              <span>{t("在侧栏显示")}</span>
              <Switch
                data-action="remote.hide"
                on={!hidden.includes(editing)}
                label={t(hidden.includes(editing) ? "在侧栏显示" : "在侧栏隐藏")}
                onClick={() => toggleHiddenHost(editing)}
              />
            </label>
          ) : null}
          {/* Which machine's key the far-side kernel answers to. This one's by
              default, tunnelled back, so the other machine needs no key of its
              own and no route to the model API. */}
          <label className="rmtf">
            <span>{t("模型凭据")}</span>
            <select
              value={draft.provider || "local"}
              title={t("远端会话使用哪台机器上配置的 Provider 与 Key")}
              onChange={(ev) => setDraft((d) => (d ? { ...d, provider: ev.target.value } : d))}
            >
              <option value="local">{t("使用本机配置，经隧道转发")}</option>
              <option value="remote">{t("使用该机器自身的配置")}</option>
            </select>
          </label>
          <label className="rmtf rmtck">
            <input
              type="checkbox"
              checked={!!draft.useSSHConfig}
              onChange={(ev) => setDraft((d) => (d ? { ...d, useSSHConfig: ev.target.checked } : d))}
            />
            <span>{t("留空的项将从 ~/.ssh/config 读取")}</span>
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
          + {t("添加机器")}
        </button>
      )}
    </div>
  );
}

const ROUTE_LABEL: Record<string, string> = { npm: "npm", upload: "上传", download: "下载" };

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
              ? t("远端为 {v}，版本过旧，连接时将被替换", { v: probe.outdated })
              : t("尚未安装")}
        </span>
      </div>
      <div className="rmtprobe-r">
        <span className="k">npm</span>
        <span className="v">{probe.npm || t("无法运行")}</span>
      </div>
      {closed.map((r) => (
        <p className="rmtprobe-why" key={r.name}>
          {say({ code: r.code, params: { host } }, t("{name} 无法连通", { name: t(ROUTE_LABEL[r.name] ?? r.name) }))}
        </p>
      ))}
      <div className="rmtprobe-end">
        {probe.ready ? t("可以连接 —— 远端已有内核，或可安装一个") : t("无法连接 —— 解决上述任意一项即可")}
      </div>
    </div>
  );
}
