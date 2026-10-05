import { useState } from "react";
import { useStartsOpen } from "../../state/foldpref";
import type { Tool } from "../../port/wire";
import { argOf, splitPath } from "../args";
import { Sym, glyphFor } from "../Sym";
import { Cost } from "../Cost";
import { Hits, Peek, Term, hitRows, parseHits, parseListing, parseRows, PATH } from "./ToolOutput";
import { toolFailed } from "./outcome";
import { t as tr } from "../../i18n";

// read_file numbers every line it returns, so the count is in the output rather
// than guessed from it.
const NUMBERED = /^\s*\d+→/;
const countLines = (out?: string) => (out ? out.split("\n").filter((l) => NUMBERED.test(l)).length : 0);
const countRows = (out?: string) => (out ? out.split("\n").filter((l) => l.trim()).length : 0);
const countHits = (out?: string) => (out ? (parseHits(out)?.hits.length ?? countRows(out)) : 0);

interface Row {
  id: string;
  name: string;
  dir: string;
  n: string;
  failed: boolean;
}

// What each call contributes to the manifest: the left column is what it
// touched, the right is the operation and its count — "Read · 418 行" says both
// what happened to the file and how much of it there was.
function rowOf(t: Tool): Row {
  const arg = argOf(t.args, "path", "file_path", "pattern", "query") || t.args || "—";
  const state = { id: t.id ?? "", failed: toolFailed(t) };
  switch (t.name) {
    case "read_file": {
      const [dir, name] = splitPath(arg);
      return { ...state, dir, name, n: state.failed ? tr("失败") : tr("读取 · {n} 行", { n: countLines(t.output) }) };
    }
    case "grep": {
      const where = argOf(t.args, "path", "file_path");
      return { ...state, dir: "", name: `${tr("搜索")} “${arg}”${where ? ` ${where}` : ""}`, n: state.failed ? tr("失败") : tr("{n} 处", { n: countHits(t.output) }) };
    }
    case "glob":
      return { ...state, dir: "", name: `${tr("查找文件")} ${arg}`, n: state.failed ? tr("失败") : tr("{n} 个", { n: countRows(t.output) }) };
    default:
      return { ...state, dir: "", name: `${tr("列出目录")} ${arg}`, n: state.failed ? tr("失败") : tr("{n} 项", { n: countRows(t.output) }) };
  }
}

// The fold summary is what the run was, by tool: a manifest of thirty rows says
// nothing at a glance that "read_file ×4 · grep 命中 14 处 · glob ×3" does not.
function summarise(tools: Tool[]): string {
  const reads = tools.filter((t) => t.name === "read_file").length;
  const greps = tools.filter((t) => t.name === "grep");
  const globs = tools.filter((t) => t.name === "glob").length;
  const lists = tools.filter((t) => t.name === "ls").length;
  const hits = greps.reduce((n, t) => n + countHits(t.output), 0);
  return [
    reads && tr("读取文件 ×{n}", { n: reads }),
    greps.length && tr("搜索命中 {n} 处", { n: hits }),
    globs && tr("查找文件 ×{n}", { n: globs }),
    lists && tr("列出目录 ×{n}", { n: lists }),
  ]
    .filter(Boolean)
    .join(" · ");
}

// A picked row opens the call's own result in the shape that result has: a file
// is a terminal block, a search is its excerpts, a listing is a manifest.
function Body({ tool }: { tool: Tool }) {
  const out = tool.output;
  const body = (() => {
    if (!out) return null;
    if (tool.name === "grep") {
      const found = parseHits(out);
      if (found) return <Hits rows={hitRows(found.hits)} note={found.note} />;
    }
    if (tool.name === "glob") {
      const rows = parseRows(out, PATH);
      if (rows) return <Peek rows={rows} unit="" />;
    }
    if (tool.name === "ls") {
      const rows = parseListing(out);
      if (rows) return <Peek rows={rows} unit="B" />;
    }
    return <Term text={out} />;
  })();
  return (
    <>
      {body}
      {tool.err && <div className="txt bad">{tool.err}</div>}
      {tool.bound?.kind === "truncated" && <div className="bound bad">{tr("输出不完整，部分内容未进入上下文")}</div>}
    </>
  );
}

// A run of lookups is one step, not one card each. The manifest answers what was
// touched; the result itself is one click away rather than dumped into the flow.
export function ReadsCard({ tools }: { tools: Tool[] }) {
  const [open, setOpen] = useState(-1);
  const failed = tools.filter(toolFailed).length;
  const start = useStartsOpen("steps", false, failed > 0);
  const [touched, setExpanded] = useState<boolean | null>(null);
  const expanded = touched ?? start;
  const rows = tools.map(rowOf);
  const files = tools.filter((t) => t.name === "read_file").length;
  const searches = tools.filter((t) => t.name === "grep" || t.name === "glob").length;
  const headline = files === tools.length
    ? tr("读取了 {n} 个文件", { n: files })
    : searches === tools.length
      ? tr("完成了 {n} 次搜索", { n: searches })
      : tr("查阅了 {n} 项内容", { n: tools.length });
  return (
    <div className="call" data-state={failed ? "failed" : "done"}>
      <div className="g">
        <Sym glyph={glyphFor("read_file")} />
        <span className="line" />
      </div>
      <div className="c">
        <details className="tool-disclosure reads-disclosure" open={expanded} onToggle={(event) => event.currentTarget.open !== expanded && setExpanded(event.currentTarget.open)}>
          <summary className="hl">
            <span className="nm">{headline}</span>
            <span className="arg">{summarise(tools)}</span>
            {failed > 0 && <span className="fail">{tr("{n} 项失败", { n: failed })}</span>}
            <Cost tools={tools} />
            <span
              className="tool-state"
              data-state={failed ? "failed" : "done"}
              aria-label={tr(failed ? "失败" : "已完成")}
              title={tr(failed ? "失败" : "已完成")}
            />
            <span className="tool-fold" aria-hidden="true" />
          </summary>
          <div className="out">
            <div className="peek">
              {rows.map((r, i) => (
                <button
                  className="row"
                  key={r.id || `${r.name}#${i}`}
                  data-call={r.id || undefined}
                  data-bad={r.failed ? "" : undefined}
                  data-on={open === i ? "" : undefined}
                  onClick={() => setOpen(i === open ? -1 : i)}
                >
                  <span>
                    {r.dir && <span className="d">{r.dir}</span>}
                    {r.name}
                  </span>
                  <span className="n">{r.n}</span>
                </button>
              ))}
            </div>
            {open >= 0 && tools[open] && <Body tool={tools[open]} />}
          </div>
        </details>
      </div>
    </div>
  );
}
