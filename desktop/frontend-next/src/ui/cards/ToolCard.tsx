import { useEffect, useRef, useState } from "react";
import { t } from "../../i18n";
import { nestLabel } from "../delegation";
import type { ExtensionSurface, Tool } from "../../port/wire";
import type { RewindPlan, RewindResult } from "../../port/port";
import { categoryOf, labelFor, mcpOrigin, runLabelFor } from "../icons";
import { Sym, glyphFor } from "../Sym";
import { GOAL_STATUS, argOf, goalUpdate, shortArgs } from "../args";
import { Cost } from "../Cost";
import { seconds, tokens } from "../../i18n/format";
import { parsePlan } from "../../state/session";
import { DiffView } from "./DiffView";
import { Term, ToolOutput } from "./ToolOutput";
import { ExtensionView } from "./ExtensionView";
import { toolFailed, toolFailureLabel } from "./outcome";

// The spec pops a symbol as it settles — colour arriving is the finish signal.
// Only the transition may fire it: a restored transcript is all settled cards,
// and marking them done would pop the whole page on load.
// The head crossfades before it swaps: "「正在读取…」→「读了 7 个文件」是这一步
// 唯一的状态跃迁，别用 0ms 换掉它". 90ms out, then the settled text.
const SWAP = 90;

function useSettling(running: boolean): { pop: boolean; swap: boolean } {
  const [pop, setPop] = useState(false);
  const [swap, setSwap] = useState(false);
  const was = useRef(running);
  useEffect(() => {
    const fell = was.current && !running;
    was.current = running;
    if (!fell) return;
    setPop(true);
    setSwap(true);
    const a = setTimeout(() => setSwap(false), SWAP);
    const b = setTimeout(() => setPop(false), 340);
    return () => {
      clearTimeout(a);
      clearTimeout(b);
    };
  }, [running]);
  return { pop, swap };
}

