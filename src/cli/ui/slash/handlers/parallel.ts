import { t } from "@/i18n/index.js";
import { SkillStore } from "@/skills.js";
import type { SlashHandler } from "../dispatch.js";

const parallel: SlashHandler = (args, _loop, ctx) => {
  const store = new SkillStore({
    projectRoot: ctx.codeRoot,
    homeDir: ctx.homeDir,
  });

  const skill = store.read("parallel");
  if (!skill) {
    return {
      info: [
        "Parallel skill not installed.",
        "",
        "Create ~/.reasonix/skills/parallel.md with the multi-agent parallel execution playbook.",
        "See https://github.com/nideweilaiya/Collusion for details.",
      ].join("\n"),
    };
  }

  const task = args.join(" ").trim();
  if (!task) {
    return {
      info: [
        "Usage: /parallel <task>",
        "",
        "Description: Decompose <task> into parallel subtasks, assign each to a specialized",
        "reasonix run agent, execute concurrently via blackboard+advisor architecture.",
        "",
        "Examples:",
        "  /parallel 审查当前项目的所有安全漏洞并生成修复方案",
        "  /parallel 为这个API设计单元测试、集成测试和性能测试",
        "  /parallel 同时生成用户文档、API文档和部署文档",
        "",
        "Architecture: Blackboard files (/tmp/parallel_*) serve as the only communication",
        "medium between agents. Each agent has a fixed system prompt for ~85% cache hit rate.",
      ].join("\n"),
    };
  }

  const header = `# Skill: ${skill.name}${skill.description ? `\n> ${skill.description}` : ""}`;
  const argsLine = `\n\nArguments: ${task}`;
  const payload = `${header}\n\n${skill.body}${argsLine}`;

  return {
    info: `Parallel · ${skill.name} — decomposing into parallel subtasks\n       Task: ${task.slice(0, 80)}${task.length > 80 ? "…" : ""}`,
    resubmit: payload,
  };
};

export const handlers: Record<string, import("../dispatch.js").SlashHandler> = {
  parallel,
};
