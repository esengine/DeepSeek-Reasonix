import { HttpError } from "./http_error";
import type { Attachment, DroppedRef } from "./attachment";
export { HttpError };
export type { Attachment, DroppedRef };

import type { AccountState, AccountUser, DeviceGrant } from "./account";
import type { Adjudications } from "./adjudication";
export type { AdjudicationEntry, AdjudicationState, Adjudications } from "./adjudication";
import type { HookCatalog, HookDryRun, HookEntry, HookEventInfo, HookSource } from "./hook";
import type { CapabilityScope, McpCatalog, McpDraft, McpDraftServer, McpEntry, McpInstallResult, McpInstallScope, McpRisk, McpTool, ScopeLayer } from "./mcp";
import type { MemoryCatalog, MemoryEdit, MemoryEntry } from "./memory";
import type { UsageReport } from "./usage";
export type { MemoryEdit } from "./memory";
export type { Money, UsageDay, UsageModel, UsageProvider, UsageReport } from "./usage";
import type { CompactionSettings, Completion, CompletionItem, ModelEntry, ModelPrice, RoleAssignments } from "./model";
import type { NetworkProbe, NetworkSettings } from "./network";
import type { ApprovalMode, ApprovalVerdict, Checkpoint, HistoryMessage, JobEntry, Preset, RewindPlan, RewindResult, RewindScope, SessionEntry, SessionStatus, WalletLine, WalletReading, PlanAction } from "./session";
import type { ContextBreakdown, ShellOption, ShellSettings } from "./shell";
import type { SkillCatalog, SkillEntry } from "./skill";
import type { UpdateProgress, VersionEntry, VersionHub } from "./version";
import type { ChangeDiff, WorkspaceChange, WorkspaceChanges, WorkspaceEntry, WorkspaceInfo } from "./workspace";

// The port is one contract; its subjects each keep their own file, the way the
// wire and the layers below already do. This is where a reader still finds
// them all.
export type { AccountState, AccountUser, ApprovalMode, ApprovalVerdict, CapabilityScope,
  Checkpoint, CompactionSettings, Completion, CompletionItem, ContextBreakdown, DeviceGrant, HistoryMessage,
  HookCatalog, HookDryRun, HookEntry, HookEventInfo, HookSource, JobEntry, McpCatalog, McpDraft,
  McpDraftServer, McpEntry, McpInstallResult, McpInstallScope, McpRisk, McpTool, MemoryCatalog,
  MemoryEntry, ModelEntry, ModelPrice, NetworkProbe, NetworkSettings, Preset, RewindPlan,
  RewindResult, RewindScope, RoleAssignments, ScopeLayer, SessionEntry, SessionStatus,
  ShellOption, ShellSettings, SkillCatalog, SkillEntry, UpdateProgress, VersionEntry,
  VersionHub, WalletLine, WalletReading, ChangeDiff, WorkspaceChange, WorkspaceChanges, WorkspaceEntry, WorkspaceInfo };

import type { ExecutionGraphRead, WireEvent } from "./wire";
import type { PluginExport, PluginInstallRequest, PluginPackage, PluginPlan } from "./plugin";
import type { Appearance, ThemePack } from "./look";
import type { ConfigProblem, ConfigRepair, PermissionLists, PermissionRules, SandboxSettings } from "./boundary";
import type { Protocol, ProviderCheck, ProviderDraft, ProviderEdit, ProviderEntry, ProviderProbe, ProviderSetup } from "./provider";
import type { StoragePlan, StorageState } from "./storage";

export type * from "./plugin";

// GET /mcp. One external tool provider. state is ready | connecting | failed |
// disabled | idle: disabled is switched off and stays off across restarts, idle
// is configured and simply not needed yet. They look identical to the live host
// and mean opposite things, so the server resolves which one it is.
// A theme pack is data and the reader's own settings sit on top of it; both
// live next door, because they are one subject and this file is the port's
// whole surface rather than any one part of it.
export type * from "./look";

