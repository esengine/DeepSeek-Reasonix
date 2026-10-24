import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, Completion, CompletionItem } from "../port/port";
import { chipKind } from "./skillchips";

const EMPTY: Completion = { kind: "", from: 0, to: 0, items: [] };

// What the right-hand column says a row is. A file and a command look nothing
// alike in what they do, and the menu is the only place that difference shows.
const KIND: Record<string, string> = {
  builtin: "内置",
  command: "命令",
  skill: "技能",
  subagent: "子代理",
  prompt: "MCP",
  file: "文件",
  dir: "目录",
  resource: "资源",
  session: "会话",
};

const HINT: Record<string, string> = {
  ref: "↑↓ 选择 · 回车引用 · Tab 补全或进入目录 · Esc 关闭",
  session: "↑↓ 选择 · 回车添加到这条消息 · Esc 关闭",
  slash: "↑↓ 选择 · Tab 或回车补全 · Esc 关闭",
  "slash-arg": "↑↓ 选择 · Tab 或回车补全 · Esc 关闭",
};

// Asking the kernel costs one local round trip, so the gate is only about not
// asking at all while ordinary prose is being typed. It is deliberately looser
// than the real grammar — '@' mid-word and '/' mid-line are both rejected on
// the other side — because a false yes costs a request and a false no costs a
// menu that never opens.
function mightComplete(text: string): boolean {
  return text.startsWith("/") || text.includes("@") || text.includes("#");
}

// A "/" opening a word past the start of the line can only become a chip. The
// kernel reads a slash at the start alone, so the word is asked about as if it
// opened the line, and only what a sentence can hold comes back.
function midSlash(text: string, caret: number): { from: number; to: number; token: string } | null {
  const from = text.slice(0, caret).search(/\/\S*$/);
  if (from <= 0 || !/\s/.test(text[from - 1])) return null;
  const rest = text.slice(from).search(/\s/);
  return { from, to: rest < 0 ? text.length : from + rest, token: text.slice(from, caret) };
}

// Whether the caret rests in a word that opens with "/" at the line start or
// after a space: the one place a person is waiting for the command list. Prose
// typed after a finished command still asks (an argument may complete) but is
// not waiting, so it never announces loading.
function inSlashWord(text: string, caret: number): boolean {
  return /(^|\s)\/\S*$/.test(text.slice(0, caret));
}

// The chips a menu pick can become, and whether the caret rests on one: a chip
// is finished, so the letters it is spelled with are not a word to complete.
interface ChipHost {
  place: (text: string, c: Completion, item: CompletionItem) => { text: string; caret: number } | null;
  touches: (caret: number) => boolean;
}

function ask(port: AgentPort, text: string, caret: number, mid: ReturnType<typeof midSlash>, signal: AbortSignal): Promise<Completion> {
  if (!mid) return port.complete(text, caret, signal).then(withTyped);
  return port.complete(mid.token, mid.token.length, signal).then((r) => ({
    kind: "slash",
    from: mid.from,
    to: mid.to,
    query: r.query,
    items: (r.items ?? []).filter((it) => chipKind(it) !== null),
  }));
}

function withTyped(r: Completion): Completion {
  return r.typed && r.items?.length ? { ...r, items: [r.typed, ...r.items] } : r;
}

interface State {
  completion: Completion;
  active: number;
  open: boolean;
  // The line already spells the highlighted command in full.
  typedChosen: boolean;
  loading: boolean;
  move: (delta: number) => void;
  hover: (i: number) => void;
  dismiss: () => void;
  accept: (item?: CompletionItem, mode?: "confirm" | "descend") => void;
  // Whether the keys still hold the highlight. A menu opens under whatever the
  // pointer happens to be resting on, so the pointer only takes it by moving.
  kb: boolean;
}

