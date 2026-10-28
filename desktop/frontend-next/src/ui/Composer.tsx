import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { keepPasteAsFile, PasteKeep, type PasteChip } from "./pastekeep";
import type { AgentPort, ChipCall, ModelEntry, SessionStatus, WorkspaceGit } from "../port/port";
import { Picker } from "./Menu";
import { BranchChip } from "./BranchChip";
import { WorktreeChip } from "./WorktreeChip";
import { useWorktreeSetup, WorktreeSetupFailure } from "./useWorktreeSetup";
import { WorktreeSetupPage } from "./WorktreeSetupPage";
import { Policy } from "./Policy";
import { chosenGroups, groupHeadings, modelLabel, modelMenu, modelTooltip, narrowToGroups } from "./modelmenu";
import { ModelGroupChips } from "./ModelGroupChips";
import { effortMenu, effortReading, effortsFor, forcesThinkingFor, routeEffortPick } from "./effort";
import { CompletionMenu, useCompletion } from "./Completion";
import { ChipMirror, useSkillChips } from "./ChipMirror";
import { useIme } from "./ime";
import { usePlaceholderFit } from "./placeholderfit";
import { useFitHeight } from "./fitHeight";
import { useSendShortcut } from "../state/prefs";
import { sendAction, sendAria, sendHint, sendLabel, sendsOnKey } from "./sendkey";
import { countLines, pasteIsLong, planTone, planVerb } from "./intake";
import { kindOf, nameOf, previewURL } from "./chipfile";
import { useIntake } from "./useIntake";
import type { Dropped } from "./filedrop";
import type { Quote } from "./cards/SayCard";
import { StudioIcon } from "./StudioIcon";
import { usePromptRefine } from "./PromptRefine";
import { useProviderOrder } from "../state/providerorder";
import { useModelFilter, writeModelFilter } from "../state/modelfilter";
import { useDraft } from "./useDraft";
import type { Submitted } from "./usesubmit";
import { touchKeyboard } from "./touchKeyboard";
import { ImagePreview } from "./ImagePreview";
import { SideAskButton } from "./SideAskButton";
import { chipId, chipName, isPicture, quoteBlock, releaseChip, type Chip } from "./composerChips";

interface Props {
  worktreeTarget?: HTMLElement | null;
  onWorktreePhase?: (phase: string | null) => void;
  fresh?: boolean;
  port: AgentPort;
  status: SessionStatus | null;
  running: boolean;
  queued?: boolean;
  // Text a card asked to be quoted, and a counter that makes the same text
  // twice two requests. Quoting the same reply again is an ordinary thing to
  // do, and comparing the string alone would drop the second one.
  quote?: Quote;
  // A line taken back from the queue. The counter makes the same text twice
  // two requests, as it does for a quote.
  restore?: { n: number; text: string };
  // Text another surface hands the composer, such as a file reference; it goes
  // in at the caret with a space on either side.
  inject?: { n: number; text: string; session?: { id: string; title: string } };
  // Resolves false when the line never left, so what was typed comes back
  // rather than being lost to a refusal the user could not have prevented.
  // Bumped when something outside asks for the cursor — answering a plan card
  // with "revise" is a request to say what to change, and the saying happens here.
  focus?: number;
  onSubmit: (text: string, chips?: ChipCall) => Promise<Submitted>;
  onChanged: () => void;
  onError: (e: unknown) => void;
  onSettings?: (section?: string) => void;
  side?: boolean;
  onSide?: () => void;
  sidePanel?: ReactNode;
  changeCount?: number;
  countElsewhere?: boolean;
  // The work tree's Git state as git itself reports it, refreshed on the same
  // path as changeCount (turn boundaries, writes). null is "not answered yet";
  // repo:false is a workspace with no repository, and the two render apart.
  git?: WorkspaceGit | null;
  // The switch already answers with the workspace's Git state.
  onTreeChanged?: (git: WorkspaceGit) => void;
  // Bumped when settings change; a source edited there can change the ladder.
  pulse?: number;
  draftKey?: string;
}

