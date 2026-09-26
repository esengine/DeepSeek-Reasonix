import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, ModelEntry, SessionStatus, Attachment } from "../port/port";
import { Picker } from "./Menu";
import { Policy } from "./Policy";
import { modelMenu } from "./modelmenu";
import { effortMenu, effortReading, effortsFor } from "./effort";
import { CompletionMenu, useCompletion } from "./Completion";
import { useIme } from "./ime";
import { countLines, pasteIsLong, planTone, planVerb } from "./intake";
import { useIntake } from "./useIntake";
import type { Dropped } from "./filedrop";
import type { Quote } from "./cards/SayCard";
import { StudioIcon } from "./StudioIcon";
import { usePromptRefine } from "./PromptRefine";
import { useProviderOrder } from "../state/providerorder";

interface Props {
  port: AgentPort;
  status: SessionStatus | null;
  running: boolean;
  // Text a card asked to be quoted, and a counter that makes the same text
  // twice two requests. Quoting the same reply again is an ordinary thing to
  // do, and comparing the string alone would drop the second one.
  quote?: Quote;
  // Resolves false when the line never left, so what was typed comes back
  // rather than being lost to a refusal the user could not have prevented.
  // Bumped when something outside asks for the cursor — answering a plan card
  // with "revise" is a request to say what to change, and the saying happens here.
  focus?: number;
  onSubmit: (text: string) => Promise<boolean>;
  onChanged: () => void;
  onError: (e: unknown) => void;
  onSettings?: (section?: string) => void;
  changeCount?: number;
  // Bumped when settings change; a source edited there can change the ladder.
  pulse?: number;
}

// What is riding along with this turn. An attachment travels as bytes or a path
// the kernel already saved; a long paste is held out of the box so eight hundred
// lines of log do not become the composer.
type Chip =
  | {
      k: "attachment";
      id: string;
      name: string;
      state: "adding" | "ready" | "failed";
      a?: Attachment;
      blob?: File;
      url?: string;
      error?: string;
    }
  | { k: "paste"; id: string; body: string; lines: number; name?: string }
  | { k: "quote"; id: string; body: string; turn?: number; lines: number };

// Two screenshots pasted in a row are one filename apart, which is the one
// thing the chip has to tell them by. The preview comes off the blob that was
// just attached — the kernel keeps the bytes, this keeps a handle to look at.
function previewURL(blob: Blob): string | undefined {
  try {
    return URL.createObjectURL(blob);
  } catch {
    return undefined;
  }
}

// A dropped file has no preview to stand behind: the host named it, it was
// never read. Its kind fills the square, because a blank one reads as an image
// that failed to load.
function kindOf(path: string): string {
  const name = nameOf(path);
  const dot = name.lastIndexOf(".");
  return dot > 0 ? name.slice(dot + 1).toUpperCase().slice(0, 4) : "FILE";
}

function nameOf(path: string): string {
  return path.split(/[\\/]/).pop() ?? path;
}

function chipName(c: Chip): string {
  if (c.k === "quote") return c.turn === undefined ? t("引用回复") : t("引用第 {n} 轮回复", { n: c.turn });
  return c.k === "paste" ? c.name ?? t("粘贴的文本") : c.name;
}

// What the model reads instead of a bare blockquote. Which reply this is about
// is the kernel's turn number, so it is stated rather than left to be inferred
// from the words; a rebuilt transcript with no turn says nothing instead of
// guessing one. English like the kernel's other host-authored prefixes: the
// line is addressed to the model, not to the reader.
function quoteBlock(c: Extract<Chip, { k: "quote" }>): string {
  const head = c.turn === undefined
    ? "[Quoting an earlier assistant reply in this conversation.]"
    : `[Quoting the assistant reply from turn ${c.turn}.]`;
  return [head, ...c.body.split("\n").map((line) => `> ${line}`)].join("\n");
}

function isPicture(c: Chip): boolean {
  return c.k === "attachment" && (c.a?.image === true || (c.a?.image !== false && c.blob?.type.startsWith("image/") === true));
}

function releaseChip(c: Chip) {
  if (c.k === "attachment" && c.url) URL.revokeObjectURL(c.url);
}

