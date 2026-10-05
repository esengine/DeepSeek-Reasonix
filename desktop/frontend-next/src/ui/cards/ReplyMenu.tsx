import { useCallback, useRef, type FocusEvent, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useDismiss } from "../dismiss";
import { beside, pinToViewport, useFollow, zoom } from "../place";

// Gap between the trigger and the list, in the space a rect reports.
const GAP = 5;

const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** The list one of a finished reply's action-row buttons opens. It is drawn
 *  into the body because the card paints under containment inside a scroller,
 *  and either one cuts off a list positioned within it. `anchor` is the
 *  trigger, which stays in the row. */
export function ReplyMenu({
  anchor,
  open,
  onClose,
  children,
}: {
  anchor: RefObject<HTMLButtonElement | null>;
  open: boolean;
  onClose: () => void;
  children: ReactNode;
}) {
  const pop = useRef<HTMLDivElement>(null);
  const entry = useRef<HTMLSpanElement>(null);

  // A list that closes while it holds the focus hands it back to its trigger,
  // or the caret drops to the document and the next Tab starts from the top.
  const close = useCallback(() => {
    if (pop.current?.contains(document.activeElement)) anchor.current?.focus();
    onClose();
  }, [anchor, onClose]);
  useDismiss(open, anchor, close, pop);

  // Above the row when the whole list fits there, below it otherwise. The layout
  // height, not the painted one, so the entrance animation cannot skew it.
  const place = useCallback(() => {
    const el = pop.current;
    const to = anchor.current?.getBoundingClientRect();
    if (!el || !to) return;
    pinToViewport(el, to.left, beside(to, el.offsetHeight * zoom(), innerHeight, "above", GAP));
  }, [anchor]);
  useFollow(open, place);

  // The list left the row's DOM, so the row keeps a stop where it was: Tab from
  // the trigger enters at the first item, Shift+Tab from the next control at
  // the last, and walking off either end lands where the row would have.
  const rows = () => [...(pop.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])];
  const enter = (e: FocusEvent) => {
    const from = e.relatedTarget;
    const back = from instanceof Node && !!entry.current && !!(entry.current.compareDocumentPosition(from) & Node.DOCUMENT_POSITION_FOLLOWING);
    const all = rows();
    (back ? all[all.length - 1] : all[0])?.focus();
  };
  const leave = () => {
    const order = [...document.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => !pop.current?.contains(el));
    const next = entry.current ? order[order.indexOf(entry.current) + 1] : undefined;
    (next ?? anchor.current)?.focus();
  };

  if (!open) return null;
  return (
    <>
      <span className="sr-only" tabIndex={0} ref={entry} onFocus={enter} />
      {createPortal(
        <div className="acts-pop" role="menu" ref={pop}>
          <span className="sr-only" tabIndex={0} onFocus={() => anchor.current?.focus()} />
          {children}
          <span className="sr-only" tabIndex={0} onFocus={leave} />
        </div>,
        document.body,
      )}
    </>
  );
}
