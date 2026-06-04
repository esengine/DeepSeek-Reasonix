import { memo, useCallback, useState } from "react";
import { ChevronRight } from "lucide-react";
import { Markdown } from "./Markdown";
import { CopyButton } from "./CopyButton";
import { Tooltip } from "./Tooltip";
import { useT } from "../lib/i18n";
import type { Item } from "../lib/useController";

type AssistantItem = Extract<Item, { kind: "assistant" }>;

// ── Shared constants ────────────────────────────────────────────────────────
// Pre-compiled once at module load: the regex itself is a hot allocation
// otherwise because the component renders for every user message and the
// literal would be re-instantiated on every render. Re-using the same RegExp
// instance also lets the V8 engine keep its internal bytecode warm across
// renders, so the replace pass over the user-typed text is materially
// cheaper than a fresh literal.
//
// The rewind scopes live at module scope too — Transcript re-creates the
// rewind button JSX on every render today, and a stable reference lets us
// map() without re-allocating the descriptors array.
const ATTACHMENT_TOKEN = /@\.reasonix\/attachments\/[^\s]+/g;

type RewindScope = "both" | "conversation" | "code" | "fork" | "summ-from" | "summ-upto";
const REWIND_SCOPES: { scope: RewindScope; labelKey: string }[] = [
  { scope: "both", labelKey: "rewind.both" },
  { scope: "conversation", labelKey: "rewind.conversation" },
  { scope: "code", labelKey: "rewind.code" },
  { scope: "fork", labelKey: "rewind.fork" },
  { scope: "summ-from", labelKey: "rewind.summFrom" },
  { scope: "summ-upto", labelKey: "rewind.summUpto" },
];

// ── RewindMenu ──────────────────────────────────────────────────────────────
// The rewind menu only re-renders when its `open` flag flips, so it's safe
// to lift into a tiny standalone component. Keeps UserMessage's render body
// focused on the bubble itself and gives the rewind menu a stable identity
// for any future animation or focus-management work.
function RewindMenu({
  open,
  onRewind,
  t,
}: {
  open: boolean;
  onRewind: (scope: RewindScope) => void;
  t: (key: string) => string;
}) {
  if (!open) return null;
  return (
    <div className="rewind__menu">
      {REWIND_SCOPES.map(({ scope, labelKey }) => (
        <button key={scope} onClick={() => onRewind(scope)}>
          {t(labelKey)}
        </button>
      ))}
    </div>
  );
}

// ── UserMessage ─────────────────────────────────────────────────────────────
function UserMessageImpl({
  text,
  turn,
  open,
  onToggle,
  onRewind,
}: {
  text: string;
  turn?: number;
  open?: boolean; // whether this message's rewind menu is the open one (lifted to Transcript)
  onToggle?: () => void;
  onRewind?: (turn: number, scope: string) => void;
}) {
  const t = useT();
  const canRewind = onRewind != null && turn != null;
  // Bind turn once into a stable callback; the original onRewind is
  // referentially stable across renders, so this factory only re-runs when
  // `turn` changes — and turn itself never changes for a given user message
  // (it's the ordinal among user messages, computed once in Transcript).
  const rewind = useCallback(
    (scope: RewindScope) => {
      if (onRewind && turn != null) onRewind(turn, scope);
    },
    [onRewind, turn],
  );
  // Image attachments are dropped before display (the model never needs the
  // local @.reasonix/attachments/... path, and the user already saw the
  // visual). The regex has the /g flag, so reset lastIndex defensively —
  // String.prototype.replace is stateful about /g and any future caller that
  // switches to .match() or .exec() would otherwise see a stale start index.
  ATTACHMENT_TOKEN.lastIndex = 0;
  const displayText = text.replace(ATTACHMENT_TOKEN, "[image]");
  return (
    <div className="msg msg--user">
      <span className="msg__caret">›</span>
      <div className="msg__text">{displayText}</div>
      {canRewind && (
        <div className="rewind">
          <Tooltip label={t("rewind.label")}>
            <button className="rewind__btn" onClick={onToggle}>
              ⟲
            </button>
          </Tooltip>
          <RewindMenu open={!!open} onRewind={rewind} t={t as (k: string) => string} />
        </div>
      )}
    </div>
  );
}

// memo: like AssistantMessage, an unchanged user bubble keeps a stable
// `text` ref across a streaming turn's per-token re-renders, so the whole
// backlog (which can be hundreds of messages on a long session) skips the
// `replace` pass, the i18n hook, and the JSX tree. Turn is also stable
// per-mount, so the rewind factory captures the right scope on first
// render and the memo equality check is O(props) without diving into the
// closure.
export const UserMessage = memo(UserMessageImpl);

// memo: an unchanged message keeps a stable `item` ref across a streaming turn's
// per-token re-renders, so only the live bubble re-parses markdown, not the whole
// backlog.
export const AssistantMessage = memo(function AssistantMessage({ item }: { item: AssistantItem }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  return (
    <div className="msg msg--assistant">
      {item.reasoning && (
        <div className="reasoning">
          <button className="reasoning__toggle" onClick={() => setOpen((v) => !v)}>
            <ChevronRight
              className={`reasoning__chevron ${open ? "reasoning__chevron--open" : ""}`}
              size={12}
            />
            {t("msg.thinking")}
          </button>
          {open && <div className="reasoning__body">{item.reasoning}</div>}
        </div>
      )}
      <div className="msg__body">
        {item.streaming ? (
          // While streaming, render raw text (stable, monospace-free) instead of
          // re-parsing markdown on every token — partial markdown reflows the
          // layout and makes the view jitter. Markdown renders once, on completion.
          <div className="msg__stream">
            {item.text}
            <span className="cursor" />
          </div>
        ) : (
          <Markdown text={item.text} />
        )}
      </div>
      {!item.streaming && item.text && (
        <div className="msg__actions">
          <CopyButton text={item.text} label={t("msg.copy")} />
        </div>
      )}
    </div>
  );
});
