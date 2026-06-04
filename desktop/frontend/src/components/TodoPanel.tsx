import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, ChevronRight, Circle, CircleDot, RefreshCw, X } from "lucide-react";
import { useT } from "../lib/i18n";
import type { Todo } from "../lib/tools";
import { Tooltip } from "./Tooltip";

// TodoPanel is the live task list pinned just above the composer — the kernel's
// latest todo_write call drives it, and it updates in place as the agent flips
// items to in_progress / completed, so the user watches the plan get worked
// through one item at a time. Collapsed, it still
// shows the current item so the footer stays compact during a long run. The ✕
// dismisses it (onDismiss) when the user abandons the task; a fresh todo_write
// brings it back.
export function TodoPanel({ todos, stale, onDismiss }: { todos: Todo[]; stale?: boolean; onDismiss: () => void }) {
  const t = useT();
  const [open, setOpen] = useState(true);
  const currentRef = useRef<HTMLLIElement | null>(null);

  const done = todos.filter((t) => t.status === "completed").length;
  const current = todos.find((t) => t.status === "in_progress");

  useEffect(() => {
    if (!open) return;
    currentRef.current?.scrollIntoView({ block: "nearest" });
  }, [open, current?.content, current?.activeForm]);

  if (todos.length === 0) return null;

  return (
    <div className="todobar">
      <div className="todobar__head">
        <button className="todobar__toggle" onClick={() => setOpen((v) => !v)}>
          {open ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
          <span className="todobar__title">{t("todo.title")}</span>
          <span className="todobar__count">
            {done}/{todos.length}
          </span>
          {stale && (
            <span className="todobar__stale">
              <RefreshCw size={11} />
              {t("todo.stale")}
            </span>
          )}
          {!open && current && (
            <span className="todobar__current">{current.activeForm || current.content}</span>
          )}
        </button>
        <Tooltip label={t("todo.dismiss")}>
          <button className="todobar__close" onClick={onDismiss}>
            <X size={13} />
          </button>
        </Tooltip>
      </div>

      {open && (
        <ul className="todobar__list">
          {todos.map((t, i) => (
            <li
              key={i}
              ref={t.status === "in_progress" ? currentRef : undefined}
              className={`todobar__item todobar__item--${t.status}${t.level ? " todobar__item--sub" : ""}`}
            >
              {t.status === "completed" ? (
                <Check size={14} className="todobar__ico todobar__ico--done" />
              ) : t.status === "in_progress" ? (
                <CircleDot size={14} className="todobar__ico todobar__ico--active" />
              ) : (
                <Circle size={14} className="todobar__ico" />
              )}
              <span className="todobar__text">
                {t.status === "in_progress" && t.activeForm ? t.activeForm : t.content}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
