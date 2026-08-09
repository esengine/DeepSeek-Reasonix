// Run: tsx src/__tests__/decision-surface.test.tsx
//
// Decision surfaces: ordinary approvals stay select-then-confirm while Plan
// and Auto boundary cards use immediate buttons; no double submit.

import { readFileSync } from "node:fs";
import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ApprovalModal } from "../components/ApprovalModal";
import { ClearContextCard } from "../components/ClearContextCard";
import { RuntimeDecisionCard } from "../components/RuntimeDecisionCard";
import { LocaleProvider } from "../lib/i18n";
import {
  DECISION_SURFACE_MOCK_TRIGGERS,
  LONG_DECISION_OPTIONS_MOCK_TRIGGER,
  decisionSurfaceMockFromInput,
  isLongDecisionOptionsMockInput,
} from "../lib/decisionSurfaceMock";
import type { WireApproval } from "../lib/types";

const styles = readFileSync(new URL("../styles.css", import.meta.url), "utf8");

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function flushTimers(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function installDom(language = "en-US", descriptionOverflows = true) {
  const dom = new JSDOM("<!doctype html><html><head></head><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(dom.window.navigator, "language", { configurable: true, value: language });
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.HTMLTextAreaElement = dom.window.HTMLTextAreaElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(dom.window.HTMLElement.prototype, "clientHeight", {
    configurable: true,
    get() {
      return this.classList.contains("prompt-action__desc") ? 42 : 0;
    },
  });
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollHeight", {
    configurable: true,
    get() {
      return this.classList.contains("prompt-action__desc")
        ? (descriptionOverflows ? 84 : 42)
        : 0;
    },
  });
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollIntoView", {
    configurable: true,
    value() {
      this.setAttribute("data-scrolled-into-view", "true");
    },
  });
  const style = document.createElement("style");
  style.textContent = styles;
  document.head.appendChild(style);
  return dom;
}

console.log("\ndecision surface");

eq(Object.keys(DECISION_SURFACE_MOCK_TRIGGERS).length, 7, "QA stress scenes do not change the seven product decision surfaces");
for (const [kind, trigger] of Object.entries(DECISION_SURFACE_MOCK_TRIGGERS)) {
  eq(decisionSurfaceMockFromInput(trigger), kind, `${kind} has a distinct canonical browser mock trigger`);
}
eq(decisionSurfaceMockFromInput("mock 工作区冲突"), "workspace_conflict", "Chinese mock phrases remain convenient for visual QA");
eq(decisionSurfaceMockFromInput("/approve-preview"), "tool_approval", "legacy approval preview trigger remains compatible");
eq(isLongDecisionOptionsMockInput(LONG_DECISION_OPTIONS_MOCK_TRIGGER), true, "long-option QA has a canonical browser trigger");
eq(isLongDecisionOptionsMockInput("mock 长文案选项"), true, "long-option QA has a convenient Chinese trigger");
eq(decisionSurfaceMockFromInput(LONG_DECISION_OPTIONS_MOCK_TRIGGER), null, "long-option QA is not counted as an eighth product surface");

