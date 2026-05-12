import { DeepSeekClient } from "../client.js";
import {
  loadBaseUrl,
  loadEditMode,
  loadProjectShellAllowed,
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
import { registerSkillTools } from "../tools/skills.js";
import { formatSubagentResult, spawnSubagent } from "../tools/subagent.js";
import { registerTodoTool } from "../tools/todo.js";
import { registerWebTools } from "../tools/web.js";

export interface CodeToolsetOpts {
  rootDir: string;
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
  const subagentClient = new DeepSeekClient({ baseUrl: loadBaseUrl() });
  registerSkillTools(tools, {
    projectRoot: opts.rootDir,
    subagentRunner: async (skill, task, signal) => {
      const result = await spawnSubagent({
        client: subagentClient,
        parentRegistry: tools,
        parentSignal: signal,
        system: skill.body,
        task,
        model: skill.model,
        allowedTools: skill.allowedTools,
      });
      return formatSubagentResult(result);
    },
  });

  const semantic = await reBootstrapSemantic(opts.rootDir);

  return { tools, jobs, registerRooted, reBootstrapSemantic, semantic };
}
