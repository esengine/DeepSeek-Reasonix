import { useEffect, useRef, useState } from "react";
import { useT } from "../lib/i18n";
import type { Todo } from "../lib/tools";
import { shouldOpenTodoPanelByDefault } from "../lib/todoVisibility";
import { PromptBadge, PromptHeaderAction, PromptShelf } from "./PromptShelf";

const STORAGE_KEY = "todoPanel:openStates";
const MAX_STORED_OPEN_STATES = 80;

function loadOpenStates(): Record<string, boolean> {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (!saved) return {};
    const parsed = JSON.parse(saved) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};
    const states: Record<string, boolean> = {};
    for (const [key, value] of Object.entries(parsed)) {
      if (typeof value === "boolean") states[key] = value;
    }
    return states;
  } catch {
    return {};
  }
}

function loadOpenState(stateKey: string, defaultOpen: boolean): boolean {
  const states = loadOpenStates();
  return Object.prototype.hasOwnProperty.call(states, stateKey) ? states[stateKey] : defaultOpen;
}

function saveOpenState(stateKey: string, open: boolean): void {
  try {
    const entries = Object.entries(loadOpenStates()).filter(([key]) => key !== stateKey);
    entries.push([stateKey, open]);
    const trimmed = entries.slice(-MAX_STORED_OPEN_STATES);
    localStorage.setItem(STORAGE_KEY, JSON.stringify(Object.fromEntries(trimmed)));
  } catch {
    /* ignore quota errors */
  }
}

// TodoPanel is the live task list pinned just above the composer — the kernel's
// latest todo_write call drives it, and it updates in place as the agent flips
// items to in_progress / completed. Each new todo batch starts collapsed so the
// header can show live progress and the current task without occupying extra
// space. Manual expand/collapse is restored only for the same batch.
export function TodoPanel({
  stateKey,
  todos,
  onDismiss,
}: {
  stateKey: string;
  todos: Todo[];
  onDismiss: () => void;
}) {
  const t = useT();
  const currentRef = useRef<HTMLLIElement | null>(null);

  const done = todos.filter((t) => t.status === "completed").length;
  const current = todos.find((t) => t.status === "in_progress");
  const allDone = todos.length > 0 && done === todos.length;
  const summary = current?.activeForm || current?.content || todos[todos.length - 1]?.content || "";
  const [open, setOpen] = useState(() => loadOpenState(stateKey, shouldOpenTodoPanelByDefault()));
  const wasAllDoneRef = useRef(allDone);

  useEffect(() => {
    if (allDone && !wasAllDoneRef.current) {
      saveOpenState(stateKey, false);
      setOpen(false);
    }
    wasAllDoneRef.current = allDone;
  }, [allDone, stateKey]);

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
          <PromptHeaderAction
            onClick={() => setOpen((value) => {
              const next = !value;
              saveOpenState(stateKey, next);
              return next;
            })}
          >
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