// Plan exposes start, revise, and leave-without-executing as direct buttons so
// declining the current plan never traps the user in Plan mode.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const answers: Array<[boolean, boolean, boolean]> = [];
  const revisions: string[] = [];
  let exits = 0;
  const approval: WireApproval = {
    id: "plan-1",
    tool: "exit_plan_mode",
    subject: "Plan ready",
  };

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ApprovalModal
          approval={approval}
          onAnswer={(a, s, p) => answers.push([a, s, p])}
          onRevisePlan={(text) => revisions.push(text)}
          onExitPlan={() => { exits += 1; }}
          onStop={() => undefined}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const actions = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLButtonElement[];
  eq(actions.length, 3, "Plan has start, revise, and exit-without-executing actions");
  eq(document.querySelector(".prompt-shelf__actions")?.getAttribute("role"), "group", "Plan actions use button-group semantics");
  ok(actions.every((action) => action.getAttribute("role") === "button"), "Plan actions are announced as buttons");
  ok(!document.querySelector(".decision-confirm-bar__confirm"), "Plan has no redundant confirm button");
  ok(actions[2].textContent?.includes("Exit without executing"), "Plan card exposes a clear non-executing exit");
  ok(!document.body.textContent?.includes("Stop task"), "Plan card relies on the global Stop control");
  const planDescriptionToggle = document.querySelector(".prompt-action-row .prompt-action__description-toggle") as HTMLButtonElement | null;
  if (!planDescriptionToggle) throw new Error("Plan description disclosure did not render");
  await act(async () => {
    planDescriptionToggle.click();
    await flushTimers();
  });
  eq(answers.length, 0, "expanding a Plan description never starts execution");
  eq(planDescriptionToggle.getAttribute("aria-expanded"), "true", "Plan disclosure announces its expanded state");

  await act(async () => {
    actions[1].click();
    await flushTimers();
  });
  ok(document.querySelector(".plan-revision__input") != null, "Revise opens the inline editor in one click");
  eq(answers.length, 0, "Opening revision does not start execution");

  await act(async () => {
    actions[2].click();
    actions[2].click();
    await flushTimers(220);
  });
  eq(exits, 1, "Exit without executing runs once and ignores a double click");
  eq(answers.length, 0, "Exit without executing never approves plan execution");
  eq(revisions.length, 0, "Exit without executing does not submit a plan revision");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  let exits = 0;

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ApprovalModal
          approval={{ id: "plan-exit-key", tool: "exit_plan_mode", subject: "Plan ready" }}
          onAnswer={() => undefined}
          onExitPlan={() => { exits += 1; }}
          onStop={() => undefined}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "3", bubbles: true }));
    await flushTimers(220);
  });
  eq(exits, 1, "number key 3 exits Plan without execution");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const answers: Array<[boolean, boolean, boolean]> = [];

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ApprovalModal
          approval={{ id: "plan-start", tool: "exit_plan_mode", subject: "Plan ready" }}
          onAnswer={(a, s, p) => answers.push([a, s, p])}
          onStop={() => undefined}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const start = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")]
    .find((action) => action.textContent?.includes("Start execution")) as HTMLButtonElement;
  await act(async () => {
    start.click();
    start.click();
    await flushTimers(220);
  });
  eq(answers.length, 1, "Plan starts with one click and ignores double submit");
  eq(JSON.stringify(answers[0]), JSON.stringify([true, false, false]), "Plan start approves execution once");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Tool approval: click only selects; confirm submits once; double-confirm ignored.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const answers: Array<[boolean, boolean, boolean]> = [];
  const approval: WireApproval = {
    id: "bash-1",
    tool: "bash",
    subject: "ls -la",
  };

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ApprovalModal
          approval={approval}
          onAnswer={(a, s, p) => answers.push([a, s, p])}
          onStop={() => undefined}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const actions = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLButtonElement[];
  eq(actions.length, 4, "ordinary tool approval has four options");
  ok(actions[0].classList.contains("prompt-action--selected"), "default selection is allow once");
  ok(Boolean(actions[0].title), "tool approval keeps the complete option description in a desktop tooltip");
  const toolDescriptionToggle = document.querySelector(".prompt-shelf__footnote .prompt-action__description-toggle") as HTMLButtonElement | null;
  if (!toolDescriptionToggle) throw new Error("tool approval description disclosure did not render");
  const toolContent = document.querySelector(".prompt-shelf__content") as HTMLElement | null;
  const toolFooter = document.querySelector(".prompt-shelf__footer") as HTMLElement | null;
  if (!toolContent || !toolFooter) throw new Error("tool approval scroll layout did not render");
  eq(window.getComputedStyle(toolContent).overflow, "auto", "tool approval uses the shared decision scroller");
  eq(toolContent.contains(toolFooter), false, "tool approval confirm footer stays visible outside the scroller");
  await act(async () => {
    toolDescriptionToggle.dispatchEvent(new window.KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    }));
    await flushTimers();
  });
  eq(answers.length, 0, "Enter on tool disclosure never approves the default permission");
  eq(toolDescriptionToggle.getAttribute("aria-expanded"), "true", "Enter expands tool approval copy");
  await act(async () => {
    toolDescriptionToggle.dispatchEvent(new window.KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    }));
    await flushTimers();
  });
  eq(toolDescriptionToggle.getAttribute("aria-expanded"), "false", "Enter collapses tool approval copy again");
  await act(async () => {
    toolDescriptionToggle.click();
    await flushTimers();
  });
  eq(answers.length, 0, "expanding tool approval copy never submits the selected permission");
  eq(toolDescriptionToggle.getAttribute("aria-expanded"), "true", "tool approval disclosure announces its expanded state");

  await act(async () => {
    actions[3].click();
    await flushTimers();
  });
  eq(answers.length, 0, "clicking deny only selects");
  ok(actions[3].classList.contains("prompt-action--selected"), "deny becomes selected");

  const confirm = document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement;
  await act(async () => {
    confirm.click();
    confirm.click();
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    await flushTimers(220);
  });
  eq(answers.length, 1, "double click/enter submits only once");
  eq(JSON.stringify(answers[0]), JSON.stringify([false, false, false]), "deny maps to (false,false,false)");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Auto reuses the decision shelf with one-click continue or revise. Task
