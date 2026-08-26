// ── Remote SSH module (mirrors desktop/remote_app.go view structs) ──

export type RemoteConnState =
  | "connecting"
  | "connected"
  | "reconnecting"
  | "degraded"
  | "pending_hostkey"
  | "pending_secret"
  | "stopped";

export type RemoteServerState =
  | "starting"
  | "detect"
  | "install"
  | "waiting_lock"
  | "launch"
  | "health_check"
  | "ready"
  | "error"
  | "stopped"
  | "reuse";

// ── Remote projects ──
export interface RemoteTabRefView {
  hostId: string;
  workspace: string;
}

export interface RemoteTabMetaFields {
  remote?: RemoteTabRefView;
  remoteState?: RemoteTabStateValue;
}

export interface RemoteProjectNodeFields {
  remote?: RemoteTabRefView;
  remoteSession?: { hostId: string; workspace: string; name: string };
}

export interface RemoteSessionMetaFields {
  remote?: RemoteTabRefView;
}

export interface RemoteProjectView {
  hostId: string;
  workspace: string;
  title?: string;
  color?: string;
  merged?: boolean;
}

export interface RemoteSessionView {
  name: string;
  title: string;
  turns: number;
  current?: boolean;
  lastActivityAt?: number;
  pinned?: boolean;
}

export type RemoteTabStateValue = "connecting" | "ready" | "reconnecting" | "serve_down" | "error" | "disconnected";

export interface RemoteTabState {
  state: RemoteTabStateValue;
  error?: string;
}

export interface RemoteTabOpenOptions {
  newSession?: boolean;
  sessionName?: string;
}

export interface RemoteTabSnapshot {
  history: unknown[];
  context?: unknown;
  todos?: unknown[];
  checkpoints?: unknown[];
  models?: string[];
  commands?: unknown[];
  status?: unknown;
  pendingEvents?: unknown[];
}

export interface RemoteAskAnswer {
  QuestionID: string;
  Selected: string[];
}

export interface RemoteHostView {
  id: string;
  label: string;
  host: string;
  port: number;
  user: string;
  identityFile: string;
  proxyJump: string;
  defaultWorkspace: string;
  serveInstall: string;
  credentialMode: string;
  useSSHConfig: boolean;
  passwordSet?: boolean;
  keyPassphraseSet?: boolean;
}

export interface RemoteHostInput {
  label: string;
  host: string;
  port: number;
  user: string;
  identityFile: string;
  proxyJump: string;
  defaultWorkspace: string;
  serveInstall: string;
  credentialMode: string;
  useSSHConfig: boolean;
  password?: string;
  keyPassphrase?: string;
  clearPassword?: boolean;
  clearPassphrase?: boolean;
  preserveExistingSettings?: boolean;
}

export interface RemoteFingerprintView {
  hostId: string;
  address: string;
  keyType: string;
  sha256: string;
}

export interface RemoteSecretPromptView {
  promptId: string;
  hostId: string;
  host: string;
  kind: "password" | "passphrase";
  identity?: string;
}

export interface RemoteKnownHostLocation {
  path: string;
  line: number;
}

export interface RemoteConnectionErrorDetails {
  code: "connection_failed" | "auth_failed" | "host_key_rejected" | "host_key_mismatch";
  presentedSha256?: string;
  knownHostRecords?: RemoteKnownHostLocation[];
}

export interface RemoteConnectionStatus {
  hostId: string;
  state: RemoteConnState;
  error?: string;
  errorDetails?: RemoteConnectionErrorDetails;
  fingerprint?: RemoteFingerprintView;
  secretPrompt?: RemoteSecretPromptView;
  attempt?: number;
}

export interface RemoteDirEntry {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  mtimeUnix: number;
  symlink: boolean;
}

export interface RemoteFilePreview {
  path: string;
  body: string;
  size: number;
  mtimeUnix: number;
  truncated: boolean;
  binary: boolean;
  err?: string;
}

export interface RemoteWriteResult {
  ok: boolean;
  conflict: boolean;
  newMtimeUnix: number;
}

export interface RemoteForwardInput {
  localPort: number;
  remoteHost: string;
  remotePort: number;
  label: string;
}

export interface RemoteForwardView {
  id: string;
  hostId: string;
  localPort: number;
  remoteHost: string;
  remotePort: number;
  label: string;
  state: string;
  error?: string;
}

export interface RemoteServerView {
  hostId: string;
  workspace: string;
  state: RemoteServerState;
  message?: string;
  localUrl?: string;
  error?: string;
}

/** Path-free summary of files left behind by the removed Remote Workbench. */
export interface RemoteLegacyWorkbenchData {
  mirrorCount: number;
  mirrorBytes: number;
  trustFile: boolean;
}

export interface RemoteForwardsEvent {
  hostId: string;
  forwards: RemoteForwardView[];
}
