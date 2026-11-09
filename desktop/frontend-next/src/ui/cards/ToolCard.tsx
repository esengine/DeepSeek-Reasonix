import { JsonText } from "./JsonText";
import { useEffect, useRef, useState } from "react";
import { useStartsOpen } from "../../state/foldpref";
import { t } from "../../i18n";
import { nestLabel, ranLabel } from "../delegation";
import type { ExtensionSurface, Tool } from "../../port/wire";
import type { RewindPlan, RewindResult } from "../../port/port";
import { categoryOf, labelFor, mcpOrigin, runLabelFor } from "../icons";
import { Sym, iconFor } from "../Sym";
import { activityVerb } from "../activityEarlier";
import { GOAL_STATUS, argOf, goalUpdate, shortArgs } from "../args";
import { Cost } from "../Cost";
import { bytes, count, seconds, tokens } from "../../i18n/format";
import { currentStep, parsePlan, stepDone } from "../../state/session";
import { DiffView } from "./DiffView";
import { BringCall } from "./BringCall";
import { StepWait, StopCall } from "./StopCall";
import { stepSummary } from "./stepsum";
import { BackgroundNote, BackgroundReading } from "./BackgroundJob";
import { WaitNote } from "./WaitNote";
import { Term, ToolOutput, BoundNote } from "./ToolOutput";
import { ExtensionView } from "./ExtensionView";
import { toolChangedFile, toolErrorDetail, toolFailed, toolFailureLabel, toolOutputEchoed, toolRefusalReason } from "./outcome";
import { StudioIcon } from "../StudioIcon";
import { registerNest } from "./nestreveal";
import { useFullOutput, type ReadOutput } from "./useFullOutput";
import type { StateTool } from "../../state/outputcap";
import { ImagePreview } from "../ImagePreview";

// The spec pops a symbol as it settles — colour arriving is the finish signal.
// Only the transition may fire it: a restored transcript is all settled cards,
// and marking them done would pop the whole page on load.
// The head crossfades before it swaps: "「正在读取…」→「读了 7 个文件」是这一步
// 唯一的状态跃迁，别用 0ms 换掉它". 90ms out, then the settled text.
const SWAP = 90;

function prettyArgs(args?: string): string {
  if (!args) return "";
  try {
    return JSON.stringify(JSON.parse(args), null, 2);
  } catch {
    return args;
  }
}

const shortCallId = (id?: string) => id ? id.replace(/^call_/, "").slice(-8) : "—";

const lastLine = (text?: string) => text?.split("\n").map((l) => l.trim()).filter(Boolean).at(-1) ?? "";

// How long an activity step has run or took, in whole seconds; nothing under one.
// A running step counts from when the kernel says it started, so a row drawn
// late does not restart the clock; a call with no start counts from its row.
function StepTime({ tool, running }: { tool: StateTool; running: boolean }) {
  const drawnAt = useRef(Date.now()).current;
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!running) return;
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [running]);
  const ms = running ? Math.max(0, now - (tool.startedAt || drawnAt)) : tool.durationMs ?? 0;
  if (ms < 1000) return null;
  return <span className="step-sum">{t("common.n_s", { n: Math.round(ms / 1000) })}</span>;
}