// What GET /complete answers: the menu, and the half-open span of the token an
// accepted item replaces. Offsets are UTF-16 code units — the units a string
// index uses here, converted from the kernel's bytes at the boundary.
export type * from "./boundary";

export type * from "./provider";

/** What the kernel answers when a line is queued — mid-turn guidance or a whole
 *  turn waiting its go. itemId is the durable entry, and it is the whole reason
 *  the line can be taken back: without it the row on screen has no name to
 *  cancel by. */
export interface Queued {
  itemId: string;
  disposition?: string;
}

/** GET /inbox. What is actually waiting, kernel-side. The optimistic rows this
 *  window adds say only what it sent itself: they do not survive a reload, and
 *  they never show what the CLI or another window put in the same queue. */
export interface Queue {
  revision: number;
  paused: boolean;
  // A newer on-disk format loads read-only and never auto-runs, so the panel
  // has to offer looking rather than editing.
  readonly?: boolean;
  // Entries a crash left behind. The kernel pauses itself when it finds any,
  // because re-running an instruction nobody re-confirmed is the worse error.
  recoveredCount?: number;
  items: QueueItem[];
  capacity: QueueCapacity;
}

/** One waiting line — metadata only. preview is all the manifest keeps; the
 *  body is a separate read, which is why editing takes the text back. */
export interface QueueItem {
  id: string;
  intent: "followup" | "steer";
  // Absent means the user wrote it. "host" is the runtime's own continuation —
  // a finished background job asking the session to pick the work back up.
  origin?: "host";
  state: "queued" | "steer_accepted" | "steer_consumed" | "running" | "blocked" | "uncertain";
  preview: string;
  createdAt: string;
  // Why a blocked entry stopped, from the kernel that stopped it.
  blockReason?: string;
  // Files this entry froze at the moment it was queued. Their presence is what
  // makes re-freezing meaningful; the entry quotes them as they were.
  refs?: { path?: string }[];
}

/** Both limits bite: the queue refuses on count and on total bytes, and a
 *  panel that showed one of them would be wrong about the other. */
export interface QueueCapacity {
  items: number;
  maxItems: number;
  bytes: number;
  maxBytes: number;
}

/** What the window says about its own status icon. null where there is no
 *  window under the page — the same build runs in a browser tab. */
export interface TrayPrefs {
  // What the setting asks for.
  icon: boolean;
  // Whether there is one right now. It can only differ from icon until the
  // next launch: an icon cannot be put back on a process that took one down.
  live: boolean;
  closeToTray: boolean;
}

