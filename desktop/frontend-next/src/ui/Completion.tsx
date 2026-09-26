import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, Completion, CompletionItem } from "../port/port";

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
};

const HINT: Record<string, string> = {
  ref: "Tab 补全或进入目录 · ↑↓ 后回车确认 · Esc 关闭",
  slash: "↑↓ 选择 · Tab 或回车补全 · Esc 关闭",
  "slash-arg": "↑↓ 选择 · Tab 或回车补全 · Esc 关闭",
};

// Asking the kernel costs one local round trip, so the gate is only about not
// asking at all while ordinary prose is being typed. It is deliberately looser
// than the real grammar — '@' mid-word and '/' mid-line are both rejected on
// the other side — because a false yes costs a request and a false no costs a
// menu that never opens.
function mightComplete(text: string): boolean {
  return text.startsWith("/") || text.includes("@");
}

interface State {
  completion: Completion;
  active: number;
  open: boolean;
  loading: boolean;
  move: (delta: number) => void;
  hover: (i: number) => void;
  dismiss: () => void;
  accept: (item?: CompletionItem) => void;
  // Whether the keys still hold the highlight. A menu opens under whatever the
  // pointer happens to be resting on, so the pointer only takes it by moving.
  kb: boolean;
  // Whether Enter belongs to the menu. A half-typed command is not a message,
  // so Enter completes it; a reference is ordinary prose the moment it resolves,
  // so Enter sends unless the user went looking through the list.
  ownsEnter: boolean;
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
): State {
  const [completion, setCompletion] = useState<Completion>(EMPTY);
  const [loading, setLoading] = useState(false);
  const [active, setActive] = useState(0);
  const [picked, setPicked] = useState(false);
  // The answer that was dismissed, not a flag: a reset run as an effect lands
  // after the commit that opened the menu, and a blur between the two is undone.
  const [dismissed, setDismissed] = useState<Completion | null>(null);
  const [kb, setKb] = useState(true);
  // Answers can land out of order; only the newest question still has an answer
  // worth showing.
  const asked = useRef(0);

  useEffect(() => {
    const id = ++asked.current;
    if (!mightComplete(text)) {
      setCompletion(EMPTY);
      setLoading(false);
      return;
    }
    setCompletion(EMPTY);
    setLoading(text.startsWith("/"));
    port
      .complete(text, caret)
      .then((r) => {
        if (id === asked.current) {
          setCompletion(r.items?.length ? r : EMPTY);
          setLoading(false);
        }
      })
      .catch(() => {
        if (id === asked.current) {
          setCompletion(EMPTY);
          setLoading(false);
        }
      });
  }, [port, text, caret]);

  // A keystroke changed the list under the pointer, so its claim on the
  // highlight is stale even though it never moved.
  useEffect(() => {
    setActive(0);
    setPicked(false);
    setKb(true);
  }, [completion]);

  const items = completion.items;
  const open = dismissed !== completion && items.length > 0;
  const at = Math.min(active, items.length - 1);

  const accept = useCallback(
    (item?: CompletionItem) => {
      const pick = item ?? items[Math.min(active, items.length - 1)];
      if (!pick) return;
      const next = text.slice(0, completion.from) + pick.insert + text.slice(completion.to);
      apply(next, completion.from + pick.insert.length);
    },
    [items, active, text, completion, apply],
  );

  return {
    completion,
    active: at,
    open,
    loading,
    ownsEnter: open && (completion.kind !== "ref" || picked),
    kb,
    move: (delta) => {
      setPicked(true);
      setKb(true);
      setActive((i) => (((Math.min(i, items.length - 1) + delta) % items.length) + items.length) % items.length);
    },
    hover: (i) => {
      setPicked(true);
      setKb(false);
      setActive(i);
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
