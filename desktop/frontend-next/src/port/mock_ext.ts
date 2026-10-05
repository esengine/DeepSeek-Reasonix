import { download } from "./download";
import type { PluginExport, PluginInstallRequest, PluginPackage, PluginPlan, ScopeLayer, SkillCatalog, SkillEntry } from "./port";
import { MockMarket } from "./mock_market";

// The extension half of the fixture: skills, and the packages that bring them.
// MockPort extends this rather than holding one, so the class still satisfies
// AgentPort in a single declaration and the settings pane's whole extension
// tab can be developed with no Go process at all.
export class MockExtensions extends MockMarket {
  private skillList: SkillEntry[] = [
    {
      name: "review", slashName: "review", description: "复核这一轮改动，给出严重度分级",
      scope: "project", path: ".reasonix/skills/review/SKILL.md", subagent: true, enabled: true,
    },
    { name: "init", slashName: "init", description: "为这个仓库生成一份项目说明", scope: "builtin", enabled: true },
    {
      name: "security-review", slashName: "security-review", description: "只读地过一遍安全面",
      scope: "builtin", subagent: true, readOnly: true, effort: "high", enabled: true,
    },
    // No slash name of its own: only model discovery reaches it. This is the
    // row the old list could not show at all.
    {
      name: "release-notes", description: "从提交历史起草发布说明",
      scope: "global", path: "~/.reasonix/skills/release-notes/SKILL.md", enabled: false,
    },
    {
      name: "deploy-runbook", slashName: "deploy-runbook", description: "只在你点名时跑的部署清单",
      scope: "project", path: ".reasonix/skills/deploy-runbook/SKILL.md", manual: true, enabled: true,
    },
  ];

  // Two packages, because the extension page has to read differently for one
  // that only adds skills and one that also runs a process.
  private packages: PluginPackage[] = [
    {
      name: "review-kit", version: "1.4.0", root: "~/.reasonix/plugins/review-kit",
      description: "代码评审的技能与命令", source: "github:acme/review-kit",
      manifestKind: "reasonix", enabled: true,
      skills: [
        { name: "review", description: "按改动范围逐块评审", invocation: "/review-kit:review" },
        { name: "risk", description: "只找会出事的那几处", invocation: "/review-kit:risk" },
      ],
      commands: [{ name: "pr", description: "写一份 PR 说明", invocation: "/review-kit:pr" }],
    },
    {
      name: "notion-bridge", version: "0.9.1", root: "~/.reasonix/plugins/notion-bridge",
      description: "把 Notion 接进来", source: "https://example.com/notion-bridge",
      manifestKind: "claude", enabled: false,
      skills: [{ name: "notes", description: "读写页面", invocation: "/notion-bridge:notes" }],
      hooks: [{ event: "SessionStart", command: "hooks/sync.sh", description: "开会话时同步一次" }],
      mcpServers: [{ name: "notion", transport: "stdio", command: "notion-mcp", autoStart: true }],
      runtime: {
        command: "bin/bridge", args: ["--serve"],
        intercepts: ["tool.before"], capabilities: ["interceptors", "ui"],
      },
    },
  ];

  async skills(): Promise<SkillCatalog> {
    return { implicit: true, skills: this.skillList.map((s) => ({ ...s })) };
  }

  // The fixture has no runtime to restart, but the button's states — pending,
  // settled, refused-while-busy — are exactly what needs designing without a Go
  // process, so it takes long enough to see.
  async reloadExtensions(): Promise<void> {
    await new Promise((r) => setTimeout(r, 600));
  }

  // localSkills is what the fixture counts as this project's exceptions; the
  // real store keys them by repository, which a browser fixture cannot do.
  protected localSkills = new Set<string>();

  async setSkillEnabled(name: string, enabled: boolean, scope: ScopeLayer = "project") {
    const sk = this.skillList.find((x) => x.name === name);
    if (!sk) return;
    sk.enabled = enabled;
    sk.switchScope = scope;
    if (scope === "project") this.localSkills.add(name);
    else this.localSkills.delete(name);
  }

  async clearSkillOverride(name: string) {
    const sk = this.skillList.find((x) => x.name === name);
    if (!sk) return;
    sk.switchScope = undefined;
    sk.enabled = true;
    this.localSkills.delete(name);
  }

  async plugins(): Promise<PluginPackage[]> {
    return this.packages.map((p) => ({ ...p }));
  }

  // The plan is deliberately the loud kind: a package that runs a process, adds
  // a hook and connects a server is what the confirmation page exists for.
  async planPlugin(req: PluginInstallRequest): Promise<PluginPlan> {
    const source = req.source;
    return {
      ok: true, status: "planned", applied: false, source, planId: "high:sha256:mock",
      actions: [
        {
          kind: "plugin", action: "install_plugin_package", status: "planned",
          riskLevel: "high", name: "mock-pack", version: "2.0.0", source,
          target: "~/.reasonix/plugins/mock-pack", manifestKind: "reasonix",
          skills: ["draft", "polish"], skillCount: 2, commandCount: 1, hookCount: 1,
          riskReasons: [
            "FULL TRUST: declares a runtime process (bin/pack --serve) that runs inside Reasonix",
            "registers shell hooks that execute during Reasonix sessions",
          ],
          runtime: {
            command: "bin/pack", args: ["--serve"], intercepts: ["input.receive"],
            capabilities: ["interceptors"], fullTrust: true,
          },
        },
        {
          kind: "mcp", action: "install_mcp_server", status: "planned",
          riskLevel: "medium", name: "pack-docs", transport: "stdio",
          command: "npx", args: ["-y", "pack-docs-mcp"],
          env: { PACK_DOCS_TOKEN: "${PACK_DOCS_TOKEN}" },
          riskReasons: ["starts a process on this machine"],
        },
      ],
      next: "Review the plan, then install.",
    };
  }

  // Replacing keeps one row rather than adding a second: an update is the same
  // package arriving again, which is what the confirmation pane just said.
  async installPlugin(req: PluginInstallRequest): Promise<PluginPlan> {
    const source = req.source;
    const name = req.name ?? "mock-pack";
    this.packages = this.packages.filter((p) => p.name !== name);
    this.packages.push({
      name, version: "2.0.0", root: `~/.reasonix/plugins/${name}`,
      source, manifestKind: "reasonix", enabled: true,
      skills: [
        { name: "draft", description: "起草", invocation: `/${name}:draft` },
        { name: "polish", description: "润色", invocation: `/${name}:polish` },
      ],
      hooks: [{ event: "SessionStart", command: "hooks/boot.sh" }],
    });
    return { ok: true, status: "done", applied: true, source, planId: "high:sha256:mock", actions: [] };
  }

  async setPluginEnabled(name: string, enabled: boolean) {
    const p = this.packages.find((x) => x.name === name);
    if (p) p.enabled = enabled;
    return {};
  }

  async removePlugin(name: string): Promise<PluginPlan> {
    this.packages = this.packages.filter((p) => p.name !== name);
    return { ok: true, status: "done", applied: true, actions: [] };
  }

  // Nothing is written in the fixture; what matters here is that the caller
  // still learns which values an export would have stripped.
  async exportPlugin(name: string): Promise<PluginExport> {
    const p = this.packages.find((x) => x.name === name);
    return { required: p?.mcpServers?.length ? [`${p.name.toUpperCase()}_TOKEN`] : [] };
  }

  // No shell here, so this is the browser path — the same one a served tab
  // takes. Returning a made-up path instead would hide that the file never
  // arrives anywhere.
  async saveText(name: string, content: string): Promise<string | null> {
    download(name, content);
    return null;
  }
}