export interface AgentPort {
  providerSetup(): Promise<ProviderSetup | null>;
  saveProviderKey(apiKey: string): Promise<void>;
  models(): Promise<ModelEntry[]>;
  // What the line being typed can still become, asked once per keystroke. The
  // answer depends on the caret, so it cannot be cached into a static list the
  // way slash() is.
  complete(line: string, cursor: number): Promise<Completion>;
  skills(root?: string): Promise<SkillCatalog>;
  // Persisted, but the running session keeps the prompt index it was built
  // with: the switch reaches the model on the next rebuild, not this turn.
  // Flipping a skill this project inherits writes a project row by default: the
  // user answered for this folder, and two projects may hold different skills
  // of one name.
  setSkillEnabled(name: string, enabled: boolean, scope?: ScopeLayer, root?: string): Promise<void>;
  // Drops this project's exception so the skill inherits again.
  clearSkillOverride(name: string, root?: string): Promise<void>;
  plugins(): Promise<PluginPackage[]>;
  // Two calls, because what a package brings has to be looked at before it is
  // let in: plan writes nothing, and install echoes the plan's id back so a
  // source cannot describe one install and perform another. An update is the
  // same pair with replace set — a new version can bring a hook the old one
  // did not have, and that is exactly what the second look is for.
  planPlugin(req: PluginInstallRequest): Promise<PluginPlan>;
  installPlugin(req: PluginInstallRequest): Promise<PluginPlan>;
  setPluginEnabled(name: string, enabled: boolean): Promise<void>;
  removePlugin(name: string): Promise<PluginPlan>;
  // Hands the packed package to the user and reports what was stripped out of
  // it on the way. Installing is the same door: a folder, a link, or this
  // archive unpacked — there is no separate import.
  exportPlugin(name: string): Promise<PluginExport>;
  // Text the window assembled, put on disk. Returns where it went, or null when
  // the host handled it without a path to report (a browser download).
  saveText(name: string, content: string): Promise<string | null>;
  // Rebuilds the runtime from what is on disk now: restarts extension sidecars
  // and rescans skills/commands/hooks. An extension author editing code needs
  // this; without it the only way to load an edit is restarting the app.
  // Rejected while a turn or background job is running (busy.reload_extensions).
  reloadExtensions(): Promise<void>;
  hooks(): Promise<HookCatalog>;
  // Replaces one scope wholesale: a client that merges partial edits wrong
  // silently drops somebody else's rule.
  saveHooks(scope: "user" | "project", hooks: HookEntry[]): Promise<void>;
  dryRunHook(h: HookEntry): Promise<HookDryRun>;
  memories(): Promise<MemoryCatalog>;
  usage(days: number, source?: string): Promise<UsageReport>;
  // Archives rather than deletes: a fact dropped by mistake stays recoverable.
  forgetMemory(name: string): Promise<void>;
  saveMemory(edit: MemoryEdit): Promise<void>;
  // Every save keeps the version it replaced. These are what let a reader see
  // that history and step back into it; restoring appends rather than rewinds,
  // so the history stays intact either way.
  memoryRevisions(name: string): Promise<MemoryEntry[]>;
  restoreMemory(name: string, revision: number): Promise<void>;
  prepareFileRevert(path: string): Promise<RewindPlan>;
  commitFileRevert(planId: string, resolution?: string): Promise<RewindResult>;
  network(): Promise<NetworkSettings>;
  // password empty keeps whatever is stored; clearPassword removes it.
  saveNetwork(s: NetworkSettings, password: string, clearPassword: boolean): Promise<NetworkSettings>;
  diagnoseNetwork(): Promise<NetworkProbe[]>;
  shell(): Promise<ShellSettings>;
  // Persisted, then the runtime is rebuilt: boot resolves the interpreter while
  // assembling and hands it to the shell tool, so a choice cannot reach a
  // runtime that is already up. Refused mid-turn, like every other rebuild.
  // An empty path leaves the executable to detection.
  saveShell(prefer: string, path: string): Promise<ShellSettings>;
  permissions(): Promise<PermissionRules>;
  // Replaces all three lists at once, then rebuilds — the gate is assembled
  // with the runtime, so a rule cannot reach one that is already up. Every rule
  // is validated by the parser the gate itself uses, so a typo comes back as an
  // error here rather than as a rule that silently never matches.
  savePermissions(lists: PermissionLists): Promise<PermissionRules>;
  sandbox(): Promise<SandboxSettings>;
  saveSandbox(s: SandboxSettings): Promise<SandboxSettings>;
  // null in a browser tab: there is no window to keep running.
  trayPrefs(): Promise<TrayPrefs | null>;
  setTrayPrefs(icon: boolean, closeToTray: boolean): Promise<TrayPrefs | null>;
  // null when the config file reads. Everything else here writes to it.
  configProblem(): Promise<ConfigProblem | null>;
  repairConfig(): Promise<ConfigRepair>;
  mcp(root?: string): Promise<McpCatalog>;
  // Retries a failed or disconnected server and answers with its new state, so
  // the caller never has to race a follow-up GET against the connect.
  reconnectMcp(name: string): Promise<{ state: string; tools?: number; error?: string }>;
  setMcpEnabled(name: string, enabled: boolean, scope?: ScopeLayer, root?: string): Promise<void>;
  clearMcpOverride(name: string, root?: string): Promise<void>;
  // The project a capability listing answers for, on its own — a surface can
  // name its folder before either list has loaded.
  capabilityScope(): Promise<CapabilityScope>;
  // Every project the picker may switch between, so one folder's capabilities
  // can be managed from another without repointing the session.
  capabilityScopes(): Promise<CapabilityScope[]>;
  // Resolves a pasted block without touching anything. Separate from install so
  // the user sees what would run before agreeing to it.
  parseMcp(input: string): Promise<McpDraft>;
  installMcp(server: McpDraftServer, scope: McpInstallScope): Promise<McpInstallResult>;
  removeMcp(name: string): Promise<{ disconnected: boolean; stillConfigured: boolean }>;
  // An account is only for the networked surfaces (forum, crash follow-ups);
  // nothing in the agent loop calls these.
  // Updating the app is the shell's job, not the kernel's: only the shell knows
  // its install layout. A browser tab has no shell and gets an empty hub.
  // Empty means the job rides the main model — the default for every role, and
  // a real answer rather than a missing one.
  roles(): Promise<RoleAssignments>;
  // Persisted, then the runtime is rebuilt: boot reads every role model while
  // assembling, so an assignment cannot reach a runtime that is already up.
  setRole(role: string, ref: string): Promise<void>;
  storage(): Promise<StorageState>;
  planStorageMove(root: string, dir: string): Promise<StoragePlan>;
  moveStorage(root: string, dir: string): Promise<StoragePlan>;
  providers(): Promise<ProviderEntry[]>;
  // The wire formats a source may be saved as. Read from the kernel so a
  // protocol added there reaches this panel without a frontend release.
  protocols(): Promise<Protocol[]>;
  // Asks an endpoint what it is. Writes nothing — the answer is shown for
  // confirmation, because only the person holding the key knows what they
  // bought.
  probeProvider(baseUrl: string, apiKey: string): Promise<ProviderProbe>;
  // Re-probes what is already saved, so "is the key still good, and is this
  // still the protocol we recorded" is one button rather than a re-add.
  checkProvider(name: string): Promise<ProviderCheck>;
  saveProvider(draft: ProviderDraft): Promise<void>;
  // Changes only the fields the form owns. Saving a whole entry instead would
  // drop the per-model prices and effort lists it cannot show.
  editProvider(edit: ProviderEdit): Promise<void>;
  setProviderWebSearch(name: string, on: boolean): Promise<void>;
  setProviderThinking(name: string, on: boolean): Promise<void>;

