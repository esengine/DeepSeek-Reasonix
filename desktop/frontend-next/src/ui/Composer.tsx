import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, ApprovalMode, ModelEntry, SessionStatus, Attachment } from "../port/port";
import { Picker } from "./Menu";
import { modelMenu } from "./modelmenu";
import { CompletionMenu, useCompletion } from "./Completion";
import { useIme } from "./ime";
import { countLines, pasteIsLong, planTone, planVerb } from "./intake";
import { useIntake } from "./useIntake";
import type { Dropped } from "./filedrop";

// 从紧到松排，和闸门环的缺口一个方向。「不打扰」在内核是 permission.Deny：
// 它比「询问」更严，不是更松 —— 排在询问前面才不会被读反。
const APPROVALS: [ApprovalMode, string, string][] = [
  ["dontAsk", "不打扰", "不弹审批；要批准才能做的一概不做。"],
  ["ask", "询问", "每次动手前问你。"],
  ["auto", "自动", "低风险自己过，写操作仍然问。"],
  ["yolo", "全放行", "不问了。只在你完全信任这个工作区时用。"],
];

// Only the ladder the kernel would accept for the model in hand. A fixed list
// here offered rungs a given model does not have, and picking one looked like
// the control was dead: the request was refused downstream, with nothing on the
// composer to say so. The kernel's list already opens with "auto" — prepending
// another one put the same rung in the menu twice. The fallback is for a model
// that declares nothing at all, and only then.
const EFFORT_FALLBACK = ["auto", "low", "medium", "high", "xhigh", "max"];

function effortsFor(models: ModelEntry[], ref?: string): string[] {
  const model = models.find((m) => m.ref === ref);
  // A model that is in the list and names no levels has none: the host omits
  // the field exactly when it would refuse every level but auto. Filling that
  // in from the fallback is what put a full ladder in front of a relay whose
  // every rung came back refused. The fallback is for a list not answered yet.
  if (model) return model.efforts ?? [];
  return EFFORT_FALLBACK;
}

// 强度是有序的，批准是有序的 —— 一排全等的胶囊把这件事藏了起来。两个刻度把
// 它画回来：几格电平表示这一轮想得多深，环的缺口表示闸门开了多大。
const LEVEL: Record<string, number> = { auto: 0, low: 1, medium: 2, high: 3, xhigh: 4, max: 5 };
const GATE: Record<ApprovalMode, number> = { dontAsk: 0, ask: 1, auto: 2, yolo: 3 };

interface Props {
  port: AgentPort;
  status: SessionStatus | null;
  running: boolean;
  // Resolves false when the line never left, so what was typed comes back
  // rather than being lost to a refusal the user could not have prevented.
  // Bumped when something outside asks for the cursor — answering a plan card
  // with "revise" is a request to say what to change, and the saying happens here.
  focus?: number;
  onSubmit: (text: string) => Promise<boolean>;
  onChanged: () => void;
  onError: (e: unknown) => void;
}

// What is riding along with this turn. An image travels as bytes the kernel
// already saved; a long paste is the text itself, held out of the box so eight
// hundred lines of log do not become the composer.
type Chip =
  | { k: "image"; id: string; a: Attachment; url?: string }
  | { k: "paste"; id: string; body: string; lines: number; name?: string };

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
  const name = path.split(/[\\/]/).pop() ?? path;
  const dot = name.lastIndexOf(".");
  return dot > 0 ? name.slice(dot + 1).toUpperCase().slice(0, 4) : "FILE";
}

function releaseChip(c: Chip) {
  if (c.k === "image" && c.url) URL.revokeObjectURL(c.url);
}

let chipSeq = 0;
const chipId = () => `c${++chipSeq}`;