// cancellation stays on the ordinary Stop control instead of becoming a third
// recovery-specific branch. Details stay collapsed; no select-then-confirm.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const decisions: Array<{ action: string; feedback?: string }> = [];
  const approval: WireApproval = {
    id: "guard-1",
    tool: "bash",
    subject: "git push origin feature",
    kind: "recovery",
    recovery: {
      next_action: "git push origin feature",
      change_kind: "risk",
      can_grant_task: true,
      task_grant_scope: "git push origin → feature",
    },
  };

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ApprovalModal
          approval={approval}
          onAnswer={() => undefined}
          onResolveRecovery={(action, feedback) => decisions.push({ action, feedback })}
          onStop={() => undefined}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const actions = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLButtonElement[];
  eq(actions.length, 2, "Auto recovery has continue and try-another actions");
  eq(document.querySelector(".prompt-shelf__actions")?.getAttribute("role"), "group", "Auto boundary actions use button-group semantics");
  ok(actions.every((action) => action.getAttribute("role") === "button"), "Auto boundary actions are announced as buttons");
  ok(!actions.some((action) => action.textContent?.includes("Stop task")), "Auto recovery does not add a third Stop decision");
  ok(!document.body.textContent?.includes("Stop task"), "Auto boundary card relies on the global Stop control");
  ok(!document.querySelector(".decision-confirm-bar__confirm"), "Auto recovery has no select-then-confirm bar");
  ok(document.body.textContent?.includes("Action needs confirmation"), "Auto boundary uses plain confirmation copy");
  ok(!document.body.textContent?.includes("Auto needs"), "Auto boundary hides the internal mechanism name");
  ok(!document.body.textContent?.includes("checkpoint"), "UI hides internal checkpoint terms");
  ok(!document.body.textContent?.includes("same_strategy"), "UI hides internal reviewer terms");
  ok(actions[0].textContent?.includes("Try another approach (recommended)"), "safer action is first and explicitly recommended");
  ok(actions[0].classList.contains("prompt-action--selected"), "recommended recovery action has primary emphasis");
  ok(actions[1].textContent?.includes("Continue once"), "one-shot override remains available as the secondary action");
  ok(document.querySelector(".recovery-summary"), "Auto boundary shows one concise summary by default");
  ok(document.body.textContent?.includes("may affect an external system"), "summary explains the user-visible risk");
  eq(document.body.textContent?.split("git push origin feature").length, 2, "pending action is shown once by default");
  ok(!document.querySelector(".recovery-details"), "details stay collapsed by default");
  const guidanceTrigger = document.querySelector(".recovery-guidance-trigger") as HTMLButtonElement;
  ok(guidanceTrigger, "custom requirements stay available as a quiet progressive-disclosure link");
  ok(guidanceTrigger.textContent?.includes("Tell Auto"), "guidance link uses plain user-facing copy");
  ok(!document.querySelector(".recovery-guidance__input"), "custom requirements editor stays collapsed by default");
  eq(actions.length, 2, "custom requirements do not become a third decision card");
  const recoveryDescriptionToggle = document.querySelector(".prompt-action-row .prompt-action__description-toggle") as HTMLButtonElement | null;
  if (!recoveryDescriptionToggle) throw new Error("recovery description disclosure did not render");
  const recoveryCard = document.querySelector(".prompt-shelf--recovery-approval .prompt-shelf__card") as HTMLElement | null;
  const recoveryContent = document.querySelector(".prompt-shelf--recovery-approval .prompt-shelf__content") as HTMLElement | null;
  const recoveryActions = document.querySelector(".prompt-shelf--recovery-approval .prompt-shelf__actions") as HTMLElement | null;
  if (!recoveryCard || !recoveryContent || !recoveryActions) throw new Error("recovery height bounds did not render");
  eq(window.getComputedStyle(recoveryCard).maxHeight, "min(62vh, 560px)", "recovery card stays bounded by the viewport");
  eq(window.getComputedStyle(recoveryCard).overflow, "hidden", "recovery card clips only at its shared scroll boundary");
  eq(window.getComputedStyle(recoveryContent).overflow, "auto", "recovery content uses one internal scroller");
  eq(window.getComputedStyle(recoveryActions).maxHeight, "none", "recovery options avoid a nested height cap");
  eq(window.getComputedStyle(recoveryActions).overflow, "visible", "recovery option text remains available in the shared scroller");
  await act(async () => {
    recoveryDescriptionToggle.click();
    await flushTimers();
  });
  eq(decisions.length, 0, "expanding recovery details never resolves the pending action");
  const taskGrant = document.querySelector(".recovery-task-grant input") as HTMLInputElement;
  ok(taskGrant, "bounded recovery offers a current-task semantic grant");
  ok(!taskGrant.checked, "task grant is opt-in");
  ok(document.querySelector(".recovery-continue-option .recovery-task-grant"), "task grant is grouped with Continue");
  ok(document.body.textContent?.includes("git push origin → feature"), "task grant shows the exact host-classified scope");

  await act(async () => {
    actions[1].click();
    actions[1].click();
    await flushTimers(220);
  });
  eq(decisions.length, 1, "double click submits only once");
  eq(decisions[0]?.action, "continue", "continue resolves only the waiting action");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Specific requirements are one-shot guidance for finding another approach.
