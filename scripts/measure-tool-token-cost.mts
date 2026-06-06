import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { PauseGate } from "../src/core/pause-gate.js";
import { countTokens } from "../src/tokenizer.js";
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

interface Row {
  name: string;
  schemaTok: number;
  argsTok: number;
  outputTok: number;
  status: "ok" | "skipped" | "error";
  note?: string;
}

class AutoGate extends PauseGate {
  override ask(_opts: { kind: string; payload?: unknown }): Promise<any> {
    return Promise.resolve({ type: "run_once" });
  }
}

const root = mkdtempSync(join(tmpdir(), "reasonix-tokencost-"));
const home = mkdtempSync(join(tmpdir(), "reasonix-tokencost-home-"));

try {
  mkdirSync(join(root, "src"), { recursive: true });
  writeFileSync(join(root, "hello.txt"), "line 1\nline 2\nline 3\n");
  writeFileSync(join(root, "src", "a.ts"), "export const a = 1;\n");
  writeFileSync(join(root, "src", "b.ts"), "export const b = 2;\n");
  writeFileSync(join(root, "remove-me.txt"), "to be deleted\n");
  mkdirSync(join(root, "empty-dir"), { recursive: true });
  mkdirSync(join(root, "copy-source"), { recursive: true });
  writeFileSync(join(root, "copy-source", "x.txt"), "x\n");

  const tools = new ToolRegistry();
  const jobs = new JobRegistry();
  registerFilesystemTools(tools, { rootDir: root });
  registerShellTools(tools, { rootDir: root, jobs, allowAll: true });
  registerMemoryTools(tools, { projectRoot: root });
  registerPlanTool(tools);
  registerChoiceTool(tools);
  registerTodoTool(tools);
  registerScaffoldTools(tools, { homeDir: home, projectRoot: root, configPath: join(home, "config.json") });
  registerWebTools(tools);
  registerSkillTools(tools, { projectRoot: root, disableBuiltins: true });

  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async () =>
    new Response(
      `<a class="title" href="https://example.com/a">Example A</a><p class="s">short snippet</p>`,
      { status: 200, headers: { "Content-Type": "text/html" } },
    )) as unknown as typeof fetch;

  const gate = new AutoGate();
  const fixtures: Record<string, Record<string, unknown> | null> = {
    read_file: { path: "hello.txt" },
    write_file: { path: "written.txt", content: "hello world\n" },
    edit_file: { path: "hello.txt", old_string: "line 2", new_string: "LINE_TWO" },
    multi_edit: {
      path: "hello.txt",
      edits: [{ old_string: "line 1", new_string: "L1" }],
    },
    list_directory: { path: "." },
    directory_tree: { path: "." },
    search_files: { pattern: "*.ts" },
    search_content: { pattern: "export", path: "src" },
    glob: { pattern: "**/*.ts" },
    get_file_info: { path: "hello.txt" },
    create_directory: { path: "made-by-test" },
    delete_file: { path: "remove-me.txt" },
    delete_directory: { path: "empty-dir" },
    copy_file: { source: "copy-source/x.txt", destination: "copy-dest.txt" },
    move_file: { source: "copy-source/x.txt", destination: "moved-x.txt" },

    run_command: { command: "node --version" },
    run_background: { command: "node -e \"setTimeout(()=>{},200)\"", waitSec: 0.1 },
    list_jobs: {},
    job_output: null,
    wait_for_job: null,
    stop_job: null,

    remember: { type: "feedback", content: "use libs for unicode width" },
    recall_memory: { query: "unicode" },
    forget: null,

    todo_write: { todos: [{ id: "t1", title: "demo task", status: "pending" }] },

    create_skill: {
      name: "demo-skill",
      description: "test scaffold output cost",
      body: "# demo\n",
    },
    add_mcp_server: {
      name: "tokmcp",
      transport: "stdio",
      command: "node",
      args: ["server.js"],
    },

    web_search: { query: "anthropic claude" },
    web_fetch: { url: "https://example.com/" },

    submit_plan: null,
    mark_step_complete: null,
    revise_plan: null,
    ask_choice: null,
    run_skill: null,
    install_skill: null,
  };

  let postBgJobId: number | undefined;
  const rows: Row[] = [];

  for (const spec of tools.specs()) {
    const name = spec.function?.name ?? "?";
    const desc = spec.function?.description ?? "";
    const params = JSON.stringify(spec.function?.parameters ?? {});
    const schemaTok = countTokens(desc) + countTokens(params);

    let args = fixtures[name];
    if (args === undefined) {
      rows.push({ name, schemaTok, argsTok: 0, outputTok: 0, status: "skipped", note: "no fixture" });
      continue;
    }
    if (args === null && (name === "job_output" || name === "wait_for_job" || name === "stop_job")) {
      if (postBgJobId === undefined) {
        rows.push({ name, schemaTok, argsTok: 0, outputTok: 0, status: "skipped", note: "needs run_background first" });
        continue;
      }
      args = { jobId: postBgJobId };
      if (name === "wait_for_job") args.timeoutMs = 300;
    }
    if (args === null) {
      rows.push({ name, schemaTok, argsTok: 0, outputTok: 0, status: "skipped", note: "gate-bound / interactive" });
      continue;
    }

    const argsJson = JSON.stringify(args);
    const argsTok = countTokens(argsJson);
    try {
      const out = await tools.dispatch(name, argsJson, { confirmationGate: gate });
      const outputTok = countTokens(out);
      rows.push({ name, schemaTok, argsTok, outputTok, status: "ok" });
      if (name === "run_background") {
        const m = out.match(/job (\d+) /);
        if (m) postBgJobId = Number(m[1]);
      }
    } catch (err) {
      const msg = (err as Error).message;
      rows.push({ name, schemaTok, argsTok, outputTok: 0, status: "error", note: msg.slice(0, 60) });
    }
  }

  rows.sort((a, b) => b.schemaTok + b.argsTok + b.outputTok - (a.schemaTok + a.argsTok + a.outputTok));

  const pad = (s: string | number, w: number, right = true) =>
    right ? String(s).padStart(w) : String(s).padEnd(w);

  console.log("=== Per-tool token cost (DeepSeek V4 BPE) ===");
  console.log(
    pad("name", 22, false),
    pad("schema", 8),
    pad("args", 6),
    pad("output", 7),
    pad("total", 7),
    "  status",
  );
  console.log("─".repeat(70));
  let sumSchema = 0;
  let sumArgs = 0;
  let sumOutput = 0;
  for (const r of rows) {
    const total = r.schemaTok + r.argsTok + r.outputTok;
    sumSchema += r.schemaTok;
    sumArgs += r.argsTok;
    sumOutput += r.outputTok;
    const tag = r.status === "ok" ? "ok " : r.status === "skipped" ? "—  " : "ERR";
    const note = r.note ? `  (${r.note})` : "";
    console.log(
      pad(r.name, 22, false),
      pad(r.schemaTok, 8),
      pad(r.argsTok, 6),
      pad(r.outputTok, 7),
      pad(total, 7),
      `  ${tag}${note}`,
    );
  }
  console.log("─".repeat(70));
  console.log(
    pad(`TOTAL (${rows.length} tools)`, 22, false),
    pad(sumSchema, 8),
    pad(sumArgs, 6),
    pad(sumOutput, 7),
    pad(sumSchema + sumArgs + sumOutput, 7),
  );

  console.log("\nNote: schema = fixed per-turn cost while tool is registered.");
  console.log("      args + output = one specific dispatch (representative input, not worst case).");
  console.log("      skipped: gate-bound (plan/choice/skills) or needs prior call.");

  globalThis.fetch = originalFetch;
  await jobs.shutdown(2000);
} finally {
  rmSync(root, { recursive: true, force: true });
  rmSync(home, { recursive: true, force: true });
}

process.exit(0);