// A step that turns failed under the reader shakes once; one drawn failed does not.
function useFellInto(bad: boolean): boolean {
  const [on, setOn] = useState(false);
  const was = useRef(bad);
  useEffect(() => {
    const fell = !was.current && bad;
    was.current = bad;
    if (!fell) return;
    setOn(true);
    const id = setTimeout(() => setOn(false), 240);
    return () => clearTimeout(id);
  }, [bad]);
  return on;
}

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
  tool, running, children = [], readOutput, omitted = 0, takeover, onExtInvoke, onPrepareFileRevert, onCommitFileRevert, onStop, onBring, pinned = false, activity = false,
}: {
  tool: StateTool;
  running: boolean;
  activity?: boolean;
  children?: StateTool[];
  // Reads a call's whole output back from the kernel when the page kept only its ends.
  readOutput?: ReadOutput;
  // Characters of a running call's output the page let go of from the front.
  omitted?: number;
  // Reverting the file this call wrote. Optional: a card drawn outside a live
  // pane (history, a fixture) has nothing to revert against.
  onPrepareFileRevert?: (path: string) => Promise<RewindPlan>;
  onCommitFileRevert?: (planId: string, resolution?: string) => Promise<RewindResult>;
  // A view an extension published against this call. It replaces the body only.
  // Ends this call alone and leaves the turn running. Absent where no live
  // session stands behind the card.
  onStop?: () => void;
  // Shows this call's result again at the end of the transcript. Absent on the
  // copy that is already there, and where no transcript stands behind the card.
  onBring?: () => void;
  // The copy brought to the end: drawn open, because it was asked for.
  pinned?: boolean;
  takeover?: ExtensionSurface;
  onExtInvoke?: (actionId: string) => void;
}) {
  // use_capability is the proxy most tools are reached through — the provider
  // sees thirteen names and everything else routes via it — so it says nothing
  // about who answered. Name the resolved tool; only a target that really is an
  // MCP tool reads MCP, which labelFor decides from the mcp__ prefix.
  const shown = tool.resolvedName || tool.name;
  const from = mcpOrigin(shown);
  // Running and settled are two different lines: the category says what it is
  // doing now, the label says what it was once it is done.
  const verb = activityVerb(shown);
  const bgJob = tool.execution?.state === "background_started" ? tool.execution.jobId : undefined;
  const head = activity && bgJob ? t("chat.sent_to_background") : activity ? (from?.tool || (verb ? t(verb) : labelFor(shown))) : running ? runLabelFor(shown) : labelFor(shown);
  // A write's payload streams as arguments, and the provider only reports how
  // many characters have landed — the JSON is unparseable until the last one.
  // Showing that count is the difference between a card that is visibly filling
  // and one that sits blank until the whole file arrives at once.
  const streaming = running && !tool.args && (tool.argChars ?? 0) > 0;
  const arg = tool.name === "todo_write" ? "" : streaming ? `${tokens(tool.argChars!)} 字符` : shortArgs(tool.args ?? "");
  const bad = toolFailed(tool);
  // The number is the actionable half; the state only says that something went
  // wrong, which the colour already says.
  const badLabel = toolFailureLabel(tool);
  const refusal = toolRefusalReason(tool);
  const errorDetail = toolErrorDetail(tool);
  // Which server answered belongs on the card, not in a panel: this is the
  // moment the user can judge whether an external service should have run.
  // The interpreter that actually ran it, or the remote tool a capability call
  // resolved to. Anything else the name already said.
  const tag = tagFor(tool);
  // A delegated step is only auditable if it names the profile that ran: "a
  // subagent" is not a thing you can go read, "skill:security-review" is.
  const who = tool.profile?.name?.trim();
  const settling = useSettling(running);
  const shaking = useFellInto(bad);
  // A refused call carries the same sentence twice: the kernel writes it to the
  // model as output and to the reader as an error. Rendering both prints it
  // twice, and the second copy reads like a second failure.
  const echoed = toolOutputEchoed(tool);
  const full = useFullOutput(tool, readOutput);
  // update_goal's payload is the model's own prose about the turn. Its shape is
  // one claim — done, still going, or stuck — so the card says the claim rather
  // than the object carrying it.
  const goal = tool.name === "update_goal" ? goalUpdate(tool.args) : null;
  // Running is not a reason to show the arguments: the group above already says
  // the work is happening, and a call that opens itself pushes the transcript
  // around while it reads. A failure is different — that is what is being asked.
  // In the activity group a failure says itself in the box under its row, so
  // the call's whole body stays folded until the reader asks for it.
  const start = useStartsOpen("steps", false, bad && !activity, toolChangedFile(tool));
  const [touched, setOpen] = useState<boolean | null>(pinned ? true : null);
  useEffect(() => {
    if (bad) setOpen(null);
  }, [bad]);
  const open = touched ?? start;
  // The body of a finished, closed card is most of what a long session holds in
  // the page, so such a card does not mount it; once opened it stays, so what the
  // reader did inside (show all) survives closing it again.
  const opened = useRef(false);
  useEffect(() => {
    if (open) opened.current = true;
  }, [open]);
  const nestMounted = open || running || opened.current;
  useEffect(() => {
    if (nestMounted || children.length === 0) return;
    return registerNest(children.map((c) => c.id).filter((id): id is string => !!id), () => setOpen(true));
  }, [nestMounted, children]);
  const changes = changeCounts(tool);
  const sum = activity && !running ? stepSummary(tool, verb, bad) : "";
  // A host refusal says why in its own words; a command's last line of output says
  // more than the generic error that it exited; any other tool's output is not about its failure.
  const failLine = activity && bad ? (refusal || (tool.refusalCode && errorDetail) || (tool.execution ? lastLine(tool.output) : "") || errorDetail || tool.err || lastLine(tool.output) || "") : "";
  const activityArgs = activity ? prettyArgs(tool.args) : "";
  // A call being watched in the activity group opens to its input even with no
  // output yet: a row that cannot open is the one that looks stuck.
  const hasBody = Boolean(
    takeover || tool.diff || goal || tool.argumentNormalization || tool.name === "todo_write" ||
    (tool.output && !echoed && children.length === 0) || tool.err || children.length || (activity && running && activityArgs),
  );
  const activityResult = Boolean(
    takeover || tool.diff || goal || tool.name === "todo_write" || tool.output || tool.err || children.length,
  );
  const heading = (
    <>
      {/* 名字是给人读的，id 是给人查的。标签只在它说了名字没说的事时才占地方
          （见 tagFor），所以精确的那个字符串挂在这里，一直够得着。 */}
      {activity && <span className="kind" aria-hidden="true"><Sym icon={iconFor(shown)} /></span>}
      <span className={running ? "nm shim" : "nm"} title={shown}>{head}</span>
      {who && <span className="who" title={t("chat.a_subagent_running_under_the", { name: who })}>{who}</span>}
      {from && <span className="src" title={t("chat.a_tool_provided_by_the", { name: from.server })}>{from.server}</span>}
      {tag && <span className="tag" title={tagHint(tool)}>{tag}</span>}
      {arg && <span className={streaming ? "arg shim" : "arg"} title={streaming ? undefined : prettyArgs(tool.args)}>{arg}</span>}
      {changes && (
        <span className="tool-delta" aria-label={t("chat.added_lines_added_removed_removed", changes)}>
          <b>+{changes.added}</b><i>−{changes.removed}</i>
        </span>
      )}
      {bad && !sum && <span className="fail">{badLabel}</span>}
      {bgJob && <BackgroundReading id={bgJob} />}
      {sum ? <span className="step-sum">{sum}</span> : activity ? !changes && <StepTime tool={tool} running={running} /> : <Cost tools={[tool]} running={running} />}
      <span
        className="tool-state"
        data-state={running ? "running" : bad ? "failed" : "done"}
        data-pop={settling.pop && !bad ? "" : undefined}
        aria-label={t(running ? "common.running" : bad ? "common.failed" : "common.done_2")}
        title={t(running ? "common.running" : bad ? "common.failed" : "common.done_2")}
      >
        {running && activity ? <i className="step-spin" /> : <StudioIcon name={running ? "clock" : bad ? (activity ? "fail" : "warning") : "check"} />}
      </span>
      {onBring && tool.id && !running && hasBody ? <BringCall onBring={onBring} /> : null}
      {hasBody && <span className="tool-fold" aria-hidden="true" />}
    </>
  );
  const body = (
    <div className="out">
      {tool.argumentNormalization && (
        <section className="activity-call-input">
          <p>{t("chat.argument_format_automatically_normalized")}</p>
          <details>
            <summary>{t("chat.original_input_before_normalization")}</summary>
            <pre>{prettyArgs(tool.argumentNormalization.original)}</pre>
          </details>
        </section>
      )}
      {activity && (
        <>
          <div className="activity-call-meta">
            <span>{t("chat.call_id", { id: shortCallId(tool.id) })}</span>
            <span>{t(running ? "common.running" : bad ? "common.failed" : "common.done_2")}</span>
          </div>
          {activityArgs && (
            <section className="activity-call-input">
              <header><b>{t("common.input")}</b><span>JSON</span></header>
              <pre><JsonText text={activityArgs} /></pre>
            </section>
          )}
          {running && !tool.output && !tool.err && children.length === 0 && <WaitNote args={tool.args} />}
          {activityResult && (
            <div className="activity-call-result">
              <b>{t("chat.result")}</b>
              {tool.output && (
                <button
                  type="button"
                  data-action="tool.copy-output"
                  onClick={() => void navigator.clipboard.writeText(tool.output ?? "").catch(() => {})}
                >{t("common.copy")}</button>
              )}
            </div>
          )}
        </>
      )}
      {/* A takeover replaces what the body shows, never the frame around
          it: the tool's name, its status and the attribution below stay
          the host's, so a redrawn card is still recognisably this call and
          still visibly the extension's work. */}
      {takeover ? (
        <>
          <ExtensionView body={takeover.view?.body ?? []} onAction={(id) => onExtInvoke?.(id)} />
          <div className="drawnby">{t("chat.drawn_by_name", { name: takeover.pluginId })}</div>
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
          {!tool.diff && tool.outputDiff && tool.output && (
            <>
              <DiffView diff={tool.output} />
              <BoundNote bound={tool.bound} />
            </>
          )}
          {!tool.diff && !tool.outputDiff && tool.name === "todo_write" && <Steps tool={tool} />}
          {goal && (
            <div className="goalup" data-s={GOAL_STATUS[goal.status]?.[1] ?? "run"}>
              <span className="st">{t(GOAL_STATUS[goal.status]?.[0] ?? goal.status)}</span>
              {goal.reason && <span className="rs">{goal.reason}</span>}
            </div>
          )}
          {!tool.diff && !tool.outputDiff && !goal && tool.name !== "todo_write" && tool.output && !echoed && children.length === 0 && (
            <>
              {omitted > 0 && <div className="bound">{t("chat.n_characters_before_this_are", { n: count(omitted) })}</div>}
              {full.note}
              <ToolOutput name={shown} text={full.text ?? ""} bound={tool.bound} id={tool.id} />
            </>
          )}
          <ToolShots images={tool.images} />
          <ToolVideos videos={tool.videos} />
        </>
      )}
      {/* The error stays outside the takeover: an extension may redraw what
          a call produced, never whether it failed. */}
      {refusal && <div className="txt" data-refusal={tool.refusalCode}>{refusal}</div>}
      {errorDetail && <div className="txt bad">{errorDetail}</div>}
      {children.length > 0 && (
        <div className="nest">
          <div className="nest-hd">
            <i className="pip" />
            <span className="who">{nestLabel(who, tool.profile?.count, children.length)}</span>
            <span className="prof">{t("chat.its_own_context_stays_out")}</span>
            {ranLabel(tool.profile) && <span className="ran" title={ranLabel(tool.profile)}>{ranLabel(tool.profile)}</span>}
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
          {nestMounted && (
            <>
              <div className="nest-bd">
                {children.map((c) => (
                  <NestedCall key={c.id} tool={c} read={readOutput} />
                ))}
              </div>
              {/* What the delegate handed back closes its own panel: outside it,
                  the sentence reads as the main run's conclusion. */}
              {tool.output && <div className="nest-ret">{tool.output}</div>}
            </>
          )}
        </div>
      )}
    </div>
  );
  return (
    // data-call is the anchor the run graph lands on. The rail could already
    // find a user message; a tool call had no id in the DOM at all, so nothing
    // outside the transcript could point at one.
    <div
      className="call"
      data-call={tool.id || undefined}
      data-k={KINDED.has(categoryOf(shown)) ? categoryOf(shown) : undefined}
      data-running={running ? "" : undefined}
      data-shake={shaking ? "" : undefined}
    >
      <div className="g">
        <Sym icon={iconFor(shown)} done={settling.pop && !bad} />
      </div>
      <div className="c">
        {hasBody ? (
          <details className="tool-disclosure" data-lazy={nestMounted ? undefined : ""} open={open} onToggle={(event) => event.currentTarget.open !== open && setOpen(event.currentTarget.open)}>
            <summary className="hl" data-swap={settling.swap ? "" : undefined}>{heading}</summary>
            {nestMounted ? body : null}
          </details>
        ) : (
          <div className="hl" data-swap={settling.swap ? "" : undefined}>{heading}</div>
        )}
        {failLine && (
          <div className="step-err" role="status">
            <code>{failLine}</code>
            {hasBody && !open && <button type="button" className="step-stop" data-action="tool.full-output" onClick={() => setOpen(true)}>{t("chat.see_full_output")}</button>}
          </div>
        )}
        {running && activity && <StepWait onStop={onStop && tool.id ? onStop : undefined} quiet={!!tool.output || children.length > 0} />}
        {running && !activity && onStop && tool.id && <StopCall onStop={onStop} />}
        {bgJob && <BackgroundNote id={bgJob} />}
      </div>
    </div>
  );
}