// They never inherit a checked task grant or become approval to execute.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const decisions: Array<{ action: string; feedback?: string }> = [];
  const approval: WireApproval = {
    id: "guard-guidance",
    tool: "bash",
    subject: "git push origin feature",
    kind: "recovery",
    recovery: {
      next_action: "git push origin feature",
      change_kind: "risk",
      can_grant_task: true,
    },
  };

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ApprovalModal
          approval={approval}
          onAnswer={() => undefined}
          onResolveRecovery={(action, feedback) => decisions.push({ action, feedback })}
          onStop={() => undefined}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const openGuidance = async () => {
    await act(async () => {
      (document.querySelector(".recovery-guidance-trigger") as HTMLButtonElement).click();
      await flushTimers();
    });
  };
  ok(
    document.body.textContent?.includes("Only the same operation type and target boundary are reused"),
    "older recovery events without a display scope retain the generic safety explanation",
  );
  await openGuidance();

  let input = document.querySelector(".recovery-guidance__input") as HTMLTextAreaElement;
  ok(input != null, "guidance link expands an inline text area");
  eq(input.maxLength, 1000, "guidance input exposes the same 1000-character client limit as submission");
  ok(input.placeholder.includes("only edit the current file"), "placeholder demonstrates a concrete constraint");
  ok(input === document.activeElement, "expanded guidance receives focus");
  ok((document.querySelector(".recovery-guidance__actions .btn--primary") as HTMLButtonElement).disabled, "empty guidance cannot submit");
  eq(document.querySelectorAll(".prompt-shelf__actions .prompt-action").length, 2, "expanded guidance preserves the two primary decisions");

  await act(async () => {
    input.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await flushTimers(25);
  });
  ok(!document.querySelector(".recovery-guidance__input"), "Escape collapses custom guidance");
  ok(document.querySelector(".recovery-guidance-trigger") === document.activeElement, "Escape restores focus to the guidance link");
  eq(decisions.length, 0, "collapsing guidance does not answer the confirmation");

  await act(async () => {
    (document.querySelector(".recovery-task-grant input") as HTMLInputElement).click();
    await flushTimers();
  });
  await openGuidance();
  ok(!document.querySelector(".recovery-task-grant"), "guidance hides and clears the unrelated Continue task grant");
  input = document.querySelector(".recovery-guidance__input") as HTMLTextAreaElement;
  const feedback = "Only edit the current file; do not push.";
  await act(async () => {
    const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLTextAreaElement.prototype, "value")?.set;
    setter?.call(input, `  ${feedback}  `);
    input.dispatchEvent(new dom.window.InputEvent("input", { bubbles: true, inputType: "insertText", data: feedback }));
    input.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
    input.dispatchEvent(new dom.window.KeyboardEvent("keyup", { key: ".", bubbles: true }));
    await flushTimers();
  });
  ok(!(document.querySelector(".recovery-guidance__actions .btn--primary") as HTMLButtonElement).disabled, "non-empty guidance enables submission");

  await act(async () => {
    input.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Enter", ctrlKey: true, bubbles: true }));
    await flushTimers(220);
  });
  eq(decisions.length, 1, "Ctrl+Enter submits custom requirements once");
  eq(decisions[0]?.action, "revise", "custom requirements always choose another approach");
  eq(decisions[0]?.feedback, feedback, "custom requirements are trimmed and forwarded exactly once");

  await act(async () => root.unmount());
  dom.window.close();
}

