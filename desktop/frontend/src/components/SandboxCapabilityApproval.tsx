import { useCallback, useEffect, useRef, useState } from "react";
import gsap from "gsap";
import { useT, type Translator } from "../lib/i18n";
import type { WireSandboxCapabilityPrompt } from "../lib/types";
import {
  DecisionConfirmBar,
  PromptAction,
  PromptBadge,
  PromptHeaderAction,
  PromptShelf,
} from "./PromptShelf";
import { SandboxCapabilityCard } from "./SandboxCapabilityCard";
import { DUR_FAST } from "../lib/gsapAnimations";

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

type DecisionAction = {
  key: string;
  label: string;
  desc: string;
  tone?: "default" | "danger";
  primary?: boolean;
  kind: "submit";
};

function buildActions(prompt: WireSandboxCapabilityPrompt, t: Translator): DecisionAction[] {
  const actions: DecisionAction[] = [
    { key: "allow_once", label: t("approval.sandboxCapabilityAllowOnce"), desc: t("approval.allowOnceDesc"), kind: "submit" },
  ];
  if (prompt.reusable) {
    actions.push({ key: "allow_session", label: t("approval.sandboxCapabilityAllowSession"), desc: t("approval.allowRuleSessionDesc"), kind: "submit" });
    if (!prompt.suspected_secret) {
      actions.push({ key: "allow_persistent", label: t("approval.sandboxCapabilityAllowPersistent"), desc: t("approval.allowRulePersistentDesc"), kind: "submit" });
    }
  }
  actions.push({ key: "run_sandboxed", label: t("approval.sandboxCapabilityRunSandboxed"), desc: t("approval.denyDesc"), tone: "danger", kind: "submit" });
  return actions;
}

interface Props {
  sandboxCapability: WireSandboxCapabilityPrompt;
  onResolve: (action: string) => void;
}

export function SandboxCapabilityApproval({ sandboxCapability: sc, onResolve }: Props) {
  const t = useT();
  const actions = buildActions(sc, t);
  const actionCount = actions.length;
  const [reasonOpen, setReasonOpen] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [submitting, setSubmitting] = useState(false);
  const closingRef = useRef(false);
  const selectedIndexRef = useRef(selectedIndex);
  selectedIndexRef.current = selectedIndex;
  const shelfRef = useRef<HTMLDivElement | null>(null);
  const cardRef = useRef<HTMLDivElement | null>(null);

  const answerWithExit = useCallback((fn: () => void) => {
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
  }, [submitting]);

  const confirmSelected = useCallback(() => {
    if (submitting || closingRef.current) return;
    const action = actions[selectedIndexRef.current];
    if (!action) return;
    answerWithExit(() => onResolve(action.key));
  }, [submitting, actions, answerWithExit, onResolve]);

  const activateAction = useCallback((_action: DecisionAction, index: number) => {
    if (submitting) return;
    setSelectedIndex(index);
  }, [submitting]);

  // Keyboard handler
  useEffect(() => {
    if (submitting) return;
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const target = event.target instanceof Element ? event.target : null;
      const tag = target?.tagName.toLowerCase();
      const editing = tag === "input" || tag === "textarea" || tag === "select" ||
        (target instanceof HTMLElement && target.isContentEditable);
      if (editing) return;

      if (event.key === "ArrowUp") {
        event.preventDefault();
        setSelectedIndex((i) => ((i < 0 ? 0 : i) - 1 + actionCount) % actionCount);
      } else if (event.key === "ArrowDown") {
        event.preventDefault();
        setSelectedIndex((i) => (i < 0 ? 0 : i + 1) % actionCount);
      } else if (event.key === "Enter") {
        event.preventDefault();
        confirmSelected();
      } else if (event.key === "1" || event.key === "2" || event.key === "3" || event.key === "4") {
        const index = Number(event.key) - 1;
        if (index >= 0 && index < actionCount) {
          event.preventDefault();
          setSelectedIndex(index);
        }
      } else if (event.key === "Escape") {
        // Blocking: sandbox capability cannot be dismissed via Escape
        event.preventDefault();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [submitting, actionCount, confirmSelected]);

  return (
    <div ref={shelfRef}>
      <PromptShelf
        decision
        actionsRole="listbox"
        className="prompt-shelf--sandbox-capability"
        barRef={cardRef}
        titleId="sandbox-capability-title"
        title={t("approval.sandboxCapabilityTitle")}
        badges={<PromptBadge tone="danger">{t("approval.sandboxCapabilityBadge")}</PromptBadge>}
        meta={sc.canonical_executable}
        headerActions={
          <>
            <PromptHeaderAction onClick={() => setReasonOpen((o) => !o)} disabled={submitting}>
              {t(reasonOpen ? "approval.hideDetails" : "approval.details")}
            </PromptHeaderAction>
            <PromptHeaderAction
              onClick={() => answerWithExit(() => onResolve("cancel_command"))}
              disabled={submitting}
            >
              {t("approval.sandboxCapabilityCancel")}
            </PromptHeaderAction>
          </>
        }
        actions={
          <>
            {actions.map((action, index) => (
              <PromptAction
                key={action.key}
                keyLabel={action.key}
                label={action.label}
                description={action.desc}
                onClick={() => activateAction(action, index)}
                primary={action.primary}
                selected={selectedIndex === index}
                tone={action.tone}
                role="option"
                disabled={submitting}
                title={action.desc}
              />
            ))}
          </>
        }
        footer={
          <DecisionConfirmBar
            hint={t("decision.selectHint")}
            confirmLabel={t("decision.confirm")}
            onConfirm={confirmSelected}
            disabled={submitting}
          />
        }
      >
        {reasonOpen ? (
          <SandboxCapabilityCard sandboxCapability={sc} />
        ) : (
          <div className="sandbox-capability-card">
            {(sc.preserve_background_processes || sc.review.risk.level === "critical") && <>
              {sc.preserve_background_processes && (
                <div className="sandbox-capability-card__danger-banner">
                  <span className="sandbox-capability-card__danger-banner-icon">⚠</span>
                  <span>{t("approval.sandboxCapabilityPreserveWarning")}</span>
                </div>
              )}
              {sc.review.risk.level === "critical" && (
                <div className="sandbox-capability-card__danger-banner sandbox-capability-card__danger-banner--critical">
                  <span className="sandbox-capability-card__danger-banner-icon">⚠</span>
                  <span>{t("approval.sandboxCapabilityCriticalRiskWarning")}</span>
                </div>
              )}
            </>}
          </div>
        )}
      </PromptShelf>
    </div>
  );
}
