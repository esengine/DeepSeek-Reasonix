import { useEffect, useRef, useState } from "react";
import { useT } from "../lib/i18n";
import type { Todo } from "../lib/tools";
import { PromptBadge, PromptHeaderAction, PromptShelf } from "./PromptShelf";

const STORAGE_KEY = "todoPanel:open";

function loadOpenState(): boolean {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    return saved === null ? true : saved === "1";
  } catch {
    return true;
  }
}

function saveOpenState(open: boolean): void {
  try {
    localStorage.setItem(STORAGE_KEY, open ? "1" : "0");
  } catch {
    /* ignore quota errors */
  }
}

// TodoPanel is the live task list pinned just above the composer — the kernel's
// latest todo_write call drives it, and it updates in place as the agent flips
// items to in_progress / completed. The collapsed/expanded state is persisted
// globally in localStorage so the user's choice sticks across sessions.
export function TodoPanel({
  todos,
  onDismiss,
}: {
  todos: Todo[];
  onDismiss: () => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(loadOpenState);
  const currentRef = useRef<HTMLLIElement | null>(null);

  const done = todos.filter((t) => t.status === "completed").length;
  const current = todos.find((t) => t.status === "in_progress");
  const allDone = todos.length > 0 && done === todos.length;
  const summary = current?.activeForm || current?.content || todos[todos.length - 1]?.content || "";

  useEffect(() => {
    if (!open) return;
    currentRef.current?.scrollIntoView({ block: "nearest" });
  }, [open, current?.content, current?.activeForm]);

  if (todos.length === 0) return null;

  return (
    <PromptShelf
      titleId="todo-shelf-title"
      title={t("todo.title")}
      badges={<PromptBadge>{done}/{todos.length}</PromptBadge>}
      meta={summary}
      role="region"
      headerActions={
        <>
          <PromptHeaderAction onClick={() => setOpen((value) => { const next = !value; saveOpenState(next); return next; })}>
            {open ? t("common.collapse") : t("common.expand")}
          </PromptHeaderAction>
          {allDone && (
            <PromptHeaderAction onClick={onDismiss}>
              {t("common.close")}
            </PromptHeaderAction>
          )}
        </>
      }
    >
      {open && (
        <ul className="todobar__list">
          {todos.map((todo, index) => {
            const status = normalizeTodoStatus(todo.status);
            return (
              <li
                key={index}
                ref={status === "in_progress" ? currentRef : undefined}
                className={`todobar__item todobar__item--${status}${todo.level ? " todobar__item--sub" : ""}`}
              >
                <span className={`todobar__status todobar__status--${status}`}>
                  {t(todoStatusLabelKey(status))}
                </span>
                <span className="todobar__text">
                  {status === "in_progress" && todo.activeForm ? todo.activeForm : todo.content}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </PromptShelf>
  );
}

function normalizeTodoStatus(status: Todo["status"]): "pending" | "in_progress" | "completed" {
  switch (String(status ?? "").trim()) {
    case "completed":
      return "completed";
    case "in_progress":
      return "in_progress";
    default:
      return "pending";
  }
}

function todoStatusLabelKey(status: "pending" | "in_progress" | "completed"): "todo.pending" | "todo.inProgress" | "todo.completed" {
  switch (status) {
    case "completed":
      return "todo.completed";
    case "in_progress":
      return "todo.inProgress";
    default:
      return "todo.pending";
  }
}