// The optional semantic grant is explicit and maps to a distinct backend
// action; it is never inferred from a raw command match.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const decisions: Array<{ action: string; feedback?: string }> = [];
  const approval: WireApproval = {
    id: "guard-task-grant",
    tool: "bash",
    subject: "git push origin feature",
    kind: "recovery",
    recovery: {
      next_action: "git push origin feature",
      change_kind: "risk",
      can_grant_task: true,
      task_grant_scope: "git push origin → feature",
    },
  };

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ApprovalModal
          approval={approval}
          onAnswer={() => undefined}
          onResolveRecovery={(action, feedback) => decisions.push({ action, feedback })}
          onStop={() => undefined}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const taskGrant = document.querySelector(".recovery-task-grant input") as HTMLInputElement;
  const continueButton = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")]
    .find((action) => action.textContent?.includes("Continue once")) as HTMLButtonElement;
  await act(async () => {
    taskGrant.click();
    await flushTimers();
  });
  ok(continueButton.textContent?.includes("Continue and remember for this task"), "checked grant updates the action label before consent");
  ok(!continueButton.textContent?.includes("Continue once"), "checked grant no longer looks like a one-shot action");
  await act(async () => {
    continueButton.click();
    await flushTimers(220);
  });
  eq(decisions[0]?.action, "continue_task", "checked semantic grant uses the task-scoped recovery action");

  await act(async () => root.unmount());
  dom.window.close();
}

// A reviewer-confirmed scope/strategy transition is presented as a plan-level
// choice rather than another low-level tool warning.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const decisions: Array<{ action: string; feedback?: string }> = [];
  const approval: WireApproval = {
    id: "guard-2",
    tool: "todo_write",
    subject: "Update the active execution plan",
    kind: "recovery",
    recovery: {
      next_action: "Update the active execution plan",
      change_kind: "scope",
      change_rationale: "Publishing the migration changes the product scope.",
      plan_before: "1. Keep the public API [in_progress]\n2. Update the implementation [pending]",
      plan_after: "1. Replace the public API [in_progress]\n2. Update the implementation [pending]\n3. Update the migration guide [pending]",
    },
  };

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ApprovalModal
          approval={approval}
          onAnswer={() => undefined}
          onResolveRecovery={(action, feedback) => decisions.push({ action, feedback })}
          onStop={() => undefined}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  let actions = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLButtonElement[];
  ok(document.body.textContent?.includes("The execution plan needs your decision"), "material scope change uses a neutral plan-level title");
  ok(document.body.textContent?.includes("Scope choice"), "plan card names the user-owned decision class");
  ok(document.body.textContent?.includes("Removed from the previous plan"), "plan card identifies removed steps");
  ok(document.body.textContent?.includes("Keep the public API"), "plan card shows the removed step");
  ok(document.body.textContent?.includes("Added to the new plan"), "plan card identifies added steps");
  ok(document.body.textContent?.includes("Replace the public API"), "plan card shows the replacement step");
  ok(document.body.textContent?.includes("Update the migration guide"), "plan card shows newly added scope");
  ok(!document.body.textContent?.includes("[in_progress]"), "plan delta omits progress-only status noise");
  ok(actions[0].textContent?.includes("Adopt the new plan and continue"), "plan adoption remains explicit");
  ok(actions[1].textContent?.includes("Do not adopt; tell Auto how to adjust"), "plan rejection opens a guided revision path");
  ok(!actions[0].classList.contains("prompt-action--selected"), "plan adoption is not visually preselected");
  ok(!actions[1].classList.contains("prompt-action--selected"), "plan adjustment is not visually preselected");
  ok(!document.querySelector(".recovery-task-grant"), "unbounded scope change does not offer a task grant");
  const detailsButton = document.querySelector(".prompt-shelf__header-button") as HTMLButtonElement;
  ok(detailsButton.textContent?.includes("Technical details"), "technical diagnostics are available on demand");
  await act(async () => {
    detailsButton.click();
    await flushTimers();
  });
  ok(document.querySelector(".recovery-details"), "technical details expand on request");
  ok(document.querySelector(".recovery-details .recovery-detail-row"), "expanded diagnostics use restrained detail rows");
  ok(!document.querySelector(".recovery-details .approval-reason"), "expanded diagnostics do not render an alert wall");
  await act(async () => {
    actions[1].click();
    await flushTimers();
  });
  eq(decisions.length, 0, "opening plan adjustment does not reject or adopt the proposal");
  const guidance = document.querySelector(".recovery-guidance__input") as HTMLTextAreaElement;
  ok(guidance === document.activeElement, "plan adjustment opens and focuses the guidance field");
  const feedback = "Keep the public API and update only the migration guide.";
  await act(async () => {
    const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLTextAreaElement.prototype, "value")?.set;
    setter?.call(guidance, feedback);
    guidance.dispatchEvent(new dom.window.InputEvent("input", { bubbles: true, inputType: "insertText", data: feedback }));
    guidance.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
    guidance.dispatchEvent(new dom.window.KeyboardEvent("keyup", { key: ".", bubbles: true }));
    await flushTimers();
  });
  const submitGuidance = document.querySelector(".recovery-guidance__actions .btn--primary") as HTMLButtonElement;
  ok(submitGuidance.textContent?.includes("Submit adjustment guidance"), "plan guidance uses an explicit submit action");
  ok(!submitGuidance.disabled, "non-empty plan guidance enables submission");
  await act(async () => {
    submitGuidance.click();
    await flushTimers(220);
  });
  eq(decisions[0]?.action, "revise", "submitted plan guidance rejects the proposed transition");
  eq(decisions[0]?.feedback, feedback, "plan adjustment forwards the user's exact guidance");

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ApprovalModal
          approval={{ ...approval, id: "guard-3" }}
          onAnswer={() => undefined}
          onResolveRecovery={(action, nextFeedback) => decisions.push({ action, feedback: nextFeedback })}
          onStop={() => undefined}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });
  actions = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLButtonElement[];
  await act(async () => {
    actions[0].click();
    await flushTimers(220);
  });
  eq(decisions[1]?.action, "continue", "adopting the plan approves the waiting transition once");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// A fresh decision for a dynamic tool is one-shot.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const answers: Array<[boolean, boolean, boolean]> = [];
  const approval: WireApproval = {
		id: "dynamic-danger-1",
		tool: "extension__wipe",
		subject: "Dynamic tool declares destructive side effects",
		reason: "Review the target and arguments before allowing this call.",
    fresh: true,
  };

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ApprovalModal
          approval={approval}
          onAnswer={(a, s, p) => answers.push([a, s, p])}
          onStop={() => undefined}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const actions = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLButtonElement[];
	eq(actions.length, 2, "fresh dynamic-tool decision only offers allow once and deny");
	ok(!document.body.textContent?.includes("Always allow"), "fresh dynamic-tool decision hides remembered grants");

  const confirm = document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement;
  await act(async () => {
    confirm.click();
    await flushTimers(220);
  });
	eq(JSON.stringify(answers[0]), JSON.stringify([true, false, false]), "fresh dynamic-tool decision is one-shot");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Runtime decisions expose direct actions, so their displayed number keys must