// useCompletion keeps the menu in step with the caret. Every decision about
// what a token means — where it starts, what may replace it, how a path with
// spaces is escaped — comes from the kernel, so this hook only asks, renders,
// and splices the answer back in.
export function useCompletion(
  port: AgentPort,
  text: string,
  caret: number,
  apply: (text: string, caret: number) => void,
  chips?: ChipHost,
  onSession?: (picked: { id: string; title: string }) => void,
): State {
  const [{ completion, active, kb }, setMenu] = useState({ completion: EMPTY, active: 0, kb: true });
  // An answer and its default highlight publish together; a deferred reset can
  // overwrite navigation after the rows are already visible.
  const setCompletion = useCallback((completion: Completion) => {
    setMenu({ completion, active: 0, kb: true });
  }, []);
  const [loading, setLoading] = useState(false);
  const currentCompletion = useRef(completion);
  currentCompletion.current = completion;
  // The answer that was dismissed, not a flag: a reset run as an effect lands
  // after the commit that opened the menu, and a blur between the two is undone.
  const [dismissed, setDismissed] = useState<Completion | null>(null);
  // Answers can land out of order; only the newest question still has an answer
  // worth showing.
  const asked = useRef(0);

  useEffect(() => {
    const id = ++asked.current;
    const onChip = chips?.touches(caret) ?? false;
    const mid = onChip ? null : midSlash(text, caret);
    if (onChip || (!mid && !mightComplete(text))) {
      setCompletion(EMPTY);
      setLoading(false);
      return;
    }
    const previous = currentCompletion.current;
    const slashy = mid !== null || text.startsWith("/");
    const compatible = slashy
      ? previous.kind === "slash" || previous.kind === "slash-arg"
      : previous.kind === "ref" || previous.kind === "session";
    if (!compatible) setCompletion(EMPTY);
    setLoading(false);
    const timer = inSlashWord(text, caret) && !compatible
      ? window.setTimeout(() => { if (id === asked.current) setLoading(true); }, 180)
      : undefined;
    const asking = new AbortController();
    ask(port, text, caret, mid, asking.signal)
      .then((r) => {
        if (id === asked.current) {
          window.clearTimeout(timer);
          setCompletion(r.items?.length ? r : EMPTY);
          setLoading(false);
        }
      })
      .catch(() => {
        if (id === asked.current) {
          window.clearTimeout(timer);
          setCompletion(EMPTY);
          setLoading(false);
        }
      });
    return () => {
      window.clearTimeout(timer);
      asking.abort();
    };
  }, [port, text, caret]);

  const items = completion.items;
  const open = dismissed !== completion && items.length > 0;
  const at = Math.min(active, items.length - 1);

  const accept = useCallback(
    (item?: CompletionItem, mode: "confirm" | "descend" = "confirm") => {
      const pick = item ?? items[Math.min(active, items.length - 1)];
      if (!pick) return;
      if (completion.kind === "session" && pick.kind === "session") {
        onSession?.({ id: pick.insert.slice("@session:".length), title: pick.label });
        asked.current++;
        setCompletion(EMPTY);
        setLoading(false);
        return apply(text.slice(0, completion.from) + text.slice(completion.to).replace(/^ /, ""), completion.from);
      }
      const placed = chips?.place(text, completion, pick);
      if (placed) return apply(placed.text, placed.caret);
      const finishRef = completion.kind === "ref" && !(mode === "descend" && pick.descend);
      const suffix = text.slice(completion.to);
      const separator = finishRef && !/^\s/.test(suffix) ? " " : "";
      const next = text.slice(0, completion.from) + pick.insert + separator + suffix;
      if (completion.kind === "ref") {
        asked.current++;
        setCompletion(EMPTY);
        setLoading(false);
      }
      apply(next, completion.from + pick.insert.length + (finishRef ? 1 : 0));
    },
    [items, active, text, completion, apply, chips, onSession],
  );

  // A line that already says what the highlighted row would make it: Enter
  // sends it rather than completing it again.
  const typedChosen = completion.kind === "slash" && completion.typed !== undefined && items[at]?.insert === text;

  return {
    completion,
    active: at,
    open,
    typedChosen,
    loading,
    kb,
    move: (delta) => {
      setMenu((menu) => {
        const count = menu.completion.items.length;
        if (!count) return menu;
        return { ...menu, kb: true, active: (((Math.min(menu.active, count - 1) + delta) % count) + count) % count };
      });
    },
    hover: (i) => {
      setMenu((menu) => ({ ...menu, kb: false, active: i }));
    },
    dismiss: () => {
      asked.current++;
      setDismissed(completion);
      setLoading(false);
    },
    accept,
  };
}

// Where the query landed in the label. A fuzzy hit that cannot point at the
// letters it matched reads as the menu ignoring what was typed.
function split(label: string, query: string): [string, string, string] {
  if (!query) return [label, "", ""];
  const i = label.toLowerCase().indexOf(query.toLowerCase());
  if (i < 0) return [label, "", ""];
  return [label.slice(0, i), label.slice(i, i + query.length), label.slice(i + query.length)];
}

interface Props {
  id: string;
  items: CompletionItem[];
  active: number;
  kind: string;
  query: string;
  kb: boolean;
  onPick: (item: CompletionItem) => void;
  onHover: (i: number) => void;
}

export function CompletionMenu({ id, items, active, kind, query, kb, onPick, onHover }: Props) {
  const on = useRef<HTMLButtonElement>(null);

  // Arrow keys walk past the eight rows that fit; the list has to follow, or
  // the selection is somewhere below the fold and the menu looks frozen.
  // The key hints sit outside this scrollport on purpose: as a sticky footer
  // inside it they covered the last row while "nearest" still counted it as
  // visible, so the first press past the fold scrolled nothing.
  useEffect(() => {
    on.current?.scrollIntoView({ block: "nearest" });
  }, [active, items]);

  return (
    // Keyed on the kind so switching between completing a command and
    // completing a path replays the open animation: the menu is answering a
    // different question, and swapping the rows in place hides that.
    <div className="menu slashmenu" key={kind}>
      <div
        className="mlist"
        id={id}
        role="listbox"
        aria-label={t("补全")}
        data-kb={kb ? "" : undefined}
      >
        {items.map((it, i) => {
          const [before, hit, after] = split(it.label, query);
          return (
            <button
              key={(it.kind ?? "") + ":" + it.insert}
              id={`${id}-${i}`}
              ref={i === active ? on : undefined}
              className="mi"
              role="option"
              aria-selected={i === active}
              data-on={i === active ? "" : undefined}
              tabIndex={-1}
              onMouseMove={() => onHover(i)}
              // mousedown, not click: the textarea must not lose focus first.
              onMouseDown={(e) => {
                e.preventDefault();
                onPick(it);
              }}
            >
              <span className="dot" />
              <span className="tx">
                <span className="lb">
                  {before}
                  {hit && <b className="hit">{hit}</b>}
                  {after}
                  {it.descend && <i className="ah" aria-hidden="true">›</i>}
                </span>
                {it.hint && <span className="ds">{it.hint}</span>}
                {it.args?.map((a) => (
                  <span className="ds" key={a.name}>
                    {a.name} {t(a.required ? "必填" : "可选")}
                    {a.description ? ` · ${a.description}` : ""}
                  </span>
                ))}
                {it.moreArgs ? <span className="ds">{t("还有 {n} 项", { n: it.moreArgs })}</span> : null}
              </span>
              {it.kind && <span className="rt">{t(KIND[it.kind] ?? it.kind)}</span>}
            </button>
          );
        })}
      </div>
      <div className="mkeys">{t(HINT[kind] ?? HINT.slash)}</div>
    </div>
  );
}
