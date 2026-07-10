import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent } from "react";
import gsap from "gsap";
import { useT, type Translator } from "../lib/i18n";
import type { ComposerInsertRequest, DirEntry, ToolApprovalMode, WireApproval } from "../lib/types";
import { PromptAction, PromptBadge, PromptHeaderAction, PromptShelf } from "./PromptShelf";
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

function requiresFreshHumanApproval(tool: string): boolean {
  return tool === "remember" || tool === "forget" || tool === "exit_plan_mode" || tool === "sandbox_escape" || tool === "config_write";
}

const APPROVAL_MODE_RANK: Record<ToolApprovalMode, number> = { ask: 0, auto: 1, yolo: 2 };

export function approvalToolLabel(tool: string, t: Translator): string {
  switch (tool) {
    case "bash":
      return t("approval.toolLabelBash");
    case "edit_file":
      return t("approval.toolLabelEditFile");
    case "write_file":
      return t("approval.toolLabelWriteFile");
    case "multi_edit":
      return t("approval.toolLabelMultiEdit");
    case "move_file":
      return t("approval.toolLabelMoveFile");
    case "web_fetch":
      return t("approval.toolLabelWebFetch");
    case "run_skill":
      return t("approval.toolLabelRunSkill");
    case "remember":
      return t("approval.toolLabelRemember");
    case "forget":
      return t("approval.toolLabelForget");
    case "sandbox_escape":
      return t("approval.toolLabelSandboxEscape");
    case "config_write":
      return t("approval.toolLabelConfigWrite");
    case "plan_mode_read_only_command":
      return t("approval.toolLabelPlanModeReadOnly");
    case "exit_plan_mode":
      return t("approval.toolLabelExitPlan");
    default:
      return tool;
  }
}

const sandboxEscapeEnglishSubjectFallback = "run shell command unconfined once";
const sandboxEscapeEnglishSubjectPrefix = "run unconfined once: ";
const configWriteEnglishSubjectPrefix = "write Reasonix config: ";
const planModeMcpEnglishSubject = /^MCP (.+) as read-only for planning and research$/;
const planModeBashEnglishSubject = /^Trust (.+) as a read-only command prefix while planning\r?\nCommand: ([\s\S]+)$/;

function localizeApprovalSubject(tool: string, subject: string, t: Translator): string {
  const trimmed = subject.trim();
  if (tool === "sandbox_escape") {
    if (!trimmed || trimmed === sandboxEscapeEnglishSubjectFallback) return t("approval.sandboxEscapeSubjectFallback");
    if (trimmed.startsWith(sandboxEscapeEnglishSubjectPrefix)) {
      return `${t("approval.sandboxEscapeSubjectPrefix")}${trimmed.slice(sandboxEscapeEnglishSubjectPrefix.length)}`;
    }
    return trimmed;
  }
  if (tool === "config_write") {
    if (trimmed.startsWith(configWriteEnglishSubjectPrefix)) {
      return `${t("approval.configWriteSubjectPrefix")}${trimmed.slice(configWriteEnglishSubjectPrefix.length)}`;
    }
    return trimmed;
  }
  if (tool === "remember") {
    return trimmed
      .replace(/^Save\/update memory/, t("approval.memorySaveUpdate"))
      .replace(/\bbody: /g, `${t("approval.memoryBodyLabel")}: `);
  }
  if (tool === "forget" && trimmed.startsWith("Archive memory ")) {
    return `${t("approval.memoryArchivePrefix")}${trimmed.slice("Archive memory ".length)}`;
  }
  const mcpTrust = trimmed.match(planModeMcpEnglishSubject);
  if (mcpTrust) {
    return t("approval.planModeMcpTrustSubject", { target: mcpTrust[1] ?? "" });
  }
  const bashTrust = trimmed.match(planModeBashEnglishSubject);
  if (bashTrust) {
    return t("approval.planModeBashTrustSubject", { prefix: bashTrust[1] ?? "", command: bashTrust[2] ?? "" });
  }
  return trimmed;
}

function localizeApprovalReason(tool: string, reason: string | undefined, t: Translator): string {
  const trimmed = reason?.trim() ?? "";
  if (tool === "config_write") {
    if (!trimmed || trimmed.includes("Reasonix-managed configuration file")) return t("approval.configWriteReason");
    return trimmed;
  }
  if (tool !== "sandbox_escape") return trimmed;
  if (trimmed.includes("could not wrap this command")) return t("approval.sandboxEscapeWrapReason");
  if (trimmed.includes("failed while starting this command") || trimmed.includes("Run this command unconfined once?")) {
    return t("approval.sandboxEscapeRuntimeReason");
  }
  return trimmed || t("approval.sandboxEscapeRuntimeReason");
}