// invoke the same callbacks while preserving normal typing behavior.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const selected: string[] = [];
  let cancelled = 0;

  await act(async () => {
    root.render(
      <LocaleProvider>
        <RuntimeDecisionCard
          id="runtime-shortcuts"
          title="Choose runtime action"
          badge="Runtime"
          meta="Select an action"
          actions={["1", "2", "3"].map((key) => ({
            key,
            label: `Action ${key}`,
            description: `Run action ${key} across a deliberately long runtime boundary that must wrap without overlapping the next decision row.`,
            danger: key === "3",
            onClick: () => selected.push(key),
          }))}
          onCancel={() => { cancelled += 1; }}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const runtimeActions = document.querySelector(".prompt-shelf__actions") as HTMLElement | null;
  const runtimeAction = document.querySelector(".prompt-action") as HTMLElement | null;
  const runtimeActionKey = runtimeAction?.querySelector(".prompt-action__key") as HTMLElement | null;
  const runtimeActionLabel = runtimeAction?.querySelector(".prompt-action__label") as HTMLElement | null;
  const runtimeActionDescription = runtimeAction?.querySelector(".prompt-action__desc") as HTMLElement | null;
  if (!runtimeActions || !runtimeAction || !runtimeActionKey || !runtimeActionLabel || !runtimeActionDescription) {
    throw new Error("runtime decision layout did not render");
  }

  const runtimeActionsStyle = window.getComputedStyle(runtimeActions);
  eq(runtimeActionsStyle.gridAutoRows, "max-content", "decision row wrappers accommodate external details");
  eq(runtimeActionsStyle.alignContent, "start", "all decision rows stay aligned at the top of the scroll region");

  const runtimeActionStyle = window.getComputedStyle(runtimeAction);
  eq(runtimeActionStyle.height, "38px", "shared decision rows keep a stable desktop height");
  eq(runtimeActionStyle.minHeight, "38px", "shared decision rows retain a compact click target");
  eq(runtimeActionStyle.alignItems, "center", "single-line decision copy stays vertically centered with the option key");
  eq(window.getComputedStyle(runtimeActionKey).marginTop, "0px", "decision keys do not carry a top offset");
  eq(window.getComputedStyle(runtimeActionLabel).fontWeight, "620", "decision labels keep a clear visual hierarchy");

  const runtimeDescriptionStyle = window.getComputedStyle(runtimeActionDescription);
  eq(runtimeDescriptionStyle.whiteSpace, "nowrap", "shared decision descriptions stay on one summary line");
  eq(runtimeDescriptionStyle.display, "block", "supplementary runtime copy uses ordinary single-line flow");
  eq(runtimeDescriptionStyle.overflow, "hidden", "collapsed runtime summaries stay inside their row");
  eq(runtimeDescriptionStyle.textOverflow, "ellipsis", "long runtime summaries end with an ellipsis");
  eq(runtimeDescriptionStyle.lineHeight, "1.4", "runtime summaries keep readable density");
  const runtimeDescriptionToggle = document.querySelector(".prompt-action-row .prompt-action__description-toggle") as HTMLButtonElement | null;
  if (!runtimeDescriptionToggle) throw new Error("runtime description disclosure did not render");
  eq(runtimeDescriptionToggle.getAttribute("aria-expanded"), "false", "runtime description starts collapsed");
  await act(async () => {
    runtimeDescriptionToggle.click();
    await flushTimers();
  });
  eq(selected.length, 0, "expanding runtime details never triggers the decision action");
  eq(runtimeDescriptionToggle.getAttribute("aria-expanded"), "true", "runtime description expansion is announced");
  eq(window.getComputedStyle(runtimeAction).alignItems, "center", "expanded details keep the runtime row vertically centered");
  eq(window.getComputedStyle(runtimeActionDescription).overflow, "hidden", "expanded details do not alter the runtime row summary");
  const runtimeDetail = runtimeAction.closest(".prompt-action-row")?.querySelector(".prompt-description-detail") as HTMLElement | null;
  if (!runtimeDetail) throw new Error("runtime detail region did not render");
  eq(runtimeDetail.hidden, false, "runtime full copy opens in a separate detail region");
  eq(runtimeDetail.textContent?.includes("deliberately long runtime boundary"), true, "runtime detail region reveals the full copy");
  const runtimeActionButtons = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLElement[];
  const dangerousRuntimeDescription = runtimeActionButtons[2]?.querySelector(".prompt-action__desc") as HTMLElement | null;
  if (!dangerousRuntimeDescription) throw new Error("dangerous runtime description did not render");
  const dangerousRuntimeDetail = runtimeActionButtons[2]
    ?.closest(".prompt-action-row")
    ?.querySelector(".prompt-description-detail") as HTMLElement | null;
  if (!dangerousRuntimeDetail) throw new Error("dangerous runtime detail did not render");
  eq(window.getComputedStyle(dangerousRuntimeDescription).overflow, "hidden", "dangerous runtime summary keeps the shared stable row");
  eq(dangerousRuntimeDetail.hidden, false, "truncated dangerous runtime consequences open automatically outside the row");

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "1", bubbles: true }));
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "2", bubbles: true }));
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "3", bubbles: true }));
    await flushTimers();
  });
  eq(selected.join(","), "1,2,3", "runtime decision number keys invoke their displayed actions");

  await act(async () => {
    root.render(
      <LocaleProvider>
        <RuntimeDecisionCard
          id="runtime-shortcuts"
          title="Choose runtime action"
          badge="Runtime"
          meta="Select an action"
          actions={[{
            key: "2", label: "Unavailable action", description: "Cannot run yet",
            disabled: true, onClick: () => selected.push("disabled"),
          }]}
          onCancel={() => { cancelled += 1; }}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });
  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "2", bubbles: true }));
    await flushTimers();
  });
  eq(selected.join(","), "1,2,3", "runtime decision shortcuts do not invoke disabled actions");

  const input = document.createElement("input");
  document.body.append(input);
  await act(async () => {
    input.dispatchEvent(new window.KeyboardEvent("keydown", { key: "1", bubbles: true }));
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await flushTimers();
  });
  eq(selected.join(","), "1,2,3", "runtime decision shortcuts ignore editable fields");
  eq(cancelled, 1, "Escape cancels the runtime decision");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// A complete danger summary is already the full safety explanation. It must
