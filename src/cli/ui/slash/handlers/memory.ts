import { basename } from "node:path";
import { t } from "@/i18n/index.js";
import { PROJECT_MEMORY_FILE, memoryEnabled, readProjectMemory } from "@/memory/project.js";
import { type MemoryScope, MemoryStore, effectivePriority } from "@/memory/user.js";
import type { SlashHandler } from "../dispatch.js";
import { resolveMemoryTarget } from "../helpers.js";

/** Parses optional flags out of a slash-arg list. Returns the type filter (`--type X` or `--type=X`) and the residue without those tokens. */
function pickTypeFlag(args: string[]): { type: string | null; rest: string[] } {
  const rest: string[] = [];
  let type: string | null = null;
  for (let i = 0; i < args.length; i++) {
    const a = args[i] ?? "";
    if (a === "--type" || a === "-t") {
      const next = args[i + 1];
      if (next) {
        type = next;
        i++;
      }
      continue;
    }
    const eq = a.match(/^--type=(.+)$/);
    if (eq) {
      type = eq[1] ?? null;
      continue;
    }
    rest.push(a);
  }
  return { type, rest };
}

const memory: SlashHandler = (args, _loop, ctx) => {
  if (!memoryEnabled()) {
    return { info: t("handlers.memory.disabled") };
  }
  if (!ctx.memoryRoot) {
    return { info: t("handlers.memory.noRoot") };
  }
  const store = new MemoryStore({ projectRoot: ctx.codeRoot });
  const { type: typeFilter, rest: filteredArgs } = pickTypeFlag(args);
  const sub = (filteredArgs[0] ?? args[0] ?? "").toLowerCase();

  if (sub === "list" || sub === "ls") {
    const all = store.list();
    const entries = typeFilter ? all.filter((e) => e.type === typeFilter) : all;
    if (entries.length === 0) {
      return {
        info: typeFilter
          ? `no memories with type='${typeFilter}'. (${all.length} total across all types)`
          : t("handlers.memory.listEmpty"),
      };
    }
    const header = typeFilter
      ? `▸ memory entries — type=${typeFilter} (${entries.length}/${all.length})`
      : t("handlers.memory.listHeader", { count: entries.length });
    const lines = [header];
    for (const e of entries) {
      const prio = effectivePriority(e);
      const marker = prio === "high" ? "⚠ " : prio === "low" ? "· " : "  ";
      const tag = `${e.scope}/${e.type}`.padEnd(18);
      const name = e.name.padEnd(28);
      const desc = e.description.length > 70 ? `${e.description.slice(0, 69)}…` : e.description;
      lines.push(`${marker}${tag}  ${name}  ${desc}`);
    }
    lines.push("");
    lines.push(t("handlers.memory.listFooter"));
    return { info: lines.join("\n") };
  }

  if (sub === "show" || sub === "cat") {
    const target = args[1];
    if (!target) return { info: t("handlers.memory.showUsage") };
    const resolved = resolveMemoryTarget(store, target);
    if (!resolved) return { info: t("handlers.memory.showNotFound", { target }) };
    try {
      const entry = store.read(resolved.scope, resolved.name);
      return {
        info: [
          `▸ ${entry.scope}/${entry.name}  (${entry.type}, created ${entry.createdAt || "?"})`,
          entry.description ? `  ${entry.description}` : "",
          "",
          entry.body,
        ]
          .filter((l) => l !== "")
          .concat("")
          .join("\n"),
      };
    } catch (err) {
      return { info: t("handlers.memory.showFailed", { reason: (err as Error).message }) };
    }
  }

  if (sub === "forget" || sub === "rm" || sub === "delete") {
    const target = args[1];
    if (!target) return { info: t("handlers.memory.forgetUsage") };
    const resolved = resolveMemoryTarget(store, target);
    if (!resolved) return { info: t("handlers.memory.forgetNotFound", { target }) };
    try {
      const ok = store.delete(resolved.scope, resolved.name);
      return {
        info: ok
          ? t("handlers.memory.forgetInfo", { scope: resolved.scope, name: resolved.name })
          : t("handlers.memory.forgetFailed", { scope: resolved.scope, name: resolved.name }),
      };
    } catch (err) {
      return { info: t("handlers.memory.forgetError", { reason: (err as Error).message }) };
    }
  }

  if (sub === "clear") {
    const rawScope = (args[1] ?? "").toLowerCase();
    if (rawScope !== "global" && rawScope !== "project") {
      return { info: t("handlers.memory.clearUsage") };
    }
    if ((args[2] ?? "").toLowerCase() !== "confirm") {
      return {
        info: t("handlers.memory.clearConfirm", { scope: rawScope }),
      };
    }
    const scope = rawScope as MemoryScope;
    const all = store.list();
    const inScope = all.filter((e) => e.scope === scope);
    const expiring =
      scope === "project"
        ? all.filter((e) => e.scope === "global" && e.expires === "project_end")
        : [];
    let deleted = 0;
    for (const e of inScope) {
      try {
        if (store.delete(scope, e.name)) deleted++;
      } catch {
        /* skip */
      }
    }
    for (const e of expiring) {
      try {
        if (store.delete("global", e.name)) deleted++;
      } catch {
        /* skip */
      }
    }
    const extra = expiring.length > 0 ? ` (+${expiring.length} global expires=project_end)` : "";
    return { info: `${t("handlers.memory.cleared", { scope, count: deleted })}${extra}` };
  }

  const parts: string[] = [];
  const projMem = readProjectMemory(ctx.memoryRoot);
  if (projMem) {
    const label = basename(projMem.path);
    const hdr = projMem.truncated
      ? `▸ ${label}: ${projMem.path} (${projMem.originalChars.toLocaleString()} chars, truncated)`
      : `▸ ${label}: ${projMem.path} (${projMem.originalChars.toLocaleString()} chars)`;
    parts.push(hdr, "", projMem.content);
  }
  const globalIdx = store.loadIndex("global");
  if (globalIdx) {
    parts.push(
      "",
      `▸ global memory (${globalIdx.originalChars.toLocaleString()} chars${globalIdx.truncated ? ", truncated" : ""})`,
      "",
      globalIdx.content,
    );
  }
  const projectIdx = store.loadIndex("project");
  if (projectIdx) {
    parts.push(
      "",
      `▸ project memory (${projectIdx.originalChars.toLocaleString()} chars${projectIdx.truncated ? ", truncated" : ""})`,
      "",
      projectIdx.content,
    );
  }
  if (parts.length === 0) {
    return {
      info: [
        t("handlers.memory.noMemory", { root: ctx.memoryRoot }),
        "",
        t("handlers.memory.layers"),
        t("handlers.memory.layerProject", { file: PROJECT_MEMORY_FILE }),
        t("handlers.memory.layerGlobal"),
        t("handlers.memory.layerProjectHash"),
        "",
        t("handlers.memory.askModel"),
        t("handlers.memory.changesNote"),
        "",
        t("handlers.memory.subcommands"),
      ].join("\n"),
    };
  }
  parts.push("", t("handlers.memory.changesNoteShort"));
  return { info: parts.join("\n") };
};

export const handlers: Record<string, SlashHandler> = { memory };
