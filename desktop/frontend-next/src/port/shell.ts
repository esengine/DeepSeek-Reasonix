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
  compact_at: number;
  boundary: string;
  capacity_at: number;
  // The largest single messages of each class, largest first; sizes and the
  // producing tool, never text. Absent when the session holds none.
  largest?: ContextHolder[];
}

// One of the messages a class is made of. class is the kernel's own name for
// the class (user, reply, output): the panel groups by it rather than by role.
export interface ContextHolder {
  class: string;
  tool?: string;
  tokens: number;
  // Belongs to the turn still running, which a fold cannot take.
  active?: boolean;
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
  // Launcher arguments saved for path, and the exact command line that was run
  // to verify them when they are in force (absent when they fell back to auto).
  args?: string[];
  argv?: string[];
  effective: ShellOption;
  auto: ShellOption;
  options?: ShellOption[];
  platform: string;
}