function ToolVideos({ videos }: { videos?: NonNullable<Tool["videos"]> }) {
  if (!videos?.length) return null;
  return (
    <div className="tvideos">
      {videos.map((v, i) => (
        <div key={v.name + i} className="tvideo">
          {t("chat.video_name_detail_sent_to", {
            name: v.name,
            detail: [bytes(v.size), v.durationMs ? seconds(v.durationMs) : ""].filter(Boolean).join(" · "),
            model: v.model,
          })}
        </div>
      ))}
    </div>
  );
}

function ToolShots({ images }: { images?: string[] }) {
  if (!images?.length) return null;
  return (
    <div className="tshots">
      {images.map((src, i) => (
        <ImagePreview key={src + i} src={src} name={t("chat.tool_screenshot_n", { n: i + 1 })} className="tshot">
          <img src={src} alt={t("chat.tool_screenshot_n", { n: i + 1 })} loading="lazy" />
        </ImagePreview>
      ))}
    </div>
  );
}

function changeCounts(tool: Tool): { added: number; removed: number } | null {
  if (!toolChangedFile(tool)) return null;
  const lines = tool.diff?.split("\n") ?? [];
  const added = tool.added ?? lines.filter((line) => line.startsWith("+") && !line.startsWith("+++")).length;
  const removed = tool.removed ?? lines.filter((line) => line.startsWith("-") && !line.startsWith("---")).length;
  return { added, removed };
}