export function ToolCard({
  tool, running, children = [], takeover, onExtInvoke, onPrepareFileRevert, onCommitFileRevert,
}: {
  tool: Tool;
  running: boolean;
  children?: Tool[];
  // Reverting the file this call wrote. Optional: a card drawn outside a live
  // pane (history, a fixture) has nothing to revert against.
  onPrepareFileRevert?: (path: string) => Promise<RewindPlan>;
  onCommitFileRevert?: (planId: string, resolution?: string) => Promise<RewindResult>;
  // A view an extension published against this call. It replaces the body only.
  takeover?: ExtensionSurface;
  onExtInvoke?: (actionId: string) => void;
}) {
  // use_capability is the proxy most tools are reached through — the provider
  // sees thirteen names and everything else routes via it — so it says nothing
  // about who answered. Name the resolved tool; only a target that really is an
  // MCP tool reads MCP, which labelFor decides from the mcp__ prefix.
  const shown = tool.resolvedName || tool.name;
  // Running and settled are two different lines: the category says what it is
  // doing now, the label says what it was once it is done.
  const head = running ? runLabelFor(shown) : labelFor(shown);
  // A write's payload streams as arguments, and the provider only reports how
  // many characters have landed — the JSON is unparseable until the last one.
  // Showing that count is the difference between a card that is visibly filling
  // and one that sits blank until the whole file arrives at once.
  const streaming = running && !tool.args && (tool.argChars ?? 0) > 0;
  const arg = tool.name === "todo_write" ? "" : streaming ? `${tokens(tool.argChars!)} 字符` : shortArgs(tool.args ?? "");
  // A shell result carries its exit status separately from stdout, and stdout
  // alone cannot say whether the command worked.
  const bad = toolFailed(tool);
  // The number is the actionable half; the state only says that something went
  // wrong, which the colour already says.
  const badLabel = toolFailureLabel(tool);
  // Which server answered belongs on the card, not in a panel: this is the
  // moment the user can judge whether an external service should have run.
  const from = mcpOrigin(shown);
  // The interpreter that actually ran it, or the remote tool a capability call
  // resolved to. Anything else the name already said.
  const tag = tagFor(tool);
  // A delegated step is only auditable if it names the profile that ran: "a
  // subagent" is not a thing you can go read, "skill:security-review" is.
  const who = tool.profile?.name?.trim();
  const settling = useSettling(running);
  // A refused call carries the same sentence twice: the kernel writes it to the
  // model as output and to the reader as an error. Rendering both prints it
  // twice, and the second copy reads like a second failure.
  const echoed = !!tool.err && tool.err.trim() === (tool.output ?? "").trim();
  // update_goal's payload is the model's own prose about the turn. Its shape is
  // one claim — done, still going, or stuck — so the card says the claim rather
  // than the object carrying it.
  const goal = tool.name === "update_goal" ? goalUpdate(tool.args) : null;
  return (
    // data-call is the anchor the run graph lands on. The rail could already
    // find a user message; a tool call had no id in the DOM at all, so nothing
    // outside the transcript could point at one.
    <div
      className="call"
      data-call={tool.id || undefined}
      data-k={KINDED.has(categoryOf(shown)) ? categoryOf(shown) : undefined}
      data-running={running ? "" : undefined}
    >
      <div className="g">
        <Sym glyph={glyphFor(shown)} done={settling.pop && !bad} />
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl" data-swap={settling.swap ? "" : undefined}>
          {/* 名字是给人读的，id 是给人查的。标签只在它说了名字没说的事时才占地方
              （见 tagFor），所以精确的那个字符串挂在这里，一直够得着。 */}
          <span className={running ? "nm shim" : "nm"} title={shown}>{head}</span>
          {who && <span className="who" title={t("按技能 {name} 的设定运行的子代理", { name: who })}>{who}</span>}
          {from && <span className="src" title={t("外部服务 {name} 提供的工具", { name: from.server })}>{from.server}</span>}
          {tag && <span className="tag" title={tagHint(tool)}>{tag}</span>}
          {arg && <span className={streaming ? "arg shim" : "arg"}>{arg}</span>}
          {bad && <span className="fail">{badLabel}</span>}
          <Cost tools={[tool]} running={running} />
        </div>
        <div className="out">
          {/* A takeover replaces what the body shows, never the frame around
              it: the tool's name, its status and the attribution below stay
              the host's, so a redrawn card is still recognisably this call and
              still visibly the extension's work. */}
          {takeover ? (
            <>
              <ExtensionView body={takeover.view?.body ?? []} onAction={(id) => onExtInvoke?.(id)} />
              <div className="drawnby">{t("由 {name} 渲染", { name: takeover.pluginId })}</div>
            </>
          ) : (
            <>
          {tool.diff && (
            <DiffView
              diff={tool.diff}
              path={argOf(tool.args, "path", "file_path")}
              onPrepare={onPrepareFileRevert}
              onCommit={onCommitFileRevert}
            />
          )}
          {!tool.diff && tool.name === "todo_write" && <Steps tool={tool} />}
          {goal && (
            <div className="goalup" data-s={GOAL_STATUS[goal.status]?.[1] ?? "run"}>
              <span className="st">{t(GOAL_STATUS[goal.status]?.[0] ?? goal.status)}</span>
              {goal.reason && <span className="rs">{goal.reason}</span>}
            </div>
          )}
          {!tool.diff && !goal && tool.name !== "todo_write" && tool.output && !echoed && children.length === 0 && (
            <ToolOutput name={shown} text={tool.output} bound={tool.bound} id={tool.id} />
          )}
            </>
          )}
          {/* The error stays outside the takeover: an extension may redraw what
              a call produced, never whether it failed. */}
          {tool.err && <div className="txt bad">{tool.err}</div>}
          {children.length > 0 && (
            <div className="nest">
              <div className="nest-hd">
                <i className="pip" />
                <span className="who">{nestLabel(who, tool.profile?.count, children.length)}</span>
                <span className="prof">{t("独立上下文 · 不进主轨迹")}</span>
                {/* 委派出去那部分的账：耗时是父调用的，token 是子步骤各自留下的 */}
                <span className="rt">
                  {[
                    tool.durationMs ? seconds(tool.durationMs) : "",
                    childTokens(children) ? tokens(childTokens(children)) : "",
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </span>
              </div>
              <div className="nest-bd">
                {children.map((c) => (
                  <NestedCall key={c.id} tool={c} />
                ))}
              </div>
              {/* What the delegate handed back closes its own panel: outside it,
                  the sentence reads as the main run's conclusion. */}
              {tool.output && <div className="nest-ret">{tool.output}</div>}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function NestedCall({ tool }: { tool: Tool }) {
  const shown = tool.resolvedName || tool.name;
  const tag = tagFor(tool);
  const bad = toolFailed(tool);
  const clipped = (tool.output?.length ?? 0) > 400;
  return (
    <div className="call" data-call={tool.id || undefined} data-k={KINDED.has(categoryOf(shown)) ? categoryOf(shown) : undefined}>
      <div className="g">
        <Sym glyph={glyphFor(shown)} />
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm" title={shown}>{labelFor(shown)}</span>
          {tag && <span className="tag" title={tagHint(tool)}>{tag}</span>}
          {tool.args && <span className="arg">{shortArgs(tool.args)}</span>}
          {bad && <span className="fail">{toolFailureLabel(tool)}</span>}
          <Cost tools={[tool]} />
        </div>
        {tool.output && (
          <div className="out">
            <Term text={tool.output.slice(0, 400)} />
            {clipped && <div className="bound">{t("仅显示前 400 个字符")}</div>}
          </div>
        )}
        {tool.err && <div className="out"><div className="txt bad">{tool.err}</div></div>}
      </div>
    </div>
  );
}

// The plan is the one payload the flow shows twice on purpose: the rail tracks
// it for the rest of the turn, the card records what it was when it was written.
function Steps({ tool }: { tool: Tool }) {
  const steps = parsePlan(tool);
  if (!steps?.length) return <span className="fold">{t("计划已移入右栏")}</span>;
  const now = steps.findIndex((s) => !s.done);
  return (
    <div className="steps">
      {steps.map((st, i) => (
        <div className="s" key={i} data-done={st.done ? "" : undefined} data-now={i === now ? "" : undefined}>
          <span className="b">{st.done ? "✓" : i + 1}</span>
          <span className="t">
            <span className="ln">{st.text}</span>
          </span>
        </div>
      ))}
    </div>
  );
}

const childTokens = (kids: Tool[]) => kids.reduce((n, k) => n + (k.contextTokens ?? 0), 0);


// An identifier reads correctly in the mono tag and nowhere else, so this is the
// one place a raw tool id is allowed to surface. A shell call spends it on the
// interpreter instead: the name above already says "Bash", and on a host without
// one the command was actually handed to PowerShell — which is the difference
// between a command that works and the same text failing on '&&'.
// Nothing at all when the id is the very string the name above was derived
// from: "Read" beside read_file is one fact printed twice, and it was on every
// card in the record.
const tagFor = (tool: Tool): string | null => {
  const ex = tool.execution;
  if (ex?.kind === "shell" && ex.shell) return ex.shell;
  return mcpOrigin(tool.resolvedName || tool.name)?.tool ?? null;
};

const SHELL_HINT: Record<string, string> = {
  bash: "命令交给 bash 执行",
  "git-bash": "命令交给 Git Bash 执行",
  pwsh: "命令交给 PowerShell 7 执行 —— 语法是 PowerShell，不是 bash",
  powershell: "本机没有 bash，命令交由 Windows PowerShell 执行 —— 它不支持 && 和 ||",
};

const tagHint = (tool: Tool) => {
  const ex = tool.execution;
  return ex?.kind === "shell" && ex.shell ? SHELL_HINT[ex.shell] : undefined;
};

// Only the categories the spec gives a colour to; the rest stay neutral.
const KINDED = new Set(["net", "deleg", "write", "mcp", "mem"]);