export function Composer({ port, status, running, queued = false, quote, restore, inject, focus, onSubmit, onChanged, onError, onSettings = () => {}, side = false, onSide, sidePanel, changeCount = 0, countElsewhere = false, git = null, onTreeChanged, pulse = 0, draftKey = "", fresh = false, worktreeTarget, onWorktreePhase }: Props) {
  const touch = touchKeyboard();
  const providerOrder = useProviderOrder();
  const modelGroups = useModelFilter();
  const [submitting, setSubmitting] = useState(false);
  const worktree = useWorktreeSetup(port, fresh && !!git?.repo, status?.workspaceRoot ?? "", onChanged, onTreeChanged, status?.sessionPath ?? "");
  const [setupDraft, setSetupDraft] = useState<{ text: string; files: string[] }>({ text: "", files: [] });
  const setupPhase = worktree.stage?.phase ?? null;
  useEffect(() => { onWorktreePhase?.(setupPhase); }, [onWorktreePhase, setupPhase]);
  const { text, setText, beginSubmit, finishSubmit } = useDraft(draftKey, submitting);
  // The caret decides which token is being completed, so it is state here
  // rather than something read off the element when a menu happens to open.
  const [caret, setCaret] = useState(0);
  const [shots, setShots] = useState<Chip[]>([]);
  const uploads = useRef(new Map<string, AbortController>());
  useEffect(() => () => {
    uploads.current.forEach((upload) => upload.abort());
    uploads.current.clear();
  }, []);
  const [stopping, setStopping] = useState(false);
  const submittingRef = useRef(false);
  const stoppingRef = useRef(false);
  const picker = useRef<HTMLInputElement>(null);
  const [models, setModels] = useState<ModelEntry[]>([]);
  const menuItems = modelMenu(models, providerOrder, status?.modelRef);
  const chosenModelGroups = chosenGroups(menuItems, modelGroups);
  const [busy, setBusy] = useState<Record<string, boolean>>({});
  const box = useRef<HTMLTextAreaElement>(null);
  const hadSetup = useRef(false);
  useEffect(() => {
    if (hadSetup.current && !setupPhase) box.current?.focus();
    hadSetup.current = !!setupPhase;
  }, [setupPhase]);
  const refine = usePromptRefine(port, text, (next) => {
    setText(next);
    setCaret(next.length);
    box.current?.focus();
  });
  const guide = useId();
  const completionId = useId();
  const attachTipId = useId();
  // Set only when a completion moved the caret: the browser puts it at the end
  // of a programmatic value, which is wrong for anything accepted mid-line.
  const pending = useRef<number | [number, number] | null>(null);

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
      { k: "quote", id: chipId(), body: quote.text, turn: quote.turn, source: quote.source, lines: countLines(quote.text) },
    ]);
    queueMicrotask(() => box.current?.focus());
  }, [quote?.n]);

  const restoredAt = useRef(restore?.n ?? 0);
  useEffect(() => {
    if (!restore?.n || restore.n === restoredAt.current) return;
    restoredAt.current = restore.n;
    setText((prev) => {
      const next = !prev.trim() || prev.trim() === restore.text.trim()
        ? restore.text : `${prev.replace(/\s+$/, "")}\n${restore.text}`;
      pending.current = next.length;
      return next;
    });
    queueMicrotask(() => box.current?.focus());
  }, [restore?.n]);

  const injectedAt = useRef(inject?.n ?? 0);
  useEffect(() => {
    if (!inject?.n || inject.n === injectedAt.current) return;
    injectedAt.current = inject.n;
    if (inject.session) {
      addSession(inject.session);
      queueMicrotask(() => box.current?.focus());
      return;
    }
    const where = caret;
    setText((prev) => {
      const at = Math.min(where, prev.length);
      const lead = at > 0 && !/\s$/.test(prev.slice(0, at)) ? " " : "";
      const piece = lead + inject.text + " ";
      pending.current = at + piece.length;
      return prev.slice(0, at) + piece + prev.slice(at).replace(/^ /, "");
    });
    queueMicrotask(() => box.current?.focus());
  }, [inject?.n]);

  const moveTo = useCallback((next: string, at: number) => {
    pending.current = at;
    type(next, at);
  }, []);
  const chips = useSkillChips(box, text, moveTo);
  const addSession = useCallback((s: { id: string; title: string }) => setShots((prev) => prev.some((c) => c.k === "session" && c.session === s.id) ? prev : [...prev, { k: "session", id: chipId(), session: s.id, title: s.title }]), []);
  const menu = useCompletion(port, text, caret, (next, at) => {
    moveTo(next, at);
    box.current?.focus();
  }, chips, addSession);
  const ime = useIme();
  const sendShortcut = useSendShortcut();

  // A counter, not a boolean: asking twice in a row has to move the cursor twice.
  useEffect(() => {
    if (focus) box.current?.focus();
  }, [focus]);

  // The kernel's own report that no turn is live ends a stop even when the
  // turn-done event never reached this window.
  const kernelIdle = status?.running === false;
  useEffect(() => {
    if (running && !kernelIdle) return;
    stoppingRef.current = false;
    setStopping(false);
  }, [running, kernelIdle]);

  const placeCaret = useCallback((el: HTMLTextAreaElement) => {
    if (pending.current === null) return;
    const [from, to] = typeof pending.current === "number" ? [pending.current, pending.current] : pending.current;
    el.setSelectionRange(from, to);
    pending.current = null;
  }, []);
  useFitHeight(box, text, placeCaret);
  const placeholder = usePlaceholderFit(box, queued ? [t("继续输入以排队后续修改"), t("继续输入…"), "…"] : [t("描述任务、问题或要改的内容…"), t("描述任务…"), "…"]);

  // Attachments ride into the turn as path references, exactly as they do from
  // the CLI — the host saved the bytes, the turn parser resolves the token. A
  // held-back paste follows the typed text, which is where it reads as the
  // material the message is about rather than as part of the sentence.
  const keepPaste = (chip: PasteChip) => keepPasteAsFile(port, chip, setShots);

  const send = () => {
    if (submittingRef.current || shots.some((c) => c.k === "attachment" && c.state !== "ready")) return;
    const v = text.trim();
    if (!v && shots.length === 0) return;
    const refs = shots.flatMap((c) => (c.k === "attachment" && c.a ? [c.a.ref] : c.k === "session" ? ["@session:" + c.session] : []));
    const pastes = shots.flatMap((c) => (c.k === "paste" ? [c.saved ? c.saved.pointer.trim() : c.body] : []));
    // A quote is what the question is about, so it leads; a held-back paste is
    // the material the answer needs and follows what was typed.
    const quotes = shots.flatMap((c) => (c.k === "quote" ? [quoteBlock(c)] : []));
    const compose = (t: string) => [...quotes, [...refs, t.trim()].filter(Boolean).join(" "), ...pastes].filter(Boolean).join("\n\n");
    const line = compose(v);
    const call = chips.call(compose);
    const draft = { text, shots, caret: caretRef.current, chips: chips.held() };
    if (worktree.selection || worktree.stage) setSetupDraft({ text: v, files: shots.map(chipName) });
    const restoreDraft = (select = false) => {
      finishSubmit(false);
      pending.current = select ? [0, draft.text.length] : draft.caret;
      type(draft.text, draft.caret);
      chips.restore(draft.chips);
      setShots((current) => [...draft.shots, ...current]);
      if (!worktree.selection && !worktree.stage) queueMicrotask(() => box.current?.focus());
    };
    beginSubmit(text);
    submittingRef.current = true;
    setSubmitting(true);
    type("", 0);
    setShots([]);
    void (async () => {
      try {
        if (worktree.selection || worktree.stage) {
          const prepared = await worktree.prepare(draft.shots.flatMap((chip) => chip.k === "attachment" && chip.a ? [chip.a.path] : chip.k === "paste" && chip.saved ? [chip.saved.path] : []));
          if (!prepared) { restoreDraft(); return; }
        }
        const sent = await (call ? onSubmit(line, call) : onSubmit(line));
        if (sent === true) {
          finishSubmit(true);
          worktree.dismiss();
          draft.shots.forEach(releaseChip);
        }
        else {
          // A command this connection will never run is left selected, so the
          // next thing typed replaces it instead of joining it.
          restoreDraft(sent === "shell_unavailable");
          worktree.dismiss();
        }
      } catch (e) {
        restoreDraft();
        if (!(e instanceof WorktreeSetupFailure)) onError(e);
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
    uploads.current.get(c.id)?.abort();
    uploads.current.delete(c.id);
    releaseChip(c);
    setShots((prev) => prev.filter((x) => x !== c));
  }, []);

  const upload = useCallback(
    (id: string, blob: File) => {
      const controller = new AbortController();
      uploads.current.get(id)?.abort();
      uploads.current.set(id, controller);
      void port
        .attach(blob, blob.name, controller.signal)
        .then((a) =>
          setShots((prev) => prev.map((c) => (c.k === "attachment" && c.id === id ? { ...c, a, state: "ready", error: "" } : c))),
        )
        .catch((e: unknown) =>
          setShots((prev) => prev.map((c) => (c.k === "attachment" && c.id === id ? { ...c, state: "failed", error: reason(e) } : c))),
        )
        .finally(() => {
          if (uploads.current.get(id) === controller) uploads.current.delete(id);
        });
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
  const forcedThinking = forcesThinkingFor(models, status?.modelRef);
  const modelLb = modelLabel(models, status);
  // A model switch rebuilds the runtime kernel-side; other controls here may
  // land at once. Each click needs its own pending state: greying the whole
  // shelf says nothing can change while only one control waits on the kernel.
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
  const sendDisabled = submitting || stopping || adding || failed || !hasDraft || !!worktree.stage;
  const warnPictures = status?.vision === false && shots.some((c) => c.k === "attachment" && c.state === "ready" && isPicture(c));

  const setupPage = worktree.stage && <WorktreeSetupPage stage={worktree.stage} task={setupDraft.text} files={setupDraft.files} busy={submitting} onRetry={send} onCancel={() => void worktree.cancel()} onEdit={worktree.dismiss}/>;
  return (<>
    {setupPage && (worktreeTarget ? createPortal(setupPage, worktreeTarget) : setupPage)}
    <div className="dropzone" ref={dropzone} data-worktree-setup={worktree.stage ? "" : undefined} inert={!!worktree.stage}>
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
      <div role="status" aria-live="polite" aria-label={t("补全")}>
        {menu.loading && !menu.open && <div className="menu slashmenu">
          <div className="mnone">{t("正在加载命令与技能…")}</div>
        </div>}
      </div>
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
                    <ImagePreview src={c.url} name={c.name}
                      className="thumb"
                    >
                      <span aria-hidden="true" style={c.url ? { backgroundImage: `url(${c.url})` } : undefined} />
                    </ImagePreview>
                  ) : (
                    <span className="glyph" aria-hidden="true">{kindOf(c.a?.path ?? c.name)}</span>
                  )}
                  <span className="meta">
                    <span className="nm" title={c.a?.path ?? c.name}>{c.name}</span>
                    {c.state === "adding" && <span className="sz live">{t("正在添加…")}</span>}
                    {c.state === "ready" && <span className="sz">{isPicture(c) ? t("图片") : t("文件")}</span>}
                    {c.state === "failed" && c.error && <span className="why" title={c.error}>{c.error}</span>}
                    {c.state === "failed" && (
        <button
                        className="retry"
                        data-action="session.attach"
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
              ) : c.k === "session" ? (
                <>
                  <span className="glyph" aria-hidden="true">{"#"}</span>
                  <span className="meta">
                    <span className="nm" title={c.title}>{c.title}</span>
                    <span className="sz">{t("历史会话 · 只读资料")}</span>
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
                    {c.k === "paste" && <PasteKeep chip={c} onKeep={keepPaste} />}
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
          data-action-keydown="session.send" data-window-keys=""
          ref={box}
          rows={1}
          value={text}
          data-chips={chips.list.length > 0 ? "" : undefined}
          placeholder={placeholder}
          role="combobox"
          aria-label={t("任务输入")}
          aria-describedby={guide}
          aria-keyshortcuts={sendAria(sendShortcut, touch)}
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
          onKeyUp={(e) => setCaret(chips.snap(e.currentTarget, caret))}
          onClick={(e) => setCaret(chips.snap(e.currentTarget, caret))}
          data-action-paste="session.attach"
          // Clipboard files are copied into host-owned attachments. Their native
          // paths can belong to a source application's restricted or short-lived cache.
          onPaste={(e) => {
            if (submitting) {
              e.preventDefault();
              return;
            }
            const files = [...e.clipboardData.files];
            if (files.length > 0) {
              e.preventDefault();
              attach(files.map((file) => new File([file], file.name, { type: file.type })));
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
            const sendKey = sendsOnKey(e, sendShortcut, touch);
            if (menu.open && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
              e.preventDefault();
              menu.move(e.key === "ArrowDown" ? 1 : -1);
              return;
            }
            // Tab descends into directories; Enter confirms the highlighted reference.
            // The configured modifier chord sends the draft even with a menu open.
            if (menu.open && (e.key === "Tab" || (e.key === "Enter" && !menu.typedChosen && !(sendShortcut === "modifier_enter" && sendKey))) && !e.shiftKey) {
              e.preventDefault();
              menu.accept(undefined, e.key === "Tab" ? "descend" : "confirm");
              return;
            }
            // Esc closes the menu and stops there: reaching the app would cancel
            // the running turn, which is not what dismissing a menu means.
            if ((menu.open || menu.loading) && e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              menu.dismiss();
              return;
            }
            if (sendKey) {
              e.preventDefault();
              send();
            }
          }}
        />
        <ChipMirror box={box} text={text} chips={chips.list} />
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
                : sendHint(sendShortcut, running, touch)}
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
          {onSide && <SideAskButton open={side} onToggle={onSide} panel={sidePanel} />}
          {refine.button}
        <div className="studio-mode-control">
          <Picker
            className="mode studio-mode-picker"
            triggerAction="plan.mode"
            ariaPressed={status?.plan ?? false}
            place="bottom"
            title={t("工作模式")}
            menuClassName="composer-menu"
            current={status?.plan ? "plan" : "agent"}
            pending={busy["plan"]}
            items={[
              { value: "__mode", label: t("工作模式"), right: t("当前任务"), header: true },
              { value: "agent", label: "Agent", desc: t("执行任务并使用已启用工具") },
              { value: "plan", label: "Plan", desc: t("先整理步骤，不写入文件") },
              { value: "ask", label: t("Ask（未实现）"), desc: t("直接回答，不调用工具"), disabled: true },
            ]}
            onPick={(value) => change("plan", () => port.setPlanMode(value === "plan"))}
            label={<><StudioIcon name="agent" /><span className="studio-sr-label">{t("计划")}</span><span>{status?.plan ? "Plan" : "Agent"}</span><StudioIcon name="down" /></>}
          />
        </div>
        {git?.repo && (worktree.sources || status?.workspaceRoot) ? <WorktreeChip port={port} onChanged={onChanged} onSwitched={onTreeChanged} onError={onError} changeCount={countElsewhere ? 0 : changeCount} sources={worktree.sources ?? { available: false, sourceRoot: status?.workspaceRoot ?? "", sources: [] }} canCreate={fresh && !!worktree.sources?.available} selection={worktree.selection} branch={git.branch} busy={submitting || running} onSelect={worktree.setSelection} refresh={worktree.refresh} /> : <BranchChip port={port} git={git} changeCount={changeCount} countElsewhere={countElsewhere} onChanged={onChanged} onSwitched={onTreeChanged} onError={onError} />}
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
        <div className="studio-model-group">
          <Picker
            wrapClassName="studio-model-control"
            className="mode model-picker"
            data-action="model.select"
            place="bottom"
            title={modelTooltip(models, status?.modelRef, modelLb)}
            current={status?.modelRef}
            items={narrowToGroups(menuItems, chosenModelGroups)}
            head={<ModelGroupChips headings={groupHeadings(menuItems)} chosen={chosenModelGroups} onChange={writeModelFilter} />}
            menuClassName="studio-model-menu composer-menu"
            searchAlways
            searchPlaceholder={t("搜索模型或服务商…")}
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
                menuClassName="studio-effort-menu composer-menu"
                onOpen={loadModels}
                title={t("推理强度")}
                current={declared ? status.effort || "auto" : ""}
                pending={busy["effort"] || busy["mode"]}
                items={effortMenu(efforts, modelLb, "__effort-declare", status.modes, forcedThinking)}
                onPick={(value) => routeEffortPick(value, status.modes, {
                  declare: () => onSettings("providers:effort-declare"),
                  effort: (level) => change("effort", () => port.setEffort(level)),
                  mode: (mode) => change("mode", () => port.setModelMode(mode)),
                })}
                label={<><span>{declared ? effortReading(status.effort, status.modes) : t("未声明")}</span><StudioIcon name="down" /></>}
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
                void port.cancel().then(onChanged, (e: unknown) => {
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
          {(!running || hasDraft) && <button
            className="btn send"
            data-primary={running ? undefined : ""}
            data-action="session.send"
            data-running={running ? "" : undefined}
            data-delivering={sendAction(running, status?.busy) === "queue" ? "" : undefined}
            disabled={sendDisabled}
            aria-busy={submitting}
            onClick={send}
          >
            <span className="ic" aria-hidden="true">
              <svg viewBox="0 0 16 16">
                <path d="M8 12.8V3.4M4.2 7.2 8 3.4l3.8 3.8" />
              </svg>
            </span>
            <span>{sendLabel(submitting, running, status?.busy)}</span>
          </button>}
        </span>
      </div>
    </div></>
  );
}