  // Whether the opening sequence still owes this machine a showing, and the
  // acknowledgement that closes it out.
  welcomeSeen(): Promise<boolean>;
  markWelcomed(): Promise<void>;
  removeProvider(name: string): Promise<void>;
  versions(): Promise<VersionHub>;
  pinVersion(version: string): Promise<void>;
  // Installs a published version, forward or back — the same call either way,
  // because a rollback that took a second code path would be the less-tested
  // one. Resolves only on failure: a success ends with the process handing over
  // to the build it just installed.
  goToVersion(version: string): Promise<void>;
  // Returns an unsubscribe. A browser tab has no shell to report progress, so
  // it never fires there.
  onUpdateProgress(cb: (p: UpdateProgress) => void): () => void;
  account(): Promise<AccountState>;
  accountLogin(): Promise<DeviceGrant>;
  accountPoll(deviceCode: string): Promise<{ status: "pending" | "complete"; slowDown?: boolean }>;
  accountLogout(): Promise<void>;
  workspaces(): Promise<WorkspaceInfo>;
  // Rebuilds the whole runtime against another folder. The conversation does
  // not come along, so the caller has to reload the transcript afterwards.
  setWorkspace(path: string): Promise<void>;
  isolateWorkspace(): Promise<void>;
  // The native folder picker, or "" where there is none (a browser tab) or
  // when the user cancelled. Only the shell can open one.
  // The native folder picker: the chosen path, "" when the user cancelled, and
  // null where the host has none at all. Cancelling and having no picker are
  // different answers, and a caller that conflates them asks twice.
  pickFolder(): Promise<string | null>;
  // Hands a link to the platform browser. A webview has nowhere to put a new
  // tab — a target="_blank" click there does nothing at all — and navigating in
  // place would replace the session with the page.
  openExternal(url: string): Promise<void>;
  sessions(): Promise<SessionEntry[]>;
  resume(path: string): Promise<void>;
  newSession(): Promise<void>;
  deleteSession(name: string): Promise<void>;
  status(): Promise<SessionStatus>;
  /** The provider's wallet, or null when this provider has no wallet endpoint —
   *  an absence to render as nothing, which is not the same as a wallet that
   *  could not be read. That one refuses, with a code saying which failure. */
  balance(): Promise<WalletReading | null>;
  history(): Promise<HistoryMessage[]>;
  // One entry per user turn, oldest first. files is how many the writer tools
  // touched that turn — zero is normal and means there is nothing to restore.
  checkpoints(): Promise<Checkpoint[]>;
  // Two calls, because a partial-coverage plan needs consent before it runs:
  // prepare reports what will and will not be restored, commit applies the plan
  // the user agreed to.
  prepareRewind(turn: number, scope: RewindScope): Promise<RewindPlan>;
  commitRewind(planId: string): Promise<RewindResult>;
  // Reverses a committed rewind. Only reachable while the caller still holds the
  // transaction id the commit returned.
  undoRewind(transactionId: string): Promise<void>;
  // Replaying the persisted wire frames rebuilds the trajectory pane row for
  // row; the live stream only ever covers the current connection.
  trajectory(): Promise<WireEvent[]>;
  /** The run graph as the kernel's durable facts justify it, with the frame it
   *  is at least as new as. This is the execution model's authority: it is what
   *  a view is built from, and what it goes back to after a hole — the delta
   *  stream carries the same facts sooner, never instead. */
  executionGraph(): Promise<ExecutionGraphRead>;
  // What the working tree actually differs by. Tool events cannot answer it: a
  // file created and then removed by a shell command leaves both events behind
  // and nothing on disk.
  changes(): Promise<WorkspaceChanges>;
  // What one of those paths actually differs by. The list says a file moved;
  // only this says how, and asking per path is what keeps a session that
  // touched two hundred files from shipping two hundred diffs nobody opened.
  changeDiff(path: string): Promise<ChangeDiff>;
  // Saves bytes into the workspace's attachment directory and returns the
  // "@path" token a turn references it by. This is the door for what has no
  // path to offer — the clipboard, and a browser tab's dropped File. A window
  // that knows where the file came from uses dropRefs and copies nothing.
  // Naming the bytes is what stores them as that kind of file; unnamed bytes
  // must prove they are an image.
  attach(blob: Blob, name?: string): Promise<Attachment>;
  // Names what dropped paths are called inside a turn, in the order given.
  dropRefs(paths: string[]): Promise<DroppedRef[]>;
  // onGap fires when frames were lost beyond what the stream can replay: the
  // cue to re-read each model from its own authority rather than to keep
  // rendering a conversation with a hole in it.
  // bootstrap lets a read model that has its own snapshot join without a seam:
  // it reads one and answers with the frame that snapshot is at least as new
  // as. Live frames are held until the replay after that number lands, so the
  // model folds one ordered stream and folds nothing twice. The number belongs
  // to the stream, not to the model — several snapshots resume from the lowest
  // of their watermarks, because a duplicate folds and a gap does not.
  subscribe(onEvent: (ev: WireEvent) => void, onGap?: () => void, bootstrap?: () => Promise<number>): () => void;

