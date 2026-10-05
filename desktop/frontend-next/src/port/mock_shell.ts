import type { CompactionSettings, ContextBreakdown, ShellOption, ShellSettings } from "./port";
import { MockExtensions } from "./mock_ext";

// The interpreter half of the fixture. It stands on a Windows host because that
// is the only interesting shape: three shells installed, two of them PowerShell,
// and which one gets picked decides whether the agent may write '&&' at all.
// Chained onto MockExtensions for the reason given there — MockPort satisfies
// AgentPort in one declaration, and each face keeps its own file.
const GIT_BASH: ShellOption = {
  name: "git-bash",
  path: "C:\\Program Files\\Git\\bin\\bash.exe",
  supportsAndAnd: true,
  prefer: "bash",
};

const PWSH: ShellOption = {
  name: "pwsh",
  path: "C:\\Program Files\\PowerShell\\7\\pwsh.exe",
  version: "7+",
  supportsAndAnd: true,
  prefer: "pwsh",
};

const POWERSHELL: ShellOption = {
  name: "powershell",
  path: "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe",
  version: "5.1",
  supportsAndAnd: false,
  prefer: "powershell",
};

export class MockShell extends MockExtensions {
  private ctx: ContextBreakdown = CONTEXT;
  // Stored, not resolved: zero is the default and negative retires the bound,
  // which is the distinction the three choices are made of.
  private softLimit = 0;

  async context(): Promise<ContextBreakdown> {
    return { ...this.ctx };
  }

  // The gauge and the settings sheet read the same pair here, because the
  // kernel has only one: a fixture answering 128k to one and 1M to the other
  // draws a session no kernel can produce.
  async compaction(): Promise<CompactionSettings> {
    return {
      soft_limit_tokens: this.softLimit,
      default_soft_limit: DEFAULT_SOFT_LIMIT,
      ratio: RATIO,
      context_window: this.ctx.window,
      trigger: this.ctx.compact_at,
      path: "~/.reasonix/config.toml",
    };
  }

  // The kernel rebuilds on this and the gauge moves with it, so the fixture
  // moves both: a bound that saves without changing where the session folds is
  // the one failure this editor has to be developed against.
  async saveCompaction(softLimitTokens: number): Promise<CompactionSettings> {
    this.softLimit = softLimitTokens;
    this.ctx = { ...this.ctx, ...bounds(this.ctx.window, softLimitTokens) };
    return this.compaction();
  }

  // The kernel rebuilds on this and answers with the fresh gauge; here the
  // window is simply the one that was declared, which is what the panel needs
  // to be developed against a source that reports none.
  async setContextWindow(window: number): Promise<ContextBreakdown> {
    this.ctx = { ...this.ctx, window, ...bounds(window, this.softLimit) };
    return { ...this.ctx };
  }

  private sh: ShellSettings = {
    prefer: "auto",
    effective: GIT_BASH,
    auto: GIT_BASH,
    options: [GIT_BASH, PWSH, POWERSHELL],
    platform: "windows",
  };

  async shell(): Promise<ShellSettings> {
    return { ...this.sh };
  }

  // A path nobody has is the failure the pane has to render, so it is refused
  // here the way the kernel refuses it rather than quietly accepted.
  async saveShell(prefer: string, path: string): Promise<ShellSettings> {
    const found = (this.sh.options ?? []).find((o) => (path ? o.path === path : o.prefer === prefer));
    if (path && !found) throw new Error(`这个 shell 用不了：${path}: no such executable`);
    this.sh = { ...this.sh, prefer, path, effective: prefer === "auto" ? this.sh.auto : (found ?? this.sh.auto) };
    return { ...this.sh };
  }
}

// 够把分段条和悬停面板画出来的一份构成：工具定义比对话本身还大，正是这个面板
// 要让人看见的那种情况。
const RATIO = 0.85;
const DEFAULT_SOFT_LIMIT = 0;

// Which of the two bounds fires, worked out once. The pair is the kernel's
// rule, and a fixture with its own copy of it drifts into states the product
// cannot reach — which is what a fixture exists to rule out.
function bounds(window: number, soft: number) {
  const capacity = Math.round(window * RATIO);
  const economic = soft < 0 ? 0 : soft || DEFAULT_SOFT_LIMIT;
  const compact_at = economic > 0 && economic < capacity ? economic : capacity;
  return { compact_at, capacity_at: capacity, boundary: compact_at < capacity ? "economic" : "capacity" };
}

const CONTEXT: ContextBreakdown = {
  used: 24800, window: 128000, ...bounds(128000, 0),
  system: 5200, tools: 7400, user: 1800, reply: 3100, output: 7300,
};
