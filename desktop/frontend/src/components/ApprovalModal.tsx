import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent } from "react";
import gsap from "gsap";
import { useT } from "../lib/i18n";
import type { ComposerInsertRequest, DirEntry, WireApproval } from "../lib/types";
import { PromptAction, PromptBadge, PromptHeaderAction, PromptShelf } from "./PromptShelf";
import { playAttentionChime } from "../lib/sound";
import { DUR_FAST } from "../lib/gsapAnimations";
import {
  FileReferenceMenu,
  insertTextAtSelection,
  pickInlineFileReference,
  useFileReferenceMenu,
} from "./FileReferenceMenu";

function animateShelfExit(
  el: HTMLDivElement,
  options: { opacity: number; y: number; duration: number; ease: string; onComplete: () => void },
) {
  const animator = typeof gsap.to === "function"
    ? gsap
    : (gsap as unknown as { default?: typeof gsap }).default;
  if (animator && typeof animator.to === "function") {
    animator.to(el, options);
    return;
  }
  options.onComplete();
}

export function ApprovalModal({
  approval,
  onAnswer,
  onRevisePlan,
  onExitPlan,
  onStop,
  cwd,
  insertRequest,
  onRevisionActiveChange,
}: {
  approval: WireApproval;
  onAnswer: (allow: boolean, session: boolean, persist: boolean) => void;
  onRevisePlan?: (text: string) => void;
  onExitPlan?: () => void;
  onStop: () => void;
  cwd?: string;
  insertRequest?: ComposerInsertRequest | null;
  onRevisionActiveChange?: (active: boolean) => void;
}) {
  const t = useT();
  const isPlanApproval = approval.tool === "exit_plan_mode";
  const [revisionOpen, setRevisionOpen] = useState(false);
  const [revisionText, setRevisionText] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(() => (isPlanApproval ? 1 : 0));
  const cardRef = useRef<HTMLDivElement | null>(null);
  const shelfRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  const consumedInsertIdRef = useRef(0);
  // When consecutive approvals arrive, animate the old card out before
  // the new one slides in.  GSAP fromTo on the shelf wrapper avoids the
  // jarring pop when the API cycles through 4+ pending approvals.
  const closingRef = useRef(false);
  const subject = approval.subject.trim();
  const fileMenu = useFileReferenceMenu(revisionText, cwd);

  const answerWithExit = (fn: () => void) => {
    if (closingRef.current) return;
    closingRef.current = true;
    const el = shelfRef.current;
    if (el) {
      animateShelfExit(el, {
        opacity: 0,
        y: 8,
        duration: DUR_FAST,
        ease: "power2.in",
        onComplete: fn,
      });
    } else {
      fn();
    }
  };

  const choosePlanAction = (key: string) => {
    if (key === "1") setRevisionOpen((open) => !open);
    else if (key === "2") answerWithExit(() => onAnswer(true, false, false));
    else if (key === "3") answerWithExit(() => (onExitPlan ?? (() => onAnswer(false, false, false)))());
    else if (key === "Escape") answerWithExit(onStop);
  };

  const chooseToolAction = (key: string) => {
    if (key === "1") answerWithExit(() => onAnswer(true, false, false));
    else if (key === "2") answerWithExit(() => onAnswer(true, true, false));
    else if (key === "3") answerWithExit(() => onAnswer(true, true, true));
    else if (key === "4") answerWithExit(() => onAnswer(false, false, false));
    else if (key === "Escape") answerWithExit(onStop);
  };

  useEffect(() => {
    cardRef.current?.focus();
    setRevisionOpen(false);
    setRevisionText("");
    setSelectedIndex(isPlanApproval ? 1 : 0);
    playAttentionChime();
  }, [approval.id, isPlanApproval]);

  const actionCount = isPlanApproval ? 3 : 4;
  const selectedIndexRef = useRef(selectedIndex);
  selectedIndexRef.current = selectedIndex;

  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const target = event.target instanceof Element ? event.target : null;
      const tag = target?.tagName.toLowerCase();
      if (tag === "input" || tag === "textarea" || tag === "select" || (target instanceof HTMLElement && target.isContentEditable)) return;
      const interactiveTarget = target?.closest("button, a, [role='button'], [role='link']");
      if (interactiveTarget && (event.key === "ArrowLeft" || event.key === "ArrowRight" || event.key === "Enter")) return;
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        setSelectedIndex((i) => (i - 1 + actionCount) % actionCount);
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        setSelectedIndex((i) => (i + 1) % actionCount);
      } else if (event.key === "Enter") {
        event.preventDefault();
        const key = String(selectedIndexRef.current + 1);
        if (isPlanApproval) choosePlanAction(key);
        else chooseToolAction(key);
      } else if (event.key === "1" || event.key === "2" || event.key === "3" || event.key === "4" || event.key === "Escape") {
        event.preventDefault();
        if (isPlanApproval) choosePlanAction(event.key);
        else chooseToolAction(event.key);
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [isPlanApproval, onAnswer, onExitPlan, onStop, actionCount]);

  useEffect(() => {
    if (revisionOpen) {
      onRevisionActiveChange?.(true);
      inputRef.current?.focus();
      return () => onRevisionActiveChange?.(false);
    }
    onRevisionActiveChange?.(false);
  }, [revisionOpen, onRevisionActiveChange]);

  const focusRevisionInput = (caret = revisionText.length) => {
    requestAnimationFrame(() => {
      const input = inputRef.current;
      if (!input) return;
      input.focus();
      input.setSelectionRange(caret, caret);
    });
  };

  const insertRevisionText = useCallback((text: string) => {
    const input = inputRef.current;
    const start = input?.selectionStart ?? revisionText.length;
    const end = input?.selectionEnd ?? start;
    const next = insertTextAtSelection(revisionText, text, start, end);
    setRevisionText(next.value);
    focusRevisionInput(next.caret);
  }, [revisionText]);

  useEffect(() => {
    if (!insertRequest || insertRequest.id === consumedInsertIdRef.current) return;
    consumedInsertIdRef.current = insertRequest.id;
    insertRevisionText(insertRequest.text);
  }, [insertRequest, insertRevisionText]);

  const pickRevisionFile = (entry: DirEntry) => {
    const next = pickInlineFileReference(revisionText, fileMenu.atRaw, fileMenu.atDir, entry);
    setRevisionText(next);
    focusRevisionInput(next.length);
  };

  const onRevisionKeyDown = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
      submitRevision();
      event.stopPropagation();
      return;
    }
    if (fileMenu.open) {
      if (event.key === "ArrowDown" && fileMenu.count > 0) {
        event.preventDefault();
        fileMenu.setActive((index) => (index + 1) % fileMenu.count);
        return;
      }
      if (event.key === "ArrowUp" && fileMenu.count > 0) {
        event.preventDefault();
        fileMenu.setActive((index) => (index - 1 + fileMenu.count) % fileMenu.count);
        return;
      }
      if ((event.key === "Enter" || event.key === "Tab") && fileMenu.count > 0) {
        event.preventDefault();
        const entry = fileMenu.items[fileMenu.active];
        if (entry) pickRevisionFile(entry);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        fileMenu.dismiss();
        return;
      }
    }
    event.stopPropagation();
  };

  const submitRevision = () => {
    const text = revisionText.trim();
    if (!text) {
      inputRef.current?.focus();
      return;
    }
    answerWithExit(() => onRevisePlan?.(text));
  };

  // The plan is already shown above as the assistant's reply; this is just the gate.
  if (isPlanApproval) {
    return (
      <div ref={shelfRef}>
        <PromptShelf
          barRef={cardRef}
          titleId="plan-approval-title"
          title={t("approval.planReady")}
          meta={t("approval.planReadyHint")}
          badges={revisionOpen ? <PromptBadge>{t("approval.revisePlan")}</PromptBadge> : undefined}
          headerActions={
            <PromptHeaderAction onClick={() => answerWithExit(onStop)} ariaLabel={t("composer.stopShort")}>
              Esc
            </PromptHeaderAction>
          }
          actions={
            <>
              <PromptAction keyLabel="1" label={t("approval.revisePlan")} onClick={() => setRevisionOpen((open) => !open)} selected={selectedIndex === 0} />
              <PromptAction keyLabel="2" label={t("approval.startExecution")} onClick={() => answerWithExit(() => onAnswer(true, false, false))} selected={selectedIndex === 1} />
              <PromptAction
                keyLabel="3"
                label={t("approval.exitPlan")}
                onClick={() => answerWithExit(() => (onExitPlan ?? (() => onAnswer(false, false, false)))())}
                selected={selectedIndex === 2}
              />
            </>
          }
        >
          {revisionOpen && (
            <div className="plan-revision">
              <textarea
                ref={inputRef}
                className="plan-revision__input"
                value={revisionText}
                rows={3}
                placeholder={t("approval.revisePlanPlaceholder")}
                onChange={(event) => setRevisionText(event.target.value)}
                onFocus={() => onRevisionActiveChange?.(true)}
                onKeyDown={onRevisionKeyDown}
              />
              {fileMenu.open && (
                <FileReferenceMenu
                  items={fileMenu.items}
                  activeIndex={fileMenu.active}
                  onPick={pickRevisionFile}
                  onHover={fileMenu.setActive}
                />
              )}
              <div className="plan-revision__actions">
                <button className="btn" onClick={() => setRevisionOpen(false)}>
                  {t("common.cancel")}
                </button>
                <button className="btn btn--primary" onClick={submitRevision}>
                  {t("approval.sendRevision")}
                </button>
              </div>
            </div>
          )}
        </PromptShelf>
      </div>
    );
  }

  return (
    <div ref={shelfRef}>
      <PromptShelf
        barRef={cardRef}
        titleId="tool-approval-title"
        title={t("approval.toolPending")}
        badges={<PromptBadge>{approval.tool}</PromptBadge>}
        headerActions={
          <PromptHeaderAction onClick={() => answerWithExit(onStop)} ariaLabel={t("composer.stopShort")}>
            Esc
          </PromptHeaderAction>
        }
        actions={
          <>
            <PromptAction keyLabel="1" label={t("approval.allowOnce")} onClick={() => answerWithExit(() => onAnswer(true, false, false))} selected={selectedIndex === 0} />
            <PromptAction keyLabel="2" label={t("approval.allowRuleSession")} onClick={() => answerWithExit(() => onAnswer(true, true, false))} selected={selectedIndex === 1} />
            <PromptAction keyLabel="3" label={t("approval.allowRulePersistent")} onClick={() => answerWithExit(() => onAnswer(true, true, true))} selected={selectedIndex === 2} />
            <PromptAction keyLabel="4" label={t("approval.deny")} onClick={() => answerWithExit(() => onAnswer(false, false, false))} selected={selectedIndex === 3} />
          </>
        }
      >
        {approval.reason && <div className="approval-reason">{approval.reason}</div>}
        {subject && (
          <pre className="approval-subject">{subject}</pre>
        )}
      </PromptShelf>
    </div>
  );
}