  submit(text: string): Promise<void>;
  // /submit 409s once a turn holds the session. Mid-turn input is durable and
  // goes through the inbox, which delivers it at the next tool boundary. The
  // receipt is what makes it cancellable while it waits there.
  steer(text: string): Promise<Queued>;
  // A whole turn, queued because one is already running. The kernel
  // refuses /submit with a code rather than a sentence, so this is what
  // the client does about it instead of showing anyone the refusal.
  queueFollowup(text: string): Promise<Queued>;
  // Takes a queued line back before the turn reads it, and refuses once it has.
  cancelQueued(itemId: string): Promise<void>;
  // The queue as the kernel holds it. Read on every inbox_changed frame: the
  // event says it moved and nothing more, so that one answer serves every
  // client instead of each rebuilding the queue from the frames it saw.
  queue(): Promise<Queue>;
  // The entry's full text. preview is cut to a manifest-sized line, so editing
  // against it would silently shorten what the user actually said.
  readQueued(itemId: string): Promise<string>;
  // Rewrites a waiting line. The turn reads the stored body, not what was on
  // screen when it was typed, so this is how second thoughts reach it.
  editQueued(itemId: string, text: string): Promise<void>;
  moveQueued(itemId: string, toIndex: number): Promise<void>;
  // Puts a blocked entry back in line. What blocked it is the kernel's to say
  // and the panel's to show; retrying is the user deciding it is worth another.
  retryQueued(itemId: string): Promise<void>;
  // Re-freezes the files an entry references. A line that waited through the
  // work that changed them would otherwise arrive quoting what is no longer.
  refreshQueued(itemId: string): Promise<void>;
  // Holds delivery without dropping anything. The queue keeps filling; nothing
  // leaves it until this is turned back off.
  setQueuePaused(paused: boolean): Promise<void>;
  cancel(): Promise<void>;
  approve(id: string, verdict: ApprovalVerdict): Promise<void>;
  // A plan card has three outcomes, not two. The id is the host's; the frontend
  // sends it back untouched rather than describing the state it thinks it is in.
  planDecision(id: string, action: PlanAction): Promise<void>;
  answer(id: string, answers: { questionId: string; selected: string[] }[]): Promise<void>;
  // Installed theme packs and which one is active. The list carries every
  // pack's tokens so a picker can preview without a second request.
  context(): Promise<ContextBreakdown>;
  // What the host asked a person and how it ended. Read whole on every
  // adjudications_changed frame: "interrupted" is derived from an open record
  // plus who is waiting, and only the kernel knows the second half.
  adjudications(): Promise<Adjudications>;
  // Declares what the model this session runs on actually holds. Nothing can
  // probe it — a relay forwards a third party's model under its own name — so
  // it is written against that model and the runtime is rebuilt, which is what
  // gives the gauge a denominator and turns automatic compaction back on.
  // Refused mid-turn, like every other rebuild.
  setContextWindow(window: number): Promise<ContextBreakdown>;
  themes(): Promise<ThemePack[]>;
  // The reader's own size, type and picture — kept apart from the pack, which
  // is a palette somebody else authored.
  appearance(): Promise<Appearance>;
  saveAppearance(look: Appearance): Promise<Appearance>;
  uploadWallpaper(blob: Blob): Promise<Appearance>;
  clearWallpaper(): Promise<void>;
  // Where the user put each extension surface, keyed "<pluginId>:<surfaceId>".
  // It outranks what the extension asked for; an empty slot hands the decision
  // back rather than hiding the surface.
  surfaceSlots(): Promise<Record<string, string>>;
  assignSurface(surface: string, slot: string): Promise<void>;
  activateTheme(id: string): Promise<void>;
  // Extension surfaces arrive on the event stream; these carry the user's half
  // back — an action the card offered, or a published form's values.
  invokeExtensionAction(name: string): Promise<string>;
  submitExtensionForm(pluginId: string, surfaceId: string, values: Record<string, unknown>): Promise<void>;

  setPlanMode(on: boolean): Promise<void>;
  setApprovalMode(mode: ApprovalMode): Promise<void>;
  setPreset(preset: Preset): Promise<void>;
  setModel(ref: string): Promise<void>;
  setEffort(effort: string): Promise<void>;
  compaction(): Promise<CompactionSettings>;
  saveCompaction(softLimitTokens: number): Promise<CompactionSettings>;
  setGoal(text: string): Promise<void>;
}

