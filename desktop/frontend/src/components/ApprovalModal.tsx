import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent } from "react";
import gsap from "gsap";
import { useT, type Translator } from "../lib/i18n";
import type { ComposerInsertRequest, DirEntry, ToolApprovalMode, WireApproval } from "../lib/types";
import {
  DecisionConfirmBar,
  PromptAction,
  PromptBadge,
  PromptHeaderAction,
  PromptShelf,
} from "./PromptShelf";
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
  if (trimmed.includes("could not wrap this command") || trimmed.includes("does not provide an OS-level Bash sandbox")) {
    return t("approval.sandboxEscapeWrapReason");
  }
  if (
    trimmed.includes("failed while starting this command") ||
    trimmed.includes("could not start this command") ||
    trimmed.includes("Run this command unconfined once?")
  ) {
    return t("approval.sandboxEscapeRuntimeReason");
  }
  return trimmed || t("approval.sandboxEscapeRuntimeReason");
}

function localizePlanModeApprovalReason(tool: string, reason: string, t: Translator): string {
  if (tool === "plan_mode_read_only_command" && reason.includes("built-in read-only set")) {
    return t("approval.planModeBashTrustReason");
  }
  return reason;
}

type DecisionAction = {
  key: string;
  label: string;
  desc: string;
  tone?: "default" | "danger";
  primary?: boolean;
  // Plan revision and plan guidance open inline editors instead of submitting.
  // Other recovery actions use direct-click submit (no select-then-confirm).
  kind: "submit" | "toggle-revision" | "toggle-guidance" | "direct";
  run?: () => void;
};

const RECOVERY_FEEDBACK_MAX = 1000;

function recoveryReasonText(
  changeKind: string | undefined,
  fallback: string | undefined,
  t: Translator,
): string {
  switch ((changeKind ?? "").toLowerCase()) {
    case "risk":
      return t("approval.recoveryReasonRisk");
    case "scope":
      return t("approval.recoveryReasonScope");
    case "strategy":
      return t("approval.recoveryReasonStrategy");
    case "uncertain":
    case "same_strategy":
      return t("approval.recoveryReasonUncertain");
    default:
      return fallback?.trim() || t("approval.recoveryReasonUncertain");
  }
}

type PlanLine = { key: string; text: string };
type PlanDelta = { removed: string[]; added: string[] };

function planLines(raw: string | undefined): PlanLine[] {
  return (raw ?? "")
    .split(/\r?\n/)
    .map((line) => line.replace(/\s+\[[^\]\r\n]+\]\s*$/, "").trimEnd())
    .filter((line) => line.trim() !== "")
    .map((line) => {
      const match = line.match(/^(\s*)(?:\d+\.\s*)?(.*)$/);
      const nested = (match?.[1].length ?? 0) > 0;
      const body = (match?.[2] ?? line).replace(/\s+/g, " ").trim();
      return { key: `${nested ? 1 : 0}:${body}`, text: `${nested ? "  " : ""}${body}` };
    });
}