function localizePlanModeApprovalReason(tool: string, reason: string, t: Translator): string {
  if (tool === "plan_mode_read_only_command" && reason.includes("built-in read-only set")) {
    return t("approval.planModeBashTrustReason");
  }
  if (reason.includes("external read-only hints need your confirmation")) {
    return t("approval.planModeMcpTrustReason");
  }
  return reason;
}

export function ApprovalModal({
  approval,
  onAnswer,
  onRevisePlan,
  onExitPlan,
  onStop,
  cwd,
  tabId,
  workspaceScopeKey,
  insertRequest,
  onRevisionActiveChange,
  toolApprovalMode,
}: {
  approval: WireApproval;
  onAnswer: (allow: boolean, session: boolean, persist: boolean) => void;
  onRevisePlan?: (text: string) => void;
  onExitPlan?: () => void;
  onStop: () => void;
  cwd?: string;
  tabId?: string;
  workspaceScopeKey?: string;
  insertRequest?: ComposerInsertRequest | null;
  onRevisionActiveChange?: (active: boolean) => void;
  toolApprovalMode?: ToolApprovalMode;
}) {
  const t = useT();
  const isPlanApproval = approval.tool === "exit_plan_mode";
  const toolLabel = approvalToolLabel(approval.tool, t);
  const isFreshHumanApproval = requiresFreshHumanApproval(approval.tool);
  const hasFreshSessionGrant = approval.tool === "sandbox_escape" || approval.tool === "config_write";
  // Switching the approval segmented control to a more permissive mode does not
  // resolve an already-pending request; say so on the card instead of leaving
  // the user to wonder why the switch "did nothing".
  const initialToolApprovalModeRef = useRef(toolApprovalMode);
  const approvalModeRelaxed =
    !isPlanApproval &&
    toolApprovalMode !== undefined &&
    initialToolApprovalModeRef.current !== undefined &&
    APPROVAL_MODE_RANK[toolApprovalMode] > APPROVAL_MODE_RANK[initialToolApprovalModeRef.current];
  const subject = localizeApprovalSubject(approval.tool, approval.subject, t);
  const reason = localizePlanModeApprovalReason(approval.tool, localizeApprovalReason(approval.tool, approval.reason, t), t);
  const subjectSummary = subject.split(/\r?\n/).find((line) => line.trim())?.trim() ?? "";
  const toolMeta = reason || subjectSummary || approval.tool;
  const hasToolDetails = Boolean(reason || subject);
  const showToolDetailsByDefault = !isPlanApproval && hasToolDetails;
  const [revisionOpen, setRevisionOpen] = useState(false);
  const [revisionText, setRevisionText] = useState("");
  const [detailsOpen, setDetailsOpen] = useState(() => showToolDetailsByDefault);
  const [selectedIndex, setSelectedIndex] = useState(() => (isPlanApproval ? 1 : 0));
  // Action index currently hovered/focused; the consequence preview row
  // prefers it over the keyboard-selected action.
  const [hoverIndex, setHoverIndex] = useState<number | null>(null);
  const cardRef = useRef<HTMLDivElement | null>(null);
  const shelfRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  const consumedInsertIdRef = useRef(0);
  // When consecutive approvals arrive, animate the old card out before
  // the new one slides in.  GSAP fromTo on the shelf wrapper avoids the
  // jarring pop when the API cycles through 4+ pending approvals.
  const closingRef = useRef(false);
  const fileMenu = useFileReferenceMenu(revisionText, cwd, tabId, workspaceScopeKey);

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
    else if (hasFreshSessionGrant && key === "2") answerWithExit(() => onAnswer(true, true, false));
    else if (hasFreshSessionGrant && key === "3") answerWithExit(() => onAnswer(false, false, false));
    else if (isFreshHumanApproval && key === "2") answerWithExit(() => onAnswer(false, false, false));
    else if (isFreshHumanApproval && key === "4") answerWithExit(() => onAnswer(false, false, false));
    else if (!isFreshHumanApproval && key === "2") answerWithExit(() => onAnswer(true, true, false));
    else if (!isFreshHumanApproval && key === "3") answerWithExit(() => onAnswer(true, true, true));
    else if (!isFreshHumanApproval && key === "4") answerWithExit(() => onAnswer(false, false, false));
    else if (key === "Escape") answerWithExit(onStop);
  };

  const selectedToolActionKey = (index: number) => {
    if (!isFreshHumanApproval) return String(index + 1);
    if (hasFreshSessionGrant) return index === 0 ? "1" : index === 1 ? "2" : "3";
    return index === 0 ? "1" : "2";
  };

  useEffect(() => {
    cardRef.current?.focus();
    setRevisionOpen(false);
    setRevisionText("");
    setDetailsOpen(showToolDetailsByDefault);
    setSelectedIndex(isPlanApproval ? 1 : 0);
    setHoverIndex(null);
  }, [approval.id, isPlanApproval, showToolDetailsByDefault]);

  const actionCount = isPlanApproval ? 3 : isFreshHumanApproval ? (hasFreshSessionGrant ? 3 : 2) : 4;
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
        else chooseToolAction(selectedToolActionKey(selectedIndexRef.current));
      } else if (event.key === "1" || event.key === "2" || event.key === "3" || event.key === "4" || event.key === "Escape") {
        event.preventDefault();
        if (isPlanApproval) choosePlanAction(event.key);
        else chooseToolAction(event.key);
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [isPlanApproval, isFreshHumanApproval, hasFreshSessionGrant, onAnswer, onExitPlan, onStop, actionCount]);

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
          className="prompt-shelf--compact prompt-shelf--plan-approval"
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

  // Descriptor list mirrors the action buttons below; the consequence row
  // previews what the hovered (or keyboard-selected) action will actually do,
  // in the quick-pick style the ask card uses for option descriptions.
  const toolActions: { key: string; label: string; desc: string; run: () => void }[] = [
    { key: "1", label: t("approval.allowOnce"), desc: t("approval.allowOnceDesc"), run: () => onAnswer(true, false, false) },
    ...(isFreshHumanApproval
      ? hasFreshSessionGrant
        ? [
            {
              key: "2",
              label: t(approval.tool === "config_write" ? "approval.allowConfigWriteSession" : "approval.allowSandboxEscapeSession"),
              desc: t(approval.tool === "config_write" ? "approval.allowConfigWriteSessionDesc" : "approval.allowSandboxEscapeSessionDesc"),
              run: () => onAnswer(true, true, false),
            },
            { key: "3", label: t("approval.deny"), desc: t("approval.denyDesc"), run: () => onAnswer(false, false, false) },
          ]
        : [{ key: "2", label: t("approval.deny"), desc: t("approval.denyDesc"), run: () => onAnswer(false, false, false) }]
      : [
          { key: "2", label: t("approval.allowRuleSession"), desc: t("approval.allowRuleSessionDesc"), run: () => onAnswer(true, true, false) },
          { key: "3", label: t("approval.allowRulePersistent"), desc: t("approval.allowRulePersistentDesc"), run: () => onAnswer(true, true, true) },
          { key: "4", label: t("approval.deny"), desc: t("approval.denyDesc"), run: () => onAnswer(false, false, false) },
        ]),
  ];
  const previewAction = toolActions[hoverIndex ?? selectedIndex] ?? null;

  return (
    <div ref={shelfRef}>
      <PromptShelf
        className="prompt-shelf--compact prompt-shelf--tool-approval"
        barRef={cardRef}
        titleId="tool-approval-title"
        title={t("approval.toolPending")}
        badges={<PromptBadge>{toolLabel}</PromptBadge>}
        meta={toolMeta}
        headerActions={
          <>
            {hasToolDetails && (
              <PromptHeaderAction onClick={() => setDetailsOpen((open) => !open)}>
                {t(detailsOpen ? "approval.hideDetails" : "approval.details")}
              </PromptHeaderAction>
            )}
            <PromptHeaderAction onClick={() => answerWithExit(onStop)} ariaLabel={t("composer.stopShort")}>
              Esc
            </PromptHeaderAction>
          </>
        }
        actions={
          <>
            {toolActions.map((action, index) => (
              <PromptAction
                key={action.key}
                keyLabel={action.key}
                label={action.label}
                onClick={() => answerWithExit(action.run)}
                selected={selectedIndex === index}
                title={action.desc}
                onHoverChange={(hovering) =>
                  setHoverIndex((current) => (hovering ? index : current === index ? null : current))
                }
              />
            ))}
          </>
        }
        note={
          previewAction && (
            <div className="approval-consequence">
              <span className="approval-consequence__label">{previewAction.label}</span>
              <span className="approval-consequence__text">{previewAction.desc}</span>
            </div>
          )
        }
      >
        {/* Guard the whole block: PromptShelf only renders its body when children
            are truthy, and a fragment of two false branches would still count. */}
        {(approvalModeRelaxed || detailsOpen) && (
          <>
            {approvalModeRelaxed && (
              <div className="approval-mode-hint">{t("approval.modeSwitchPendingHint")}</div>
            )}
            {detailsOpen && (
              <div className="approval-details">
                {reason && <div className="approval-reason">{reason}</div>}
                {subject && (
                  <pre className="approval-subject">{subject}</pre>
                )}
              </div>
            )}
          </>
        )}
      </PromptShelf>
    </div>
  );
}
