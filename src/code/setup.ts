import { DeepSeekClient } from "../client.js";
import {
  loadBaseUrl,
  loadEditMode,
  loadProjectShellAllowed,
  loadResolvedSkillPaths,
  searchEnabled,
  webSearchEndpoint,
  webSearchEngine,
} from "../config.js";
import { bootstrapSemanticSearchInCodeMode } from "../index/semantic/tool.js";
import { ToolRegistry } from "../tools.js";
import { registerChoiceTool } from "../tools/choice.js";
import { registerFilesystemTools } from "../tools/filesystem.js";
import { JobRegistry } from "../tools/jobs.js";
import { registerMemoryTools } from "../tools/memory.js";
import { registerPlanTool } from "../tools/plan.js";
import { registerScaffoldTools } from "../tools/scaffold.js";
import { registerShellTools } from "../tools/shell.js";
import { type SkillInstalledHook, registerSkillTools } from "../tools/skills.js";
import { formatSubagentResult, spawnSubagent } from "../tools/subagent.js";
import { registerTodoTool } from "../tools/todo.js";
import { registerWebTools } from "../tools/web.js";

export interface CodeToolsetOpts {
  rootDir: string;
  /** Fired after `install_skill` writes a new skill — desktop wires this to push a fresh `$skills` event so the sidebar updates without a tab reload. */
  onSkillInstalled?: SkillInstalledHook;
  /** Fired after `run_background` / `stop_job` mutate the JobRegistry — desktop pushes a fresh `$jobs` event so the popover updates without waiting for poll. */
  onJobsChanged?: () => void;
}

export interface CodeToolset {
  tools: ToolRegistry;
  jobs: JobRegistry;
  registerRooted: (root: string) => void;
  reBootstrapSemantic: (root: string) => Promise<{ enabled: boolean }>;
  semantic: { enabled: boolean };
}

export async function buildCodeToolset(opts: CodeToolsetOpts): Promise<CodeToolset> {
  const tools = new ToolRegistry();
  const jobs = new JobRegistry();

  const registerRooted = (root: string): void => {
    registerFilesystemTools(tools, { rootDir: root });
    registerShellTools(tools, {
      rootDir: root,
      extraAllowed: () => loadProjectShellAllowed(root),
      allowAll: () => loadEditMode() === "yolo",
      jobs,
      onJobsChanged: opts.onJobsChanged,
    });
    registerMemoryTools(tools, { projectRoot: root });
  };

  const reBootstrapSemantic = async (root: string): Promise<{ enabled: boolean }> => {
    const result = await bootstrapSemanticSearchInCodeMode(tools, root);
    if (!result.enabled) tools.unregister("semantic_search");
    return result;
  };

  registerRooted(opts.rootDir);
  registerPlanTool(tools);
  registerChoiceTool(tools);
  registerTodoTool(tools);
  registerScaffoldTools(tools, { projectRoot: opts.rootDir });
  if (searchEnabled()) {
    registerWebTools(tools, {
      webSearchEngine: webSearchEngine(),
      webSearchEndpoint: webSearchEndpoint(),
    });
  }
  // Lazy: constructing DeepSeekClient throws when DEEPSEEK_API_KEY is unset,
  // which would kill `reasonix code` before the setup wizard can prompt for
  // one. Defer to first subagent dispatch — by then the user has either keyed
  // in or we error per-call instead of at boot.
  let subagentClient: DeepSeekClient | null = null;
  registerSkillTools(tools, {
    projectRoot: opts.rootDir,
    customSkillPaths: loadResolvedSkillPaths(opts.rootDir),
    onSkillInstalled: opts.onSkillInstalled,
    subagentRunner: async (skill, task, signal) => {
      if (!subagentClient) subagentClient = new DeepSeekClient({ baseUrl: loadBaseUrl() });
      const result = await spawnSubagent({
        client: subagentClient,
        parentRegistry: tools,
        parentSignal: signal,
        system: skill.body,
        task,
        model: skill.model,
        allowedTools: skill.allowedTools,
        skillName: skill.name,
      });
      return formatSubagentResult(result);
    },
  });

  const semantic = await reBootstrapSemantic(opts.rootDir);

  return { tools, jobs, registerRooted, reBootstrapSemantic, semantic };
}