const NESTED_PREVIEW = 400;
const NESTED_WHOLE_CAP = 200_000;

export function NestedCall({ tool, whole = false, read }: { tool: StateTool; whole?: boolean; read?: ReadOutput }) {
  const [all, setAll] = useState(false);
  const full = useFullOutput(tool, read);
  const body = full.text ?? "";
  const shown = tool.resolvedName || tool.name;
  const tag = tagFor(tool);
  const bad = toolFailed(tool);
  const clipped = !whole && body.length > NESTED_PREVIEW;
  const echoed = toolOutputEchoed(tool);
  const refusal = toolRefusalReason(tool);
  const errorDetail = toolErrorDetail(tool);
  return (
    <div className="call" data-call={tool.id || undefined} data-k={KINDED.has(categoryOf(shown)) ? categoryOf(shown) : undefined}>
      <div className="g">
        <Sym icon={iconFor(shown)} />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm" title={shown}>{labelFor(shown)}</span>
          {tag && <span className="tag" title={tagHint(tool)}>{tag}</span>}
          {tool.args && <span className="arg" title={prettyArgs(tool.args)}>{shortArgs(tool.args)}</span>}
          {bad && <span className="fail">{toolFailureLabel(tool)}</span>}
          <Cost tools={[tool]} />
        </div>
        {tool.output && !echoed && (
          <div className="out">
            {full.note}
            <Term text={clipped && !all ? body.slice(0, NESTED_PREVIEW) : body.slice(0, NESTED_WHOLE_CAP)} />
            {(whole || all) && body.length > NESTED_WHOLE_CAP && <div className="bound bad">{t("chat.output_too_long_showing_the")}</div>}
            {clipped && (
              <div className="bound">
                {all ? null : t("chat.showing_only_the_first_400")}
                <button type="button" className="out-more" data-action="tool.show-all" aria-expanded={all} onClick={() => setAll(!all)}>
                  {all ? t("common.collapse") : t("chat.show_all_2")}
                </button>
              </div>
            )}
          </div>
        )}
        {tool.err && (
          <div className="out">
            {refusal && <div className="txt" data-refusal={tool.refusalCode}>{refusal}</div>}
            {errorDetail && <div className="txt bad">{errorDetail}</div>}
          </div>
        )}
      </div>
    </div>
  );
}

// The plan is the one payload the flow shows twice on purpose: the rail tracks
// it for the rest of the turn, the card records what it was when it was written.
function Steps({ tool }: { tool: Tool }) {
  const steps = parsePlan(tool);
  if (!steps?.length) return <span className="fold">{t("chat.the_plan_is_in_the")}</span>;
  const now = currentStep(steps);
  return (
    <div className="steps">
      {steps.map((st, i) => (
        <div className="s" key={i} data-done={stepDone(st) ? "" : undefined} data-now={i === now ? "" : undefined}>
          <span className="b">{stepDone(st) ? <StudioIcon name="check" /> : i + 1}</span>
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
