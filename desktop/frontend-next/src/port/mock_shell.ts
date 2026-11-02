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
  async context(): Promise<ContextBreakdown> {
    return { ...this.ctx };
  }

  // The gauge and the settings sheet read the same pair here, because the
  // kernel has only one: a fixture answering 128k to one and 1M to the other
  // draws a session no kernel can produce.
  async compaction(): Promise<CompactionSettings> {
    return {
      ratio: RATIO,
      context_window: this.ctx.window,
      trigger: this.ctx.compact_at,
      path: "~/.reasonix/config.toml",
    };
  }

  // The kernel rebuilds on this and answers with the fresh gauge; here the
  // window is simply the one that was declared, which is what the panel needs
  // to be developed against a source that reports none.
  async setContextWindow(window: number): Promise<ContextBreakdown> {
    this.ctx = { ...this.ctx, window, ...bounds(window) };
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
  async saveShell(prefer: string, path: string, args: string[] = []): Promise<ShellSettings> {
    if (args.length > 0) {
      if (!/wsl\.exe$/i.test(path)) throw new Error(`这个 shell 用不了：${path} ${args.join(" ")}: did not run a command`);
      const argv = [path, ...args];
      this.sh = { ...this.sh, prefer, path, args, argv, effective: { ...GIT_BASH, name: "bash", path } };
      return { ...this.sh };
    }
    const found = (this.sh.options ?? []).find((o) => (path ? o.path === path : o.prefer === prefer));
    if (path && !found) throw new Error(`这个 shell 用不了：${path}: no such executable`);
    this.sh = { ...this.sh, prefer, path, args: undefined, argv: undefined, effective: prefer === "auto" ? this.sh.auto : (found ?? this.sh.auto) };
    return { ...this.sh };
  }
}

// 够把分段条和悬停面板画出来的一份构成：工具定义比对话本身还大，正是这个面板
// 要让人看见的那种情况。
const RATIO = 0.9;

function bounds(window: number) {
  const capacity = Math.round(window * RATIO);
  return { compact_at: capacity, capacity_at: capacity, boundary: "capacity" };
}

const CONTEXT: ContextBreakdown = {
  used: 24800, window: 128000, ...bounds(128000),
  system: 5200, tools: 7400, user: 1800, reply: 3100, output: 7300,
  largest: [
    { class: "output", tool: "read_file", tokens: 3900 },
    { class: "output", tool: "grep", tokens: 2100, active: true },
    { class: "output", tool: "bash", tokens: 800 },
    { class: "user", tokens: 900, active: true },
    { class: "user", tokens: 600 },
    { class: "reply", tokens: 1400 },
  ],
};