// LCS keeps unchanged steps out of the card and turns additions, removals, and
// reordering into a compact plan-level delta. Status suffixes are ignored.
function planDelta(beforeRaw: string | undefined, afterRaw: string | undefined): PlanDelta | null {
  const before = planLines(beforeRaw);
  const after = planLines(afterRaw);
  if (before.length === 0 || after.length === 0) return null;
  const dp = Array.from({ length: before.length + 1 }, () => Array<number>(after.length + 1).fill(0));
  for (let i = before.length - 1; i >= 0; i -= 1) {
    for (let j = after.length - 1; j >= 0; j -= 1) {
      dp[i][j] = before[i].key === after[j].key
        ? dp[i + 1][j + 1] + 1
        : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const removed: string[] = [];
  const added: string[] = [];
  let i = 0;
  let j = 0;
  while (i < before.length && j < after.length) {
    if (before[i].key === after[j].key) {
      i += 1;
      j += 1;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      removed.push(before[i].text);
      i += 1;
    } else {
      added.push(after[j].text);
      j += 1;
    }
  }
  while (i < before.length) removed.push(before[i++].text);
  while (j < after.length) added.push(after[j++].text);
  return removed.length > 0 || added.length > 0 ? { removed, added } : null;
}

export function ApprovalModal({
  approval,
  onAnswer,
  onResolveRecovery,
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
  onResolveRecovery?: (action: "continue" | "continue_task" | "revise", feedback?: string) => void;
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
  const isRecoveryApproval = approval.kind === "recovery" || Boolean(approval.recovery);
  const recovery = approval.recovery;
  const recoveryChangeKind = (recovery?.change_kind ?? "").toLowerCase();
  const isRecoveryPlanChange =
    isRecoveryApproval && (recoveryChangeKind === "strategy" || recoveryChangeKind === "scope");
  const taskGrantScope = recovery?.task_grant_scope?.trim() ?? "";
  const toolLabel = approvalToolLabel(approval.tool, t);
  const isFreshHumanApproval = approval.fresh === true || requiresFreshHumanApproval(approval.tool) || isRecoveryApproval;
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
  // Plan approvals already show the plan above; keep a short hint. Tool
  // approvals surface the command/subject by default (reason is secondary).
  const toolMeta = isPlanApproval ? t("approval.planReadyHint") : (subjectSummary || reason || approval.tool);
  const hasToolDetails = Boolean(reason || subject);
  // Subject (command) is visible by default; long reason can collapse.
  const [reasonOpen, setReasonOpen] = useState(() => {
    if (isRecoveryApproval) return false; // recovery details stay collapsed
    return Boolean(reason) && reason.length <= 160;
  });
  // Immediate Plan/Auto decisions have no hidden selection. Ordinary tool
  // approvals retain select-then-confirm and default to Allow once.
  const [selectedIndex, setSelectedIndex] = useState(() => (isPlanApproval || isRecoveryApproval ? -1 : 0));
  const [revisionOpen, setRevisionOpen] = useState(false);
  const [revisionText, setRevisionText] = useState("");
  const [recoveryGuidanceOpen, setRecoveryGuidanceOpen] = useState(false);
  const [recoveryGuidanceText, setRecoveryGuidanceText] = useState("");
  const [grantSimilarForTask, setGrantSimilarForTask] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const cardRef = useRef<HTMLDivElement | null>(null);
  const shelfRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  const recoveryGuidanceRef = useRef<HTMLTextAreaElement | null>(null);
  const recoveryGuidanceTriggerRef = useRef<HTMLButtonElement | null>(null);
  const consumedInsertIdRef = useRef(0);
  const onRevisionActiveChangeRef = useRef(onRevisionActiveChange);
  const revisionActiveRef = useRef(false);
  onRevisionActiveChangeRef.current = onRevisionActiveChange;
  // When consecutive approvals arrive, animate the old card out before
  // the new one slides in.  GSAP fromTo on the shelf wrapper avoids the
  // jarring pop when the API cycles through 4+ pending approvals.
  const closingRef = useRef(false);
  const fileMenu = useFileReferenceMenu(revisionText, cwd, tabId, workspaceScopeKey);

  const answerWithExit = (fn: () => void) => {
    if (closingRef.current || submitting) return;
    closingRef.current = true;
    setSubmitting(true);
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

  const resolveRecovery = useCallback(
    (action: "continue" | "continue_task" | "revise", feedback?: string) => {
      const resolve = onResolveRecovery ?? ((a: "continue" | "continue_task" | "revise") => onAnswer(a !== "revise", false, false));
      if (action === "revise") {
        const text = feedback?.trim().slice(0, RECOVERY_FEEDBACK_MAX) ?? "";
        resolve("revise", text || undefined);
        return;
      }
      resolve(action);
    },
    [onResolveRecovery, onAnswer],
  );

  const toolActions: DecisionAction[] = isRecoveryPlanChange
    ? [
        {
          key: "1",
          label: t("approval.recoveryAdoptPlan"),
          desc: t("approval.recoveryAdoptPlanDesc"),
          kind: "direct",
          run: () => resolveRecovery("continue"),
        },
        {
          key: "2",
          label: t("approval.recoveryAdjustPlan"),
          desc: t("approval.recoveryAdjustPlanDesc"),
          kind: "toggle-guidance",
        },
      ]
    : isRecoveryApproval
    ? [
        {
          key: "1",
          label: t("approval.recoveryRevise"),
          desc: t("approval.recoveryReviseDesc"),
          primary: true,
          kind: "direct",
          run: () => resolveRecovery("revise"),
        },
        {
          key: "2",
          label: grantSimilarForTask
            ? t("approval.recoveryContinueTask")
            : t("approval.recoveryContinue"),
          desc: grantSimilarForTask
            ? t("approval.recoveryContinueTaskDesc")
            : t("approval.recoveryContinueDesc"),
          kind: "direct",
          run: () => resolveRecovery(grantSimilarForTask && recovery?.can_grant_task ? "continue_task" : "continue"),
        },
      ]
    : isPlanApproval
    ? [
        {
          key: "1",
          label: t("approval.startExecution"),
          desc: t("approval.startExecutionDesc"),
          primary: true,
          kind: "direct",
          run: () => onAnswer(true, false, false),
        },
        {
          key: "2",
          label: t("approval.revisePlan"),
          desc: t("approval.revisePlanDesc"),
          kind: "toggle-revision",
        },
        ...(onExitPlan
          ? [{
              key: "3",
              label: t("approval.exitPlanWithoutExecution"),
              desc: t("approval.exitPlanWithoutExecutionDesc"),
              kind: "direct" as const,
              run: () => onExitPlan(),
            }]
          : []),
      ]
    : [
        {
          key: "1",
          label: t("approval.allowOnce"),
          desc: t("approval.allowOnceDesc"),
          kind: "submit",
          run: () => onAnswer(true, false, false),
        },
        ...(isFreshHumanApproval
          ? hasFreshSessionGrant
            ? [
                {
                  key: "2",
                  label: t(approval.tool === "config_write" ? "approval.allowConfigWriteSession" : "approval.allowSandboxEscapeSession"),
                  desc: t(approval.tool === "config_write" ? "approval.allowConfigWriteSessionDesc" : "approval.allowSandboxEscapeSessionDesc"),
                  kind: "submit" as const,
                  run: () => onAnswer(true, true, false),
                },
                {
                  key: "3",
                  label: t("approval.deny"),
                  desc: t("approval.denyDesc"),
                  tone: "danger" as const,
                  kind: "submit" as const,
                  run: () => onAnswer(false, false, false),
                },
              ]
            : [
                {
                  key: "2",
                  label: t("approval.deny"),
                  desc: t("approval.denyDesc"),
                  tone: "danger" as const,
                  kind: "submit" as const,
                  run: () => onAnswer(false, false, false),
                },
              ]
          : [
              {
                key: "2",
                label: t("approval.allowRuleSession"),
                desc: t("approval.allowRuleSessionDesc"),
                kind: "submit" as const,
                run: () => onAnswer(true, true, false),
              },
              {
                key: "3",
                label: t("approval.allowRulePersistent"),
                desc: t("approval.allowRulePersistentDesc"),
                kind: "submit" as const,
                run: () => onAnswer(true, true, true),
              },
              {
                key: "4",
                label: t("approval.deny"),
                desc: t("approval.denyDesc"),
                tone: "danger" as const,
                kind: "submit" as const,
                run: () => onAnswer(false, false, false),
              },
            ]),
      ];

  const actionCount = toolActions.length;
  const selectedIndexRef = useRef(selectedIndex);
  selectedIndexRef.current = selectedIndex;
  const selectedAction = toolActions[Math.min(selectedIndex, actionCount - 1)] ?? toolActions[0];

  useEffect(() => {
    cardRef.current?.focus();
    setRevisionOpen(false);
    setRevisionText("");
    setRecoveryGuidanceOpen(false);
    setRecoveryGuidanceText("");
    setGrantSimilarForTask(false);
    setReasonOpen(isRecoveryApproval ? false : Boolean(reason) && reason.length <= 160);
    setSelectedIndex(isPlanApproval || isRecoveryApproval ? -1 : 0);
    setSubmitting(false);
    closingRef.current = false;
  }, [approval.id, isPlanApproval, isRecoveryApproval, reason]);

  const confirmSelected = useCallback(() => {
    if (submitting || closingRef.current) return;
    if (isPlanApproval || isRecoveryApproval) return;
    const action = toolActions[selectedIndexRef.current];
    if (!action) return;
    if (action.kind === "toggle-revision") {
      setRevisionOpen((open) => !open);
      return;
    }
    if (action.kind === "toggle-guidance") {
      setGrantSimilarForTask(false);
      setRecoveryGuidanceOpen(true);
      return;
    }
    if (action.run) answerWithExit(action.run);
  }, [submitting, toolActions, isPlanApproval, isRecoveryApproval]);

  const activateAction = useCallback((action: DecisionAction, index: number) => {
    if (submitting) return;
    if (action.kind === "direct" && action.run) {
      answerWithExit(action.run);
      return;
    }
    if (action.kind === "toggle-revision") {
      setRevisionOpen((open) => !open);
      return;
    }
    if (action.kind === "toggle-guidance") {
      setGrantSimilarForTask(false);
      setRecoveryGuidanceOpen(true);
      return;
    }
    setSelectedIndex(index);
  }, [submitting]);

  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (submitting) return;
      if (isRecoveryApproval && recoveryGuidanceOpen && event.key === "Escape") {
        event.preventDefault();
        setRecoveryGuidanceOpen(false);
        setRecoveryGuidanceText("");
        requestAnimationFrame(() => {
          if (isRecoveryPlanChange) cardRef.current?.focus();
          else recoveryGuidanceTriggerRef.current?.focus();
        });
        return;
      }
      const target = event.target instanceof Element ? event.target : null;
      const tag = target?.tagName.toLowerCase();
      // Editing revision / file menu owns arrows and digits while focused.
      // Custom recovery guidance owns all decision shortcuts while expanded.
      const editing =
        tag === "input" ||
        tag === "textarea" ||
        tag === "select" ||
        (target instanceof HTMLElement && target.isContentEditable) ||
        (isRecoveryApproval && recoveryGuidanceOpen);
      if (editing && (event.key === "1" || event.key === "2" || event.key === "3" || event.key === "4")) {
        return;
      }
      if (tag === "input" || tag === "textarea" || tag === "select" || (target instanceof HTMLElement && target.isContentEditable)) return;
      const immediateDecision = isPlanApproval || isRecoveryApproval;
      if (immediateDecision && (event.key === "ArrowUp" || event.key === "ArrowDown" || event.key === "Enter")) {
        return;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        setSelectedIndex((i) => {
          const base = i < 0 ? 0 : i;
          return (base - 1 + actionCount) % actionCount;
        });
      } else if (event.key === "ArrowDown") {
        event.preventDefault();
        setSelectedIndex((i) => {
          const base = i < 0 ? -1 : i;
          return (base + 1) % actionCount;
        });
      } else if (event.key === "Enter") {
        if (isRecoveryApproval && selectedIndexRef.current < 0) return;
        event.preventDefault();
        confirmSelected();
      } else if (event.key === "1" || event.key === "2" || event.key === "3" || event.key === "4") {
        if (isRecoveryApproval && recoveryGuidanceOpen) return;
        const index = Number(event.key) - 1;
        if (index < 0 || index >= actionCount) return;
        event.preventDefault();
        if (immediateDecision) {
          const action = toolActions[index];
          if (action) activateAction(action, index);
          return;
        }
        setSelectedIndex(index);
      } else if (event.key === "Escape") {
        event.preventDefault();
        answerWithExit(onStop);
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [actionCount, activateAction, confirmSelected, onStop, submitting, isPlanApproval, isRecoveryApproval, isRecoveryPlanChange, recoveryGuidanceOpen, toolActions]);

  useEffect(() => {
    revisionActiveRef.current = revisionOpen;
    onRevisionActiveChangeRef.current?.(revisionOpen);
    if (revisionOpen) inputRef.current?.focus();
  }, [revisionOpen]);

  useEffect(() => () => {
    if (revisionActiveRef.current) onRevisionActiveChangeRef.current?.(false);
  }, []);

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

  const closeRecoveryGuidance = () => {
    setRecoveryGuidanceOpen(false);
    setRecoveryGuidanceText("");
    requestAnimationFrame(() => {
      if (isRecoveryPlanChange) cardRef.current?.focus();
      else recoveryGuidanceTriggerRef.current?.focus();
    });
  };

  const submitRecoveryGuidance = () => {
    const text = recoveryGuidanceText.trim();
    if (!text) {
      recoveryGuidanceRef.current?.focus();
      return;
    }
    answerWithExit(() => resolveRecovery("revise", text));
  };

  const onRecoveryGuidanceKeyDown = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
      event.preventDefault();
      event.stopPropagation();
      submitRecoveryGuidance();
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      closeRecoveryGuidance();
      return;
    }
    event.stopPropagation();
  };

  const recoveryReason = isRecoveryApproval
    ? recoveryReasonText(
        recovery?.change_kind,
        recovery?.change_rationale || recovery?.review_rationale || reason,
        t,
      )
    : "";
  const recoveryActionSummary =
    recovery?.next_action ||
    recovery?.next_tool ||
    subjectSummary ||
    approval.tool;
  const recoveryPlanDelta = isRecoveryPlanChange
    ? planDelta(recovery?.plan_before, recovery?.plan_after)
    : null;
  const hasRecoveryDetails = Boolean(
    recovery?.failed_summary ||
    recovery?.diagnosis ||
    recovery?.change_rationale ||
    recovery?.review_rationale ||
    recovery?.source_agent,
  );

  const confirmIsDanger = selectedAction?.tone === "danger";
  const confirmLabel =
    selectedAction?.kind === "toggle-revision"
      ? revisionOpen
        ? t("common.cancel")
        : t("approval.revisePlan")
      : t("decision.confirm");

  return (
    <div ref={shelfRef}>
      <PromptShelf
        decision
        actionsRole={isPlanApproval || isRecoveryApproval ? "group" : "listbox"}
        className={isPlanApproval ? "prompt-shelf--plan-approval" : isRecoveryApproval ? "prompt-shelf--recovery-approval" : "prompt-shelf--tool-approval"}
        barRef={cardRef}
        titleId={isPlanApproval ? "plan-approval-title" : isRecoveryApproval ? "recovery-approval-title" : "tool-approval-title"}
        title={
          isPlanApproval
            ? t("approval.planReady")
            : isRecoveryPlanChange
              ? t("approval.recoveryPlanChangePending")
              : isRecoveryApproval
                ? t("approval.recoveryPending")
                : t("approval.toolPending")
        }
        badges={
          <>
            {!isPlanApproval && !isRecoveryApproval && <PromptBadge tone="amber">{toolLabel}</PromptBadge>}
            {isPlanApproval && revisionOpen && <PromptBadge>{t("approval.revisePlan")}</PromptBadge>}
            {isRecoveryPlanChange && (
              <PromptBadge>
                {t(recoveryChangeKind === "strategy" ? "approval.recoveryDecisionStrategy" : "approval.recoveryDecisionScope")}
              </PromptBadge>
            )}
          </>
        }
        meta={isRecoveryApproval ? undefined : toolMeta}
        headerActions={
          <>
            {isRecoveryApproval && hasRecoveryDetails && (
              <PromptHeaderAction onClick={() => setReasonOpen((open) => !open)} disabled={submitting}>
                {t(reasonOpen ? "approval.recoveryHideTechnicalDetails" : "approval.recoveryTechnicalDetails")}
              </PromptHeaderAction>
            )}
            {!isPlanApproval && !isRecoveryApproval && hasToolDetails && reason && (
              <PromptHeaderAction onClick={() => setReasonOpen((open) => !open)} disabled={submitting}>
                {t(reasonOpen ? "approval.hideDetails" : "approval.details")}
              </PromptHeaderAction>
            )}
            {!isPlanApproval && !isRecoveryApproval && (
              <PromptHeaderAction
                onClick={() => answerWithExit(onStop)}
                ariaLabel={t("decision.stopTask")}
                disabled={submitting}
              >
                {t("decision.stopTask")}
              </PromptHeaderAction>
            )}
          </>
        }
        actions={
          <>
            {toolActions.map((action, index) => {
              const actionNode = (
                <PromptAction
                  key={action.key}
                  keyLabel={action.key}
                  label={action.label}
                  description={action.desc}
                  onClick={() => {
                    activateAction(action, index);
                  }}
                  primary={action.primary}
                  selected={selectedIndex === index}
                  tone={action.tone}
                  role={isPlanApproval || isRecoveryApproval ? "button" : "option"}
                  disabled={submitting}
                  title={action.desc}
                />
              );
              if (isRecoveryApproval && !isRecoveryPlanChange && index === 1 && recovery?.can_grant_task) {
                return (
                  <div
                    key={action.key}
                    className={[
                      "recovery-continue-option",
                      grantSimilarForTask ? "recovery-continue-option--granted" : "",
                    ].filter(Boolean).join(" ")}
                  >
                    {actionNode}
                    {!recoveryGuidanceOpen && (
                      <label className="recovery-task-grant">
                        <input
                          type="checkbox"
                          checked={grantSimilarForTask}
                          onChange={(event) => setGrantSimilarForTask(event.target.checked)}
                          disabled={submitting}
                        />
                        <span>
                          <strong>{t("approval.recoveryTaskGrant")}</strong>
                          <small>
                            {taskGrantScope ? (
                              <>
                                {t("approval.recoveryTaskGrantScope")} <code>{taskGrantScope}</code>
                              </>
                            ) : t("approval.recoveryTaskGrantDesc")}
                          </small>
                        </span>
                      </label>
                    )}
                  </div>
                );
              }
              return actionNode;
            })}
          </>
        }
        note={
          isRecoveryApproval ? (
            recoveryGuidanceOpen ? (
              <div className="recovery-guidance">
                <textarea
                  ref={recoveryGuidanceRef}
                  className="plan-revision__input recovery-guidance__input"
                  value={recoveryGuidanceText}
                  rows={3}
                  maxLength={RECOVERY_FEEDBACK_MAX}
                  aria-label={t("approval.recoveryGuidanceLabel")}
                  placeholder={t("approval.recoveryGuidancePlaceholder")}
                  onChange={(event) => setRecoveryGuidanceText(event.target.value.slice(0, RECOVERY_FEEDBACK_MAX))}
                  onKeyDown={onRecoveryGuidanceKeyDown}
                  disabled={submitting}
                  autoFocus
                />
                <div className="recovery-guidance__actions">
                  <button className="btn" type="button" onClick={closeRecoveryGuidance} disabled={submitting}>
                    {t("common.cancel")}
                  </button>
                  <button
                    className="btn btn--primary"
                    type="button"
                    onClick={submitRecoveryGuidance}
                    disabled={submitting || !recoveryGuidanceText.trim()}
                  >
                    {t(isRecoveryPlanChange ? "approval.recoveryPlanGuidanceSubmit" : "approval.recoveryGuidanceSubmit")}
                  </button>
                </div>
              </div>
            ) : isRecoveryPlanChange ? undefined : (
              <button
                ref={recoveryGuidanceTriggerRef}
                type="button"
                className="recovery-guidance-trigger"
                aria-expanded="false"
                onClick={() => {
                  // Guidance rejects the pending action; a task-scoped grant
                  // belongs only to Continue and would be misleading here.
                  setGrantSimilarForTask(false);
                  setRecoveryGuidanceOpen(true);
                }}
                disabled={submitting}
              >
                {t("approval.recoveryGuidanceTrigger")}
              </button>
            )
          ) : undefined
        }
        footer={
          isRecoveryApproval || isPlanApproval ? undefined : (
            <DecisionConfirmBar
              hint={t("decision.selectHint")}
              confirmLabel={confirmLabel}
              onConfirm={confirmSelected}
              disabled={submitting}
              danger={confirmIsDanger}
            />
          )
        }
      >
        {(approvalModeRelaxed ||
          isRecoveryApproval ||
          (!isPlanApproval && !isRecoveryApproval && (subject || (reasonOpen && reason))) ||
          (isPlanApproval && revisionOpen)) && (
          <>
            {approvalModeRelaxed && !isRecoveryApproval && (
              <div className="approval-mode-hint">{t("approval.modeSwitchPendingHint")}</div>
            )}
            {isRecoveryApproval && (
              <section className="recovery-summary" aria-label={t("approval.recoverySummaryLabel")}>
                <p className="recovery-summary__reason">{recoveryReason}</p>
                {!isRecoveryPlanChange && recoveryActionSummary && (
                  <p className="recovery-summary__action">
                    <span>{t("approval.recoveryNextLabel")}</span>
                    <code>{recoveryActionSummary}</code>
                  </p>
                )}
              </section>
            )}
            {isRecoveryPlanChange && recoveryPlanDelta && (
              <section className="plan-change-delta" aria-label={t("approval.recoveryPlanDeltaLabel")}>
                <div className="plan-change-delta__title">{t("approval.recoveryPlanDeltaLabel")}</div>
                {recoveryPlanDelta.removed.length > 0 && (
                  <div className="plan-change-delta__group plan-change-delta__group--removed">
                    <div className="plan-change-delta__label">{t("approval.recoveryPlanRemoved")}</div>
                    {recoveryPlanDelta.removed.map((line, index) => (
                      <div className="plan-change-delta__line" key={`removed-${index}-${line}`}><span>−</span>{line}</div>
                    ))}
                  </div>
                )}
                {recoveryPlanDelta.added.length > 0 && (
                  <div className="plan-change-delta__group plan-change-delta__group--added">
                    <div className="plan-change-delta__label">{t("approval.recoveryPlanAdded")}</div>
                    {recoveryPlanDelta.added.map((line, index) => (
                      <div className="plan-change-delta__line" key={`added-${index}-${line}`}><span>+</span>{line}</div>
                    ))}
                  </div>
                )}
              </section>
            )}
            {isRecoveryApproval && reasonOpen && (
              <dl className="approval-details recovery-details">
                {recovery?.failed_summary && (
                  <div className="recovery-detail-row">
                    <dt>{t("approval.recoveryFailedLabel")}</dt>
                    <dd>
                      {recovery.failed_tool && <code>{recovery.failed_tool}</code>}
                      {recovery.failed_tool && " · "}
                      {recovery.failed_summary}
                    </dd>
                  </div>
                )}
                {recovery?.diagnosis && (
                  <div className="recovery-detail-row">
                    <dt>{t("approval.recoveryDiagnosisLabel")}</dt>
                    <dd>{recovery.diagnosis}</dd>
                  </div>
                )}
                {(recovery?.change_rationale || recovery?.review_rationale) && (
                  <div className="recovery-detail-row">
                    <dt>{t("approval.recoveryWhyLabel")}</dt>
                    <dd>{recovery.change_rationale || recovery.review_rationale}</dd>
                  </div>
                )}
                {recovery?.source_agent && (
                  <div className="recovery-detail-row">
                    <dt>{t("approval.recoverySourceLabel")}</dt>
                    <dd><code>{recovery.source_agent}</code></dd>
                  </div>
                )}
              </dl>
            )}
            {!isPlanApproval && !isRecoveryApproval && subject && (
              <div className="approval-details">
                <pre className="approval-subject">{subject}</pre>
                {reasonOpen && reason && <div className="approval-reason">{reason}</div>}
              </div>
            )}
            {isPlanApproval && revisionOpen && (
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
                  disabled={submitting}
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
                  <button className="btn" type="button" onClick={() => setRevisionOpen(false)} disabled={submitting}>
                    {t("common.cancel")}
                  </button>
                  <button className="btn btn--primary" type="button" onClick={submitRevision} disabled={submitting}>
                    {t("approval.sendRevision")}
                  </button>
                </div>
              </div>
            )}
          </>
        )}
      </PromptShelf>
    </div>
  );
}