// not be duplicated below the stable action row merely because it is dangerous.
{
  const dom = installDom("en-US", false);
  const root = createRoot(document.getElementById("root")!);

  await act(async () => {
    root.render(
      <LocaleProvider>
        <RuntimeDecisionCard
          id="runtime-danger-complete"
          title="Stop jobs"
          badge="2 running"
          meta="Stopping is required"
          actions={[{
            key: "1",
            label: "Stop jobs and switch",
            description: "Wait for the processes to exit, then switch modes.",
            danger: true,
            onClick: () => {},
          }]}
          onCancel={() => {}}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  eq(document.querySelectorAll(".prompt-action").length, 1, "complete dangerous runtime summary renders one action row");
  eq(document.querySelector(".prompt-description-detail"), null, "complete dangerous runtime summary is not repeated below the row");
  eq(document.querySelector(".prompt-action__description-toggle"), null, "complete dangerous runtime summary needs no redundant disclosure");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Clear context: default cancel; clear requires explicit confirm; Escape cancels.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  let cancelled = 0;
  let confirmed = 0;

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ClearContextCard onCancel={() => { cancelled += 1; }} onConfirm={() => { confirmed += 1; }} />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const actions = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLButtonElement[];
  ok(actions[0].classList.contains("prompt-action--selected"), "clear context defaults to cancel");
  eq(
    document.querySelector(".prompt-shelf__meta")?.textContent,
    "This deletes the current transcript from local history and keeps only the system prompt.",
    "clear context combines the consequence into one header summary",
  );
  ok(document.querySelector(".prompt-shelf__body") == null, "clear context does not repeat the consequence in a second body row");
  const clearDescriptionToggle = document.querySelector(".prompt-shelf__footnote .prompt-action__description-toggle") as HTMLButtonElement | null;
  if (!clearDescriptionToggle) throw new Error("clear-context description disclosure did not render");
  await act(async () => {
    clearDescriptionToggle.dispatchEvent(new window.KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    }));
    await flushTimers();
  });
  eq(cancelled, 0, "Enter on clear-context disclosure does not trigger the safe default");
  eq(confirmed, 0, "Enter on clear-context disclosure never clears the conversation");
  eq(clearDescriptionToggle.getAttribute("aria-expanded"), "true", "Enter expands clear-context copy");
  await act(async () => {
    clearDescriptionToggle.dispatchEvent(new window.KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    }));
    await flushTimers();
  });
  eq(clearDescriptionToggle.getAttribute("aria-expanded"), "false", "Enter collapses clear-context copy again");
  await act(async () => {
    clearDescriptionToggle.click();
    await flushTimers();
  });
  eq(cancelled, 0, "expanding clear-context copy does not trigger the safe default");
  eq(confirmed, 0, "expanding clear-context copy does not clear anything");

  await act(async () => {
    actions[1].click();
    await flushTimers();
  });
  eq(confirmed, 0, "clicking clear only selects");
  ok(actions[1].classList.contains("prompt-action--selected"), "clear option becomes selected");
  const dangerousClearDetail = document.querySelector(".prompt-shelf__footnote .prompt-description-detail") as HTMLElement | null;
  if (!dangerousClearDetail) throw new Error("dangerous clear-context detail did not render");
  eq(dangerousClearDetail.hidden, false, "truncated destructive consequence opens automatically after selection");
  eq(document.querySelector(".prompt-shelf__footnote .prompt-action__description-toggle"), null, "auto-open destructive consequence has no redundant toggle");

  await act(async () => {
    (document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement).click();
    await flushTimers();
  });
  eq(confirmed, 1, "confirm runs clear once");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  let cancelled = 0;

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ClearContextCard onCancel={() => { cancelled += 1; }} onConfirm={() => undefined} />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await flushTimers();
  });
  eq(cancelled, 1, "Escape cancels clear context immediately");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Composer decision host stays in the tree while visually hidden.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(
      <div>
        <div className="composer-decision-host composer-decision-host--hidden" hidden inert aria-hidden="true">
          <textarea id="composer-input" defaultValue="draft text" />
        </div>
      </div>,
    );
    await flushTimers();
  });

  const host = document.querySelector(".composer-decision-host") as HTMLElement;
  const input = document.getElementById("composer-input") as HTMLTextAreaElement;
  ok(host != null, "composer decision host remains mounted");
  ok(host.hasAttribute("hidden"), "host is hidden during decision");
  ok(host.hasAttribute("inert") || (host as HTMLElement & { inert?: boolean }).inert === true, "host is inert during decision");
  eq(input.value, "draft text", "draft value survives while host is hidden");
  eq(host.getAttribute("aria-hidden"), "true", "host is aria-hidden during decision");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// New approval id resets selection and submitting state.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const answers: Array<[boolean, boolean, boolean]> = [];
  let approval: WireApproval = { id: "a1", tool: "bash", subject: "echo 1" };

  const paint = async (next: WireApproval) => {
    approval = next;
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ApprovalModal
            approval={approval}
            onAnswer={(a, s, p) => answers.push([a, s, p])}
            onStop={() => undefined}
          />
        </LocaleProvider>,
      );
      await flushTimers();
    });
  };

  await paint(approval);
  const actions = () => [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLButtonElement[];
  await act(async () => {
    actions()[3].click();
    await flushTimers();
  });
  ok(actions()[3].classList.contains("prompt-action--selected"), "deny selected on first prompt");

  await paint({ id: "a2", tool: "bash", subject: "echo 2" });
  ok(actions()[0].classList.contains("prompt-action--selected"), "new prompt id resets selection to allow once");
  eq(answers.length, 0, "selection reset does not submit");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
