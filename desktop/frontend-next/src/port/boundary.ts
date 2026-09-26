// boundary.ts — the tool boundary as an editor sees it: which calls are
// refused before they run, and how far an approved one may write.

// The fine-grained gate. The three lists are checked in the order deny → ask →
// allow, and mode decides only what nothing matched. deny is the one entry no
// approval prompt can talk its way past, which is why it is worth a screen.
export interface PermissionLists {
  mode: string;
  allow: string[];
  ask: string[];
  deny: string[];
}

// What an editor needs beyond the lists: the file a save lands in, and — only
// when a project config declares its own — the merge actually in force, which
// an edit here cannot move.
export interface PermissionRules extends PermissionLists {
  path: string;
  // What was allowed on a prompt for this session alone. Nothing wrote it down,
  // so the file on screen is less than the agent may currently do.
  granted?: string[];
  shadowedBy?: string;
  effective?: PermissionLists;
}

// Where an approved write may land, and whether bash runs jailed. The
// effective* fields are what the confiner will use: an empty workspaceRoot is
// not "anywhere", it is "the session directory", and an empty bash is not "off".
export interface SandboxSettings {
  bash: string;
  network: boolean;
  workspaceRoot: string;
  allowWrite: string[];
  // Refuse a whole-file write over a change made since the agent last saw the file.
  protectChangedFiles: boolean;
  effectiveWriteRoots: string[];
  // The mode that will actually run, which the configured one does not always
  // survive to: Windows has no OS backend and forces off, an unset value
  // enforces elsewhere, and a project file outranks this one.
  effectiveBash: string;
  // False where this host has no OS sandbox at all — enforce would then refuse
  // every bash call rather than run it unconfined, so the switch says so
  // instead of pretending to work.
  available: boolean;
  why?: string;
  // The same explanation's code, which is what has wording in this window.
  whyCode?: string;
  platform: string;
  path: string;
  shadowedBy?: string;
}

// What a settings surface shows in place of a save it cannot perform. The
// kernel refuses every write while the file will not parse, so this arrives
// before anything is tried rather than as each panel's own error.
export interface ConfigProblem {
  path: string;
  line?: number;
  key?: string;
  excerpt?: string;
  // The offending line said the other way. Present only when the file parses
  // after the change, so it is an offer rather than a guess.
  repair?: string;
  // Which values are on screen instead: "last-known-good" or "defaults".
  recovered?: string;
  detail?: string;
}

export interface ConfigRepair {
  backup: string;
  problem: ConfigProblem | null;
}

// The built-in browser tools as the user file holds them, beside what this
// workspace will run with: a project file may set the same key and outrank it.
export interface BrowserToolsSettings {
  enabled: boolean;
  effective: boolean;
  path: string;
}
