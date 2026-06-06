/** Diagnostic — print per-tool description + schema byte counts to plan compression targets. */

import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ToolRegistry } from "../src/tools.js";
import { registerChoiceTool } from "../src/tools/choice.js";
import { registerFilesystemTools } from "../src/tools/filesystem.js";
import { JobRegistry } from "../src/tools/jobs.js";
import { registerMemoryTools } from "../src/tools/memory.js";
import { registerPlanTool } from "../src/tools/plan.js";
import { registerScaffoldTools } from "../src/tools/scaffold.js";
import { registerShellTools } from "../src/tools/shell.js";
import { registerSkillTools } from "../src/tools/skills.js";
import { registerTodoTool } from "../src/tools/todo.js";
import { registerWebTools } from "../src/tools/web.js";

const root = mkdtempSync(join(tmpdir(), "reasonix-measure-"));
try {
  const tools = new ToolRegistry();
  const jobs = new JobRegistry();
  registerFilesystemTools(tools, { rootDir: root });
  registerShellTools(tools, { rootDir: root, jobs });
  registerMemoryTools(tools, { projectRoot: root });
  registerPlanTool(tools);
  registerChoiceTool(tools);
  registerTodoTool(tools);
  registerScaffoldTools(tools, { projectRoot: root });
  registerWebTools(tools);
  registerSkillTools(tools, { projectRoot: root, disableBuiltins: true });

  const specs = tools.specs();
  const rows = specs.map((s) => {
    const desc = (s.function?.description ?? "").length;
    const schema = JSON.stringify(s.function?.parameters ?? {}).length;
    return { name: s.function?.name ?? "?", desc, schema, total: desc + schema };
  });
  rows.sort((a, b) => b.total - a.total);

  console.log("=== Per-tool sizes (bytes) ===");
  console.log("name".padEnd(24), "desc".padStart(7), "schema".padStart(7), "total".padStart(7));
  for (const r of rows) {
    console.log(r.name.padEnd(24), String(r.desc).padStart(7), String(r.schema).padStart(7), String(r.total).padStart(7));
  }
  const sumDesc = rows.reduce((s, r) => s + r.desc, 0);
  const sumSchema = rows.reduce((s, r) => s + r.schema, 0);
  console.log(`\nTOTAL: ${rows.length} tools, ${sumDesc} desc + ${sumSchema} schema = ${sumDesc + sumSchema} bytes`);
} finally {
  rmSync(root, { recursive: true, force: true });
}
