// What a turn runs inside: the shell commands are handed to, and how much of
// the model's window is left.
// GET /context: how full the window is, and what is filling it. The classes
// are measured with the same estimator the compaction thresholds use.
export interface ContextBreakdown {
  used: number;
  window: number;
  system: number;
  tools: number;
  user: number;
  reply: number;
  output: number;
  // Where this session actually folds — the lower of the window share and the
  // economic soft limit. Zero means nothing folds automatically. The window is
  // not a substitute: against a 1M window the default limit fires at 16%.
  compact_at: number;
  // Which of the two bounds that is, and where the window's own line sits.
  // The kernel decides; a panel comparing the pair itself would own a second
  // copy of the rule, and one without them shows a fold point it cannot explain.
  boundary: string;
  capacity_at: number;
}

// One interpreter this machine really has. path is where it was probed, so a
// host carrying two bashes offers two rows instead of one ambiguous name.
export interface ShellOption {
  name: string;
  path: string;
  version?: string;
  supportsAndAnd: boolean;
  // What saveShell takes to select this one.
  prefer: string;
}

// The shell tool's interpreter as the editor needs it. options is what was
// found on this machine — never a fixed list, because offering a shell that is
// not installed is a switch that breaks every command it accepts.
export interface ShellSettings {
  prefer: string;
  path?: string;
  effective: ShellOption;
  auto: ShellOption;
  options?: ShellOption[];
  platform: string;
}