export function Composer({ port, status, running, focus, onSubmit, onChanged, onError }: Props) {
  const [text, setText] = useState("");
  // The caret decides which token is being completed, so it is state here
  // rather than something read off the element when a menu happens to open.
  const [caret, setCaret] = useState(0);
  const [shots, setShots] = useState<Chip[]>([]);
  const picker = useRef<HTMLInputElement>(null);
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [switching, setSwitching] = useState(false);
  const box = useRef<HTMLTextAreaElement>(null);
  // Set only when a completion moved the caret: the browser puts it at the end
  // of a programmatic value, which is wrong for anything accepted mid-line.
  const pending = useRef<number | null>(null);

  useEffect(() => {
    port.models().then(setModels).catch(() => setModels([]));
  }, [port]);

  // A drop lands where the caret is, and the handler that receives it was built
  // once — so the position it reads has to be a ref, not the render's copy.
  const caretRef = useRef(0);
  const type = (next: string, at: number) => {
    setText(next);
    setCaret(at);
    caretRef.current = at;
  };

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

  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    if (pending.current !== null) {
      el.setSelectionRange(pending.current, pending.current);
      pending.current = null;
    }
    // CSS caps the top at five lines; the element still has to be told to grow.
    // The floor is not decoration: under an interface zoom, scrollHeight is not
    // in the same units the height we write back is, and the two engines do not
    // round it the same way. Writing a smaller number than one line squeezes the
    // box shut — an empty composer with both scrollbars showing and nowhere to
    // type. One line is the least it can ever legitimately be.
    const line = parseFloat(getComputedStyle(el).lineHeight) || 22;
    el.style.height = "auto";
    el.style.height = `${Math.max(line, el.scrollHeight)}px`;
  }, [text]);

  // Attachments ride into the turn as path references, exactly as they do from
  // the CLI — the host saved the bytes, the turn parser resolves the token. A
  // held-back paste follows the typed text, which is where it reads as the
  // material the message is about rather than as part of the sentence.
  const send = () => {
    const v = text.trim();
    if (!v && shots.length === 0) return;
    const refs = shots.flatMap((c) => (c.k === "image" ? [c.a.ref] : []));
    const pastes = shots.flatMap((c) => (c.k === "paste" ? [c.body] : []));
    const line = [[...refs, v].filter(Boolean).join(" "), ...pastes].filter(Boolean).join("\n\n");
    type("", 0);
    shots.forEach(releaseChip);
    setShots([]);
    void onSubmit(line).then((sent) => {
      // The chips are gone with their object URLs, but line still carries their
      // refs as text — putting it back returns everything the turn was made of.
      if (!sent) type(line, line.length);
    });
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

  const attach = useCallback(
    (blobs: File[]) => {
      if (blobs.length === 0) return;
      Promise.all(blobs.map((b) => port.attach(b, b.name).then((a) => ({ a, url: previewURL(b) }))))
        .then((added) =>
          setShots((prev) => [...prev, ...added.map(({ a, url }) => ({ k: "image" as const, id: chipId(), a, url }))]),
        )
        .catch(onError);
    },
    [port, onError],
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
              ? [{ k: "image" as const, id: chipId(), a: { path: r.path ?? "", ref: r.ref, image: r.image } }]
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

  const readIn = useCallback(
    (files: File[]) => {
      for (const file of files) {
        void file
          .text()
          .then((body) => {
            if (!body.trim()) return;
            setShots((prev) => [
              ...prev,
              { k: "paste", id: chipId(), body, lines: countLines(body), name: file.name },
            ]);
          })
          .catch(onError);
      }
    },
    [onError],
  );

  // One place decides what a payload becomes, whichever channel carried it.
  const receive = useCallback(
    (d: Dropped) => {
      if (d.paths.length > 0) refIn(d.paths);
      else if (d.files.length > 0) {
        const pictures = d.files.filter((f) => f.type.startsWith("image/"));
        attach(pictures);
        // Bytes that are not pixels have nowhere else to go: the kernel's
        // attachment store keeps images only, and there is no path to point at.
        // Reading them as text is the one answer left, and a good one for a
        // dropped log.
        readIn(d.files.filter((f) => !pictures.includes(f)));
      }
      if (d.text) insert(d.text + " ");
      // A drop is the start of typing, not the end of it.
      box.current?.focus();
    },
    [refIn, attach, readIn, insert],
  );

  const { plan: drag, ref: dropzone } = useIntake({
    root: status?.workspaceRoot ?? status?.cwd,
    onReceive: receive,
  });

  const apv = status?.toolApprovalMode ?? "ask";
  const eff = status?.effort || "auto";
  const efforts = effortsFor(models, status?.modelRef);
  const modelLb = status?.modelRef?.split("/").pop() ?? status?.label ?? "—";
  // Every one of these rebuilds the runtime kernel-side (~0.4s on a real
  // session). Without a pending state the click reads as a dead control.
  const change = (p: Promise<void>) => {
    setSwitching(true);
    void p.then(onChanged).catch(onError).finally(() => setSwitching(false));
  };

  return (
    // display:contents, so the box looks exactly as it did and the drop layer
    // still has one node to ask which pane a drop landed in.
    <div className="dropzone" ref={dropzone}>
      {menu.open && (
        <CompletionMenu
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
      {shots.length > 0 && (
        <ul className="shots">
          {shots.map((c, i) => (
            <li className="shot" key={c.id} style={{ "--i": i } as React.CSSProperties}>
              {c.k === "image" ? (
                <>
                  {c.a.image ? (
                    <span
                      className="thumb"
                      aria-hidden="true"
                      style={c.url ? { backgroundImage: `url(${c.url})` } : undefined}
                    />
                  ) : (
                    <span className="glyph" aria-hidden="true">{kindOf(c.a.path)}</span>
                  )}
                  <span className="meta">
                    <span className="nm" title={c.a.path}>{c.a.path.split("/").pop()}</span>
                    <span className="sz">{c.a.image ? t("图片") : t("文件")}</span>
                  </span>
                </>
              ) : (
                <>
                  <span className="glyph" aria-hidden="true">TXT</span>
                  <span className="meta">
                    <span className="nm">{c.name ?? t("粘贴的文本")}</span>
                    <button
                      className="undo"
                      onClick={() => {
                        discard(c);
                        insert(c.body);
                      }}
                    >
                      {t("{n} 行 · 展开到输入框", { n: c.lines })}
                    </button>
                  </span>
                </>
              )}
              <button
                className="x"
                aria-label={t("移除")}
                onClick={() => discard(c)}
              >
                ×
              </button>
            </li>
          ))}
          {/* The kernel keeps the image either way, but a text-only model never
              sees it — say so here rather than letting the paste vanish. */}
          {status?.vision === false && shots.some((c) => c.k === "image" && c.a.image !== false) && (
            <li className="warn">
              {status?.visionDeclared === false
                ? t("没人说过这个模型读不读图 · 先按不读处理；在「连接」里勾上它就直接发")
                : t("当前模型不读图 · 将交给能读图的子代理")}
            </li>
          )}
        </ul>
      )}
      {/* The prompt glyph and the count sit beside the box rather than in it:
          both answer something about the text, and neither may be dragged into
          a selection of it. */}
      <div className="fmain">
        <span className="prompt" aria-hidden="true">
          ›
        </span>
        <textarea
          data-action-keydown="session.send"
          ref={box}
          rows={1}
          value={text}
          placeholder={t("描述一个任务，回车发送…　/ 调用命令与技能，@ 引用文件")}
          role="combobox"
          aria-expanded={menu.open}
          aria-controls="slashmenu"
          aria-autocomplete="list"
          aria-activedescendant={menu.open ? `slash-${menu.active}` : undefined}
          onChange={(e) => type(e.target.value, e.target.selectionStart)}
          // Arrow keys and clicks move the caret without changing the text, and
          // the caret is what decides which token the menu is completing.
          onKeyUp={(e) => setCaret(e.currentTarget.selectionStart)}
          onClick={(e) => setCaret(e.currentTarget.selectionStart)}
          // Dropping is the pane's job — a one-row box is 40px to aim at, and the
          // handler that used to live here prevented the default and then did
          // nothing with text, which is worse than not handling it at all.
          onPaste={(e) => {
            const images = [...e.clipboardData.files].filter((f) => f.type.startsWith("image/"));
            if (images.length > 0) {
              e.preventDefault();
              attach(images);
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
            if (e.key === "Tab" && e.shiftKey) {
              e.preventDefault();
              change(port.setPlanMode(!status?.plan));
            }
          }}
        />
        <span className="fcount" aria-hidden="true">
          {text.length || ""}
        </span>
      </div>
      <div className="row" data-busy={switching ? "" : undefined}>
        {/* 拖进来和粘贴都走同一条路，但那两个都得先有一张图在手边。点开系统
            选择器是唯一不需要预备动作的入口。 */}
        <input
          ref={picker}
          type="file"
          accept="image/*"
          multiple
          hidden
          onChange={(e) => {
            attach([...(e.target.files ?? [])].filter((f) => f.type.startsWith("image/")));
            // 同一张图再选一次也要能进来，所以每次用完清空。
            e.target.value = "";
          }}
        />
        <button
          className="mode plain attach"
          title={t("添加图片　也可以直接拖进来或粘贴")}
          aria-label={t("添加图片")}
          onClick={() => picker.current?.click()}
        >
          <span className="ic" aria-hidden="true">
            <svg viewBox="0 0 16 16">
              <path d="M8 4.3v7.4M4.3 8h7.4" />
            </svg>
          </span>
        </button>
        {/* The one control on this shelf with no width of its own: a gateway
            can publish an id longer than the shelf is wide. It gives up
            characters before the shelf gives up a line, and the ref it was
            shortened from stays readable on hover. */}
        <Picker
          className="mode"
          place="bottom"
          title={status?.modelRef ?? modelLb}
          current={status?.modelRef}
          items={modelMenu(models)}
          onPick={(ref) => change(port.setModel(ref))}
          label={
            <>
              <span className="dot" aria-hidden="true" />
              <span className="nm">{modelLb}</span>
            </>
          }
        />
        {/* An endpoint that names no levels gets no control: a picker whose
            every rung is refused downstream reads as a dead one. */}
        {efforts.length > 0 && (
          <>
            <span className="sep" aria-hidden="true" />
            <Picker
              className="mode plain"
              place="bottom"
              current={eff}
              items={efforts.map((v) => ({ value: v, label: v, meter: LEVEL[v] }))}
              onPick={(v) => change(port.setEffort(v))}
              label={
                <>
                  <span className="bars" data-lv={LEVEL[eff] ?? 0} aria-hidden="true">
                    <i /><i /><i /><i /><i />
                  </span>
                  <span className="lb">{t("强度")}</span>
                  {/* key 让值换一次就重挂载一次 —— 这是那半秒里唯一能看出「改动生效了」的地方 */}
                  <span className="vl" data-lv={LEVEL[eff] ?? 0} key={eff}>{eff}</span>
                </>
              }
            />
          </>
        )}
        <button
          className="mode tog"
          data-action="plan.mode"
          aria-pressed={status?.plan ?? false}
          onClick={() => change(port.setPlanMode(!status?.plan))}
        >
          <span className="ic" aria-hidden="true">
            <svg viewBox="0 0 16 16">
              <path pathLength={1} d="M2.9 5.1 4.2 6.4l2.2-2.4M8 5.1h5.2M2.9 10.6l1.3 1.3 2.2-2.4M8 10.6h5.2" />
            </svg>
          </span>
          <span className="lb">{t("计划")}</span>
        </button>
        {/* The toggle keeps its legacy meaning: it follows `plan`, which the
            kernel turns off the moment a plan is approved. The lifecycle is a
            separate reading — an approved plan is still running, and saying so
            here is not the same as offering to turn planning back on. */}
        {status?.planPhase === "executing" && (
          <span className="mode plain" data-plan-phase="executing" title={t("正在执行已批准的计划")}>
            <span className="lb">{t("执行计划中")}</span>
          </span>
        )}
        <Picker
          className={apv === "yolo" ? "mode plain danger" : "mode plain"}
          place="bottom"
          current={apv}
          items={APPROVALS.map(([v, lb, ds]) => ({ value: v, label: t(lb), desc: t(ds) }))}
          onPick={(v) => change(port.setApprovalMode(v as ApprovalMode))}
          label={
            <>
              <span className="gate" data-g={GATE[apv]} aria-hidden="true">
                <svg viewBox="0 0 16 16">
                  <circle cx="8" cy="8" r="4.4" pathLength={1} />
                  {/* 闭合的环只说明「没开口」；一律不做还要再画一杠 */}
                  <path className="bar" d="M5.4 8h5.2" />
                </svg>
              </span>
              <span className="lb">{t("批准")}</span>
              <span className="vl" data-g={GATE[apv]} key={apv}>
                {t(APPROVALS.find(([m]) => m === apv)?.[1] ?? "")}
              </span>
            </>
          }
        />
        <span className="go">
          <button
            className="btn send"
            data-primary
            data-action={running ? "session.stop" : "session.send"}
            onClick={() => (running ? change(port.cancel()) : send())}
          >
            <span className="ic" aria-hidden="true">
              <svg className="i-send" viewBox="0 0 16 16">
                <path d="M2.8 8h9.4M8.4 4.2 12.2 8l-3.8 3.8" />
              </svg>
              <svg className="i-stop" viewBox="0 0 16 16">
                <rect x="4.8" y="4.8" width="6.4" height="6.4" rx="1.3" />
              </svg>
            </span>
            <span>{t(running ? "停下" : "发送")}</span>
          </button>
        </span>
      </div>
    </div>
  );
}