let chipSeq = 0;
const chipId = () => `c${++chipSeq}`;

export function Composer({ port, status, running, quote, focus, onSubmit, onChanged, onError, onSettings = () => {}, changeCount = 0, pulse = 0 }: Props) {
  const providerOrder = useProviderOrder();
  const [branch, setBranch] = useState("");
  useEffect(() => {
    let alive = true;
    port.capabilityScope()
      .then((scope) => alive && setBranch(scope.repo ? (scope.branch || t("分离状态")) : ""))
      .catch(() => alive && setBranch(""));
    return () => { alive = false; };
  }, [port, status?.workspaceRoot]);
  const [text, setText] = useState("");
  // The caret decides which token is being completed, so it is state here
  // rather than something read off the element when a menu happens to open.
  const [caret, setCaret] = useState(0);
  const [shots, setShots] = useState<Chip[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [stopping, setStopping] = useState(false);
  const submittingRef = useRef(false);
  const stoppingRef = useRef(false);
  const picker = useRef<HTMLInputElement>(null);
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [busy, setBusy] = useState<Record<string, boolean>>({});
  const box = useRef<HTMLTextAreaElement>(null);
  const refine = usePromptRefine(port, text, (next) => {
    setText(next);
    setCaret(next.length);
    box.current?.focus();
  });
  const guide = useId();
  const completionId = useId();
  const attachTipId = useId();
  const branchTipId = useId();
  // Set only when a completion moved the caret: the browser puts it at the end
  // of a programmatic value, which is wrong for anything accepted mid-line.
  const pending = useRef<number | null>(null);

  const loadModels = useCallback(() => {
    port.models().then(setModels).catch(() => setModels([]));
  }, [port]);
  useEffect(loadModels, [loadModels, pulse]);

  // A drop lands where the caret is, and the handler that receives it was built
  // once — so the position it reads has to be a ref, not the render's copy.
  const caretRef = useRef(0);
  const type = (next: string, at: number) => {
    setText(next);
    setCaret(at);
    caretRef.current = at;
  };

  // A quote rides above the box like a long paste does, for the same reason: an
  // answer pasted into the composer becomes the composer. It is material for
  // the question, not the question, so it stays removable until the turn goes.
  useEffect(() => {
    if (!quote?.n) return;
    setShots((prev) => [
      ...prev,
      { k: "quote", id: chipId(), body: quote.text, turn: quote.turn, lines: countLines(quote.text) },
    ]);
    queueMicrotask(() => box.current?.focus());
  }, [quote?.n]);

  const menu = useCompletion(port, text, caret, (next, at) => {
    pending.current = at;
    type(next, at);
    box.current?.focus();
  });
  const ime = useIme();

  // A counter, not a boolean: asking twice in a row has to move the cursor twice.
  useEffect(() => {
    if (focus) box.current?.focus();
  }, [focus]);

  useEffect(() => {
    if (running) return;
    stoppingRef.current = false;
    setStopping(false);
  }, [running]);

  const sizeBox = useCallback(() => {
    const el = box.current;
    if (!el) return;
    if (pending.current !== null) {
      el.setSelectionRange(pending.current, pending.current);
      pending.current = null;
    }
    // CSS caps the top by the available room; the element still has to be told to grow.
    // The floor is not decoration: under an interface zoom, scrollHeight is not
    // in the same units the height we write back is, and the two engines do not
    // round it the same way. Writing a smaller number than one line squeezes the
    // box shut — an empty composer with both scrollbars showing and nowhere to
    // type. One line is the least it can ever legitimately be.
    const line = parseFloat(getComputedStyle(el).lineHeight) || 22;
    el.style.height = "auto";
    // A placeholder is not content. On first paint the sidebars may still own
    // most of a narrow viewport, and its wrapped scrollHeight must not become
    // the empty editor's remembered height.
    el.style.height = `${text ? Math.max(line, el.scrollHeight) : line}px`;
  }, [text]);

  useLayoutEffect(sizeBox, [sizeBox]);

  useEffect(() => {
    const el = box.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    let width = el.getBoundingClientRect().width;
    const observer = new ResizeObserver(([entry]) => {
      const next = entry?.contentRect.width ?? width;
      if (Math.abs(next - width) < 0.5) return;
      width = next;
      sizeBox();
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [sizeBox]);

  // Attachments ride into the turn as path references, exactly as they do from
  // the CLI — the host saved the bytes, the turn parser resolves the token. A
  // held-back paste follows the typed text, which is where it reads as the
  // material the message is about rather than as part of the sentence.
  const send = () => {
    if (submittingRef.current || shots.some((c) => c.k === "attachment" && c.state !== "ready")) return;
    const v = text.trim();
    if (!v && shots.length === 0) return;
    const refs = shots.flatMap((c) => (c.k === "attachment" && c.a ? [c.a.ref] : []));
    const pastes = shots.flatMap((c) => (c.k === "paste" ? [c.body] : []));
    // A quote is what the question is about, so it leads; a held-back paste is
    // the material the answer needs and follows what was typed.
    const quotes = shots.flatMap((c) => (c.k === "quote" ? [quoteBlock(c)] : []));
    const line = [...quotes, [...refs, v].filter(Boolean).join(" "), ...pastes].filter(Boolean).join("\n\n");
    const draft = { text, shots, caret: caretRef.current };
    submittingRef.current = true;
    setSubmitting(true);
    type("", 0);
    setShots([]);
    void (async () => {
      try {
        const sent = await onSubmit(line);
        if (sent) draft.shots.forEach(releaseChip);
        else {
          pending.current = draft.caret;
          type(draft.text, draft.caret);
          setShots((current) => [...draft.shots, ...current]);
          queueMicrotask(() => box.current?.focus());
        }
      } catch (e) {
        pending.current = draft.caret;
        type(draft.text, draft.caret);
        setShots((current) => [...draft.shots, ...current]);
        queueMicrotask(() => box.current?.focus());
        onError(e);
      } finally {
        submittingRef.current = false;
        setSubmitting(false);
      }
    })();
  };

  const insert = useCallback((snippet: string) => {
    const el = box.current;
    const at = el ? el.selectionStart : caretRef.current;
    setText((prev) => {
      const cut = Math.min(at, prev.length);
      const next = prev.slice(0, cut) + snippet + prev.slice(cut);
      pending.current = cut + snippet.length;
      return next;
    });
  }, []);

  const discard = useCallback((c: Chip) => {
    releaseChip(c);
    setShots((prev) => prev.filter((x) => x !== c));
  }, []);

  const upload = useCallback(
    (id: string, blob: File) => {
      void port
        .attach(blob, blob.name)
        .then((a) =>
          setShots((prev) => prev.map((c) => (c.k === "attachment" && c.id === id ? { ...c, a, state: "ready", error: "" } : c))),
        )
        .catch((e: unknown) =>
          setShots((prev) => prev.map((c) => (c.k === "attachment" && c.id === id ? { ...c, state: "failed", error: reason(e) } : c))),
        );
    },
    [port],
  );

  const attach = useCallback(
    (blobs: File[]) => {
      if (blobs.length === 0 || submittingRef.current) return;
      const added: Chip[] = blobs.map((blob) => ({
        k: "attachment",
        id: chipId(),
        name: blob.name,
        state: "adding",
        blob,
        url: blob.type.startsWith("image/") ? previewURL(blob) : undefined,
      }));
      setShots((prev) => [...prev, ...added]);
      for (const c of added) if (c.k === "attachment" && c.blob) upload(c.id, c.blob);
    },
    [upload],
  );

  // A dropped file is referenced where it lives. Copying it in is what let a
  // turn edit the copy and report the edit as done while the file the user
  // pointed at never changed. Only the host can mint the token: whether a path
  // is inside the workspace compares two spellings of one location.
  const refIn = useCallback(
    (paths: string[]) => {
      port
        .dropRefs(paths)
        .then((refs) => {
          const took = refs.flatMap((r) =>
            r.ref
              ? [{
                  k: "attachment" as const,
                  id: chipId(),
                  name: nameOf(r.path ?? r.ref),
                  state: "ready" as const,
                  a: { path: r.path ?? "", ref: r.ref, image: r.image },
                }]
              : [],
          );
          if (took.length > 0) setShots((prev) => [...prev, ...took]);
          const refused = refs.flatMap((r) => (r.error ? [r.error] : []));
          if (refused.length > 0) onError(new Error(refused.join("\n")));
        })
        .catch(onError);
    },
    [port, onError],
  );

  // One place decides what a payload becomes, whichever channel carried it.
  const receive = useCallback(
    (d: Dropped) => {
      if (d.paths.length > 0) refIn(d.paths);
      else if (d.files.length > 0) {
        attach(d.files);
      }
      if (d.text) insert(d.text + " ");
      // A drop is the start of typing, not the end of it.
      box.current?.focus();
    },
    [refIn, attach, insert],
  );

  const { plan: drag, ref: dropzone } = useIntake({
    root: status?.workspaceRoot ?? status?.cwd,
    onReceive: receive,
  });

  const efforts = effortsFor(models, status?.modelRef);
  // Whether this model says anything about reasoning levels at all. The session
  // may still carry one from a model that did, and printing that would be the
  // composer answering for an endpoint that never spoke.
  const declared = efforts.length > 0;
  const modelLb = status?.modelRef?.replace(/^[^/]+\//, "") ?? status?.label ?? "—";
  // A model switch rebuilds the runtime kernel-side (~0.4s on a real session);
  // the other controls on this shelf may land immediately, and which is which
  // is the kernel's to say, not this file's. Either way the click needs a
  // pending state or it reads as a dead control —— 而等的是哪一个就只标哪一
  // 个：一整排一起变灰，说的是「现在什么都不能改」，而实际上只有这一个在等
  // 内核回话。
  const busyRef = useRef(new Set<string>());
  const change = (key: string, call: () => Promise<void>) => {
    if (busyRef.current.has(key)) return;
    busyRef.current.add(key);
    setBusy((b) => ({ ...b, [key]: true }));
    void Promise.resolve().then(call).then(onChanged).catch(onError).finally(() => {
      busyRef.current.delete(key);
      setBusy((b) => ({ ...b, [key]: false }));
    });
  };

  const adding = shots.some((c) => c.k === "attachment" && c.state === "adding");
  const failed = shots.some((c) => c.k === "attachment" && c.state === "failed");
  const hasDraft = text.trim().length > 0 || shots.some((c) => c.k !== "attachment" || c.state === "ready");
  const lines = countLines(text);
  const showCount = text.length >= 240 || lines > 3;
  const sendDisabled = submitting || stopping || adding || failed || !hasDraft;
  const warnPictures = status?.vision === false && shots.some((c) => c.k === "attachment" && c.state === "ready" && isPicture(c));

  return (
    // display:contents, so the box looks exactly as it did and the drop layer
    // still has one node to ask which pane a drop landed in.
    <div className="dropzone" ref={dropzone}>
      {menu.open && (
        <CompletionMenu
          id={completionId}
          items={menu.completion.items}
          active={menu.active}
          kind={menu.completion.kind}
          query={menu.completion.query ?? ""}
          kb={menu.kb}
          onPick={menu.accept}
          onHover={menu.hover}
        />
      )}
      {/* What letting go will do, said before it happens. A drop that only
          reports afterwards is the pattern this replaces. */}
      {drag && (
        <p className="intake" data-tone={planTone(drag)} role="status" aria-live="polite">
          {planVerb(drag)}
        </p>
      )}
      {refine.card}
      {shots.length > 0 && (
        <ul className="shots">
          {shots.map((c, i) => (
            <li
              className="shot"
              key={c.id}
              data-state={c.k === "attachment" ? c.state : undefined}
              style={{ "--i": i } as React.CSSProperties}
            >
              {c.k === "attachment" ? (
                <>
                  {isPicture(c) ? (
                    <span
                      className="thumb"
                      aria-hidden="true"
                      style={c.url ? { backgroundImage: `url(${c.url})` } : undefined}
                    />
                  ) : (
                    <span className="glyph" aria-hidden="true">{kindOf(c.a?.path ?? c.name)}</span>
                  )}
                  <span className="meta">
                    <span className="nm" title={c.a?.path ?? c.name}>{c.name}</span>
                    {c.state === "adding" && <span className="sz live">{t("正在添加…")}</span>}
                    {c.state === "ready" && <span className="sz">{isPicture(c) ? t("图片") : t("文件")}</span>}
                    {c.state === "failed" && (
        <button
                        className="retry"
                        data-action="session.attach"
                        title={c.error}
                        onClick={() => {
                          if (!c.blob) return;
                          setShots((prev) => prev.map((x) => (x === c ? { ...c, state: "adding", error: "" } : x)));
                          upload(c.id, c.blob);
                        }}
                      >
                        {t("添加失败 · 重试")}
                      </button>
                    )}
                  </span>
                </>
              ) : (
                <>
                  <span className="glyph" aria-hidden="true">{c.k === "quote" ? "❝" : "TXT"}</span>
                  <span className="meta">
                    <span className="nm" title={c.body}>{chipName(c)}</span>
                    <button
                      className="undo"
                      onClick={() => {
                        discard(c);
                        insert(c.k === "quote" ? quoteBlock(c) : c.body);
                      }}
                    >
                      {t("{n} 行 · 展开到输入框", { n: c.lines })}
                    </button>
                  </span>
                </>
              )}
              <button
                className="x"
                aria-label={t("移除 {name}", { name: chipName(c) })}
                onClick={() => discard(c)}
              >
                ×
              </button>
            </li>
          ))}
        </ul>
      )}
      {/* A warning cannot be the last horizontally scrolling attachment: that
          is exactly where it disappears when there are enough files to matter. */}
      {warnPictures && (
        <p className="shotwarn" role="status">
          {status?.visionDeclared === false
            ? t("该模型是否支持读图未声明 · 暂按不支持处理；在「连接」中勾选后即可直接发送")
            : t("当前模型不支持读图 · 图片将按看图模型设置处理")}
        </p>
      )}
      {/* The prompt glyph sits beside the box rather than in it, so it cannot be
          dragged into a selection of what was typed. */}
      <div className="fmain">
        <span className="prompt" aria-hidden="true">
          ›
        </span>
        <textarea
          data-action-keydown="session.send"
          ref={box}
          rows={1}
          value={text}
          placeholder={t("描述任务、问题或要改的内容…")}
          role="combobox"
          aria-label={t("任务输入")}
          aria-describedby={guide}
          aria-keyshortcuts="Enter Shift+Enter"
          aria-busy={submitting}
          readOnly={submitting}
          aria-expanded={menu.open}
          aria-controls={menu.open ? completionId : undefined}
          aria-autocomplete="list"
          aria-activedescendant={menu.open ? `${completionId}-${menu.active}` : undefined}
          onChange={(e) => type(e.target.value, e.target.selectionStart)}
          onBlur={() => menu.dismiss()}
          // Arrow keys and clicks move the caret without changing the text, and
          // the caret is what decides which token the menu is completing.
          onKeyUp={(e) => setCaret(e.currentTarget.selectionStart)}
          onClick={(e) => setCaret(e.currentTarget.selectionStart)}
          data-action-paste="session.attach"
          // Dropping is the pane's job — a one-row box is 40px to aim at, and the
          // handler that used to live here prevented the default and then did
          // nothing with text, which is worse than not handling it at all.
          onPaste={(e) => {
            if (submitting) {
              e.preventDefault();
              return;
            }
            const files = [...e.clipboardData.files];
            if (files.length > 0) {
              e.preventDefault();
              attach(files);
              return;
            }
            const body = e.clipboardData.getData("text/plain");
            if (!pasteIsLong(body)) return;
            // Long enough to bury the composer. It still goes with the turn — it
            // is just held beside the box instead of becoming it.
            e.preventDefault();
            setShots((prev) => [...prev, { k: "paste", id: chipId(), body, lines: countLines(body) }]);
          }}
          {...ime.handlers}
          onKeyDown={(e) => {
            if (!ime.isIme(e.nativeEvent) && refine.onKey(e)) return;
            // Picking a word from an input method is not typing in this box: its
            // Enter confirms a candidate, and acting on it would send a
            // half-written message or accept a completion nobody asked for.
            if (ime.isIme(e.nativeEvent)) {
              // Esc dismisses the candidate window; letting it through would
              // cancel the running turn as a side effect of closing an IME.
              if (e.key === "Escape") e.stopPropagation();
              // That Enter belongs to the input method, so it must do nothing —
              // returning without stopping it left the textarea to insert a
              // newline, which is why the first Enter after a word broke the line
              // and only the second one sent.
              if (e.key === "Enter") e.preventDefault();
              return;
            }
            if (menu.open && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
              e.preventDefault();
              menu.move(e.key === "ArrowDown" ? 1 : -1);
              return;
            }
            // Tab completes, always. Enter belongs to the menu only where the
            // line is not yet a message — a half-typed command — or where the
            // user went looking through the list themselves.
            if (menu.open && (e.key === "Tab" || (e.key === "Enter" && menu.ownsEnter)) && !e.shiftKey) {
              e.preventDefault();
              menu.accept();
              return;
            }
            // Esc closes the menu and stops there: reaching the app would cancel
            // the running turn, which is not what dismissing a menu means.
            if (menu.open && e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              menu.dismiss();
              return;
            }
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              send();
            }
          }}
        />
      </div>
      <div className="composeguide" id={guide}>
        <span className="composecaps" aria-hidden="true">
          <span><kbd>/</kbd>{t("命令与技能")}</span>
          <span><kbd>@</kbd>{t("引用文件")}</span>
        </span>
        <span className="composehint" aria-live="polite">
          {stopping
            ? t("正在停止…")
            : submitting
            ? t("正在发送…")
            : adding
              ? t("正在添加附件…")
              : failed
                ? t("有附件添加失败，请重试或移除")
                : t(running ? "Enter 插话 · Shift+Enter 换行" : "Enter 发送 · Shift+Enter 换行")}
        </span>
        {showCount && <span className="fcount">{t("{n} 字 · {lines} 行", { n: text.length, lines })}</span>}
      </div>
      <div className="row">
        {/* 拖进来和粘贴都走同一条路，但那两个都得先有一张图在手边。点开系统
            选择器是唯一不需要预备动作的入口。 */}
        <input
          ref={picker}
          data-action-change="session.attach"
          type="file"
          multiple
          hidden
          onChange={(e) => {
            attach([...(e.target.files ?? [])]);
            // 同一张图再选一次也要能进来，所以每次用完清空。
            e.target.value = "";
          }}
        />
        <div className="turntools">
          <button
            className="mode plain attach studio-attach"
            aria-label={t("添加附件")}
            aria-describedby={attachTipId}
            onClick={() => picker.current?.click()}
          >
            <StudioIcon name="plus" />
            <span className="studio-control-tip" id={attachTipId} role="tooltip">
              <b>{t("添加附件")}</b>
              <span>{t("也可直接拖入或粘贴")}</span>
            </span>
          </button>
          {refine.button}
        <div className="studio-mode-control">
          <Picker
            className="mode studio-mode-picker"
            triggerAction="plan.mode"
            ariaPressed={status?.plan ?? false}
            place="bottom"
            title={t("工作模式")}
            current={status?.plan ? "plan" : "agent"}
            pending={busy["plan"]}
            items={[
              { value: "__mode", label: t("工作模式"), right: t("当前任务"), header: true },
              { value: "agent", label: "Agent", desc: t("执行任务并使用已启用工具") },
              { value: "plan", label: "Plan", desc: t("先整理步骤，不写入文件") },
              { value: "ask", label: "Ask", desc: t("直接回答，不调用工具"), disabled: true },
            ]}
            onPick={(value) => change("plan", () => port.setPlanMode(value === "plan"))}
            label={<><StudioIcon name="agent" /><span className="studio-sr-label">{t("计划")}</span><span>{status?.plan ? "Plan" : "Agent"}</span><StudioIcon name="down" /></>}
          />
        </div>
        {branch && (
          <div className="studio-branch-pop">
            <div
              className="mode plain studio-branch"
              tabIndex={0}
              aria-label={t("当前 Git 分支：{branch}", { branch })}
              aria-describedby={branchTipId}
            >
              <span className="ic" aria-hidden="true"><StudioIcon name="branch" /></span>
              <span className="lb">{branch}</span>
              {changeCount > 0 && <small>{t("{n} 个变更", { n: changeCount })}</small>}
            </div>
            <div className="studio-branch-card" id={branchTipId} role="tooltip">
              <b>{t("当前分支 · {branch}", { branch })}</b>
              <span>{changeCount > 0 ? t("当前工作区 · {n} 个本地变更", { n: changeCount }) : t("当前工作区 · 后续任务继续使用此分支")}</span>
              <small>{t("仅作状态提示，无需点击")}</small>
            </div>
          </div>
        )}
        {/* The toggle keeps its legacy meaning: it follows `plan`, which the
            kernel turns off the moment a plan is approved. The lifecycle is a
            separate reading — an approved plan is still running, and saying so
            here is not the same as offering to turn planning back on. */}
        {status?.planPhase === "executing" && (
          <span className="mode plain" data-plan-phase="executing" title={t("正在执行已批准的计划")}>
            <span className="lb">{t("执行计划中")}</span>
          </span>
        )}
        <Policy port={port} status={status} onChanged={onChanged} onBoundary={() => onSettings("tools:sandbox")} />
        <span className="turntools-spacer" aria-hidden="true" />
        <div className="studio-model-group">
          <Picker
            wrapClassName="studio-model-control"
            className="mode model-picker"
            data-action="model.select"
            place="bottom"
            title={status?.modelRef ?? modelLb}
            current={status?.modelRef}
            items={modelMenu(models, providerOrder)}
            menuClassName="studio-model-menu"
            menuTitle={<><b>{t("选择模型")}</b><small>{t("用于后续任务")}</small></>}
            onOpen={loadModels}
            pending={busy["model"]}
            onPick={(ref) => ref === "__manage-models" ? onSettings("providers") : change("model", () => port.setModel(ref))}
            label={<><span className="nm">{modelLb}</span><StudioIcon name="down" /></>}
          />
          {/* 推理强度属于模型能力，因此与模型共用一组轮廓。 */}
          {status && (
            <div className="studio-effort-control">
              <Picker
                className="mode studio-effort-picker"
                data-action="reasoning.effort"
                place="bottom"
                align="end"
                menuClassName="studio-effort-menu"
                onOpen={loadModels}
                title={t("推理强度")}
                current={declared ? status.effort || "auto" : ""}
                pending={busy["effort"]}
                items={effortMenu(efforts, modelLb, "__effort-declare")}
                onPick={(value) => value === "__effort-declare" ? onSettings("providers:effort-declare") : change("effort", () => port.setEffort(value))}
                label={<><span>{declared ? effortReading(status.effort) : t("未声明")}</span><StudioIcon name="down" /></>}
              />
            </div>
          )}
        </div>
        </div>
        <span className="go">
          {running && (
            <button
              className="btn stop"
              data-primary
              data-action="session.stop"
              data-pending={stopping ? "" : undefined}
              disabled={stopping}
              aria-busy={stopping}
              onClick={() => {
                if (stoppingRef.current) return;
                stoppingRef.current = true;
                setStopping(true);
                void port.cancel().catch((e: unknown) => {
                  stoppingRef.current = false;
                  setStopping(false);
                  onError(e);
                });
              }}
            >
              <span className="ic" aria-hidden="true">
                <svg viewBox="0 0 16 16">
                  <rect x="4.8" y="4.8" width="6.4" height="6.4" rx="1.3" />
                </svg>
              </span>
              <span>{t(stopping ? "正在停止…" : "停下")}</span>
            </button>
          )}
          {/* While a turn runs, the emphasised action is stopping it: steering is
              one Enter away and the footer says so, while stopping has no other
              way in. The rightmost button keeps its place either way — it is
              always "send what I typed". */}
          <button
            className="btn send"
            data-primary={running ? undefined : ""}
            data-action="session.send"
            data-running={running ? "" : undefined}
            disabled={sendDisabled}
            aria-busy={submitting}
            onClick={send}
          >
            <span className="ic" aria-hidden="true">
              <svg viewBox="0 0 16 16">
                <path d="M8 12.8V3.4M4.2 7.2 8 3.4l3.8 3.8" />
              </svg>
            </span>
            <span>{t(submitting ? "正在发送…" : running ? "插话" : "发送")}</span>
          </button>
        </span>
      </div>
    </div>
  );
}
