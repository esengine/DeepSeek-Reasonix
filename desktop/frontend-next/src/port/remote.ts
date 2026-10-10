// What a machine's link is doing, in the kernel's own words. A host nobody has
// connected to reports "idle" rather than being absent — its row is where
// connecting starts. "degraded" is reachable but with a forward that did not
// attach, which reads as connected right up until a button does nothing.
export type RemoteStatus = "idle" | "connecting" | "connected" | "reconnecting" | "degraded" | "stopped";

// One machine in the host book. Everything past `target` is the live link, so a
// row drawn from configuration alone is complete and simply idle.
export interface RemoteHost {
  name: string;
  // How the user would type it for ssh, so a row is recognisable without
  // opening it: user@host, and a port only when it is not the assumed one.
  target: string;
  status: RemoteStatus;

  // The stored row in full, not only what a row displays. Saving replaces an
  // entry, so editing one field while holding a partial copy would blank
  // whatever this page never received.
  host?: string;
  port?: number;
  user?: string;
  identityFile?: string;
  proxyJump?: string;
  // The default folder a bare connect lands in, and every folder this machine
  // is worked in with that default first. The far kernel remembers what has
  // been opened over there; this is what the window can offer before there is
  // a link to ask through.
  workspace?: string;
  workspaces?: string[];
  serveInstall?: string;
  // Where this host's model credentials come from: "local" resolves them on
  // this machine over the tunnel, "remote" leaves it reading its own config.
  provider?: string;
  useSSHConfig?: boolean;
  passphraseEnv?: string;
  passwordEnv?: string;
  // Off: the machine stays in the book but is not dialed — no pane opens on it
  // and the rail leaves it out. Absent means on, so an old config reads as it
  // always did.
  disabled?: boolean;
  // Set from the CLI, with no control here — counted so an edit does not look
  // like it dropped them.
  forwards?: number;
  // Which reconnect this is, while reconnecting.
  attempt?: number;
  // The bootstrap step a first connect has reached. Present only while one is
  // running: a cold connect installs a binary, and a spinner cannot say so.
  step?: string;
  detail?: string;
  error?: string;
  panes?: number;
}

// The bootstrap's steps, in the order a cold connect walks them. The kernel
// sends the key; the labels are here because only a screen needs words. An
// unknown key is shown as itself rather than dropped — a new step is worth
// seeing untranslated, and better than a list that silently skips it.
export const REMOTE_STEPS = ["detect", "install", "waiting_lock", "launch", "health_check", "ready", "reuse"] as const;

export const REMOTE_STEP_LABEL: Record<string, string> = {
  detect: "探测平台",
  install: "安装 reasonix",
  waiting_lock: "等待其他连接",
  launch: "启动 serve",
  health_check: "等待就绪",
  ready: "挂载端口转发",
  reuse: "接入已运行的实例",
};

// One row as the settings page writes it. Secrets are named, never carried:
// the two Env fields hold the name of an environment variable, the same shape
// a provider's key takes — so nothing here is a password in transit.
export interface RemoteHostEdit {
  name: string;
  host?: string;
  port?: number;
  user?: string;
  identityFile?: string;
  proxyJump?: string;
  // The list is what this page writes; the head of it is the default. Sending
  // the two apart is how they would come to disagree about which one that is.
  workspaces?: string[];
  serveInstall?: string;
  provider?: string;
  // Layer ~/.ssh/config under whatever this row leaves unset. With it the alias
  // is the address, which is what makes an imported row complete on its own.
  useSSHConfig?: boolean;
  passphraseEnv?: string;
  passwordEnv?: string;
  disabled?: boolean;
}

// One folder on another machine. The name is carried beside the path because
// only that machine's rules can cut it: a Windows host answers with a drive
// letter and backslashes, and this one's split() would keep the whole string.
export interface RemoteFolder {
  name: string;
  path: string;
}

// One directory of that machine, as it spells it. An absent parent is the top —
// which is the far side saying so, not this side reasoning about a path syntax
// that is not its own. Truncated marks a folder too big to send whole; the rows
// left out are said so rather than silently missing.
export interface RemoteListing {
  path: string;
  parent?: string;
  folders: RemoteFolder[];
  truncated?: boolean;
}

// A question the link layer stopped for. Three identities: the kernel lifetime
// it belongs to, the operation it is blocking, and itself — an answer has to
// carry all three, or a restart that numbered a question the same would take it.
export interface RemoteAsk {
  epoch: string;
  operationId: string;
  askId: string;
  kind: "hostkey" | "passphrase" | "password";
  host: string;
  // hostkey: what the machine presented, to compare against what the person
  // was told it should be. Never pre-answered — accepting a fingerprint
  // nobody read is the one thing this dialog exists to prevent.
  address?: string;
  keyType?: string;
  fingerprint?: string;
  // passphrase: which key file is locked.
  identityFile?: string;
}

/** One way a kernel could reach a machine. A closed route carries the same
 *  dotted code a failed connect would refuse with, so say() words a foreseen
 *  failure exactly like the failure itself. */
export interface RemoteRoute {
  name: string;
  ok: boolean;
  code?: string;
}

/** What a machine can do, read before anything is attempted on it. `ready`
 *  means a route to a kernel exists — never that taking it will succeed. */
export interface RemoteProbe {
  os: string;
  arch: string;
  home: string;
  kernel?: string;
  version?: string;
  outdated?: string;
  npm?: string;
  ready: boolean;
  routes: RemoteRoute[];
}
