import {
  addSkillPath,
  defaultConfigPath,
  loadDisabledSkills,
  loadResolvedSkillPaths,
  loadSkillPaths,
  removeSkillPath,
  resolveSkillDir,
  toggleSkillDisabled,
} from "@/config.js";
import { t } from "@/i18n/index.js";
import { SkillStore, builtinSkillDescription } from "@/skills.js";
import type { SlashHandler } from "../dispatch.js";

function displayDescription(skill: { description: string; name: string; scope: string }): string {
  return skill.scope === "builtin" ? builtinSkillDescription(skill.name) : skill.description;
}

const skill: SlashHandler = (args, _loop, ctx) => {
  const baseDir = ctx.codeRoot ?? process.cwd();
  const configPath = ctx.configPath ?? defaultConfigPath();
  const rawSkillPaths = loadSkillPaths(baseDir, configPath);
  const store = new SkillStore({
    projectRoot: ctx.codeRoot,
    customSkillPaths: loadResolvedSkillPaths(baseDir, configPath),
    skillDir: resolveSkillDir(baseDir, configPath),
    disabledSkills: loadDisabledSkills(configPath),
  });
  const sub = (args[0] ?? "").toLowerCase();

  if (sub === "new" || sub === "init") {
    const name = args[1];
    if (!name) return { info: t("handlers.skill.newUsage") };
    const wantsGlobal = args.slice(2).includes("--global") || !ctx.codeRoot;
    const result = store.create(name, wantsGlobal ? "global" : "project");
    if ("error" in result) {
      return { info: t("handlers.skill.newError", { reason: result.error }) };
    }
    return { info: t("handlers.skill.newCreated", { name, path: result.path }) };
  }

  if (sub === "paths") {
    const action = (args[1] ?? "list").toLowerCase();
    if (action === "add") {
      const raw = args.slice(2).join(" ").trim();
      if (!raw) return { info: t("handlers.skill.pathsAddUsage") };
      const result = addSkillPath(raw, baseDir, configPath);
      if ("error" in result) return { info: result.error };
      return {
        info: [
          result.added
            ? t("handlers.skill.pathsAdded", { path: result.path })
            : t("handlers.skill.pathsAlready", { path: result.path }),
          t("handlers.skill.pathsRestartHint"),
        ].join("\n"),
      };
    }
    if (action === "remove" || action === "rm") {
      const raw = args.slice(2).join(" ").trim();
      if (!raw) return { info: t("handlers.skill.pathsRemoveUsage") };
      const result = removeSkillPath(raw, baseDir, configPath);
      if (!result.removed)
        return { info: t("handlers.skill.pathsRemoveNotFound", { target: raw }) };
      return {
        info: [
          t("handlers.skill.pathsRemoved", { path: result.path ?? raw }),
          t("handlers.skill.pathsRestartHint"),
        ].join("\n"),
      };
    }
    if (action !== "list") return { info: t("handlers.skill.pathsUsage") };
    const roots = store.roots();
    const customRoots = store.customRoots();
    const lines = [t("handlers.skill.pathsHeader")];
    for (const root of roots) {
      const customIndex =
        root.scope === "custom" ? customRoots.findIndex((r) => r.dir === root.dir) : -1;
      const label = customIndex >= 0 ? `${root.scope} #${customIndex + 1}` : root.scope;
      const raw = customIndex >= 0 ? rawSkillPaths[customIndex] : undefined;
      const suffix = raw && raw !== root.dir ? ` raw=${raw} resolved=${root.dir}` : root.dir;
      lines.push(
        `  ${String(root.priority + 1).padStart(2, " ")}. ${label.padEnd(10)} ${root.status.padEnd(13)} ${suffix}`,
      );
    }
    lines.push("", t("handlers.skill.pathsPriority"));
    return { info: lines.join("\n") };
  }

  if (sub === "enable") {
    const name = args[1];
    if (!name) return { info: t("handlers.skill.enableUsage") };
    const result = toggleSkillDisabled("enable", name, configPath);
    if ("error" in result) return { info: result.error };
    return { info: t("handlers.skill.enabled", { name }) };
  }

  if (sub === "disable") {
    const name = args[1];
    if (!name) return { info: t("handlers.skill.disableUsage") };
    const result = toggleSkillDisabled("disable", name, configPath);
    if ("error" in result) return { info: result.error };
    return { info: t("handlers.skill.disabled", { name }) };
  }

  if (sub === "" || sub === "list" || sub === "ls") {
    const skills = store.list();
    if (skills.length === 0) {
      const lines = [t("handlers.skill.listEmpty")];
      if (store.hasProjectScope()) {
        lines.push(t("handlers.skill.listProjectScope"));
      }
      lines.push(t("handlers.skill.listGlobalScope"));
      if (!store.hasProjectScope()) {
        lines.push(t("handlers.skill.listProjectOnly"));
      }
      lines.push(
        "",
        t("handlers.skill.listFrontmatter"),
        t("handlers.skill.listInvoke"),
        "",
        t("handlers.skill.listEmptyNewHint"),
      );
      return { info: lines.join("\n") };
    }
    const lines = [t("handlers.skill.listHeader", { count: skills.length })];
    for (const s of skills) {
      const scope = `(${s.scope})`.padEnd(11);
      const name = s.name.padEnd(24);
      const resolvedDesc = displayDescription(s);
      const desc = resolvedDesc.length > 70 ? `${resolvedDesc.slice(0, 69)}…` : resolvedDesc;
      const shortPath = s.path.replace(baseDir, ".");
      lines.push(`  ${scope} ${name}  ${desc}  ${shortPath}`);
    }
    const disabledSet = new Set(loadDisabledSkills(configPath));
    if (disabledSet.size > 0) {
      lines.push("", t("handlers.skill.disabledHeader", { count: disabledSet.size }));
      for (const name of [...disabledSet].sort()) {
        lines.push(`  (disabled) ${name}`);
      }
    }
    lines.push("");
    lines.push(t("handlers.skill.listFooter"));
    return { info: lines.join("\n") };
  }

  if (sub === "show" || sub === "cat") {
    const target = args[1];
    if (!target) return { info: t("handlers.skill.showUsage") };
    const found = store.read(target);
    if (!found) return { info: t("handlers.skill.showNotFound", { name: target }) };
    return {
      info: [
        `▸ ${found.name}  (${found.scope})`,
        found.description ? `  ${displayDescription(found)}` : "",
        `  ${found.path}`,
        "",
        found.body,
      ]
        .filter((l) => l !== "")
        .join("\n"),
    };
  }

  const name = args[0] ?? "";
  const found = store.read(name);
  if (!found) {
    return { info: t("handlers.skill.runNotFound", { name }) };
  }
  const extra = args.slice(1).join(" ").trim();
  const resolvedDesc = displayDescription(found);
  const header = `# Skill: ${found.name}${resolvedDesc ? `\n> ${resolvedDesc}` : ""}`;
  const argsLine = extra ? `\n\nArguments: ${extra}` : "";
  const payload = `${header}\n\n${found.body}${argsLine}`;
  return {
    info: t("handlers.skill.runInfo", {
      name: found.name,
      args: extra ? ` — ${extra}` : "",
    }),
    resubmit: payload,
  };
};

export const handlers: Record<string, SlashHandler> = {
  skill,
  skills: skill,
};
