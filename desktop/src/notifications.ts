import { t } from "./i18n";

export type ApprovalSnapshot = {
  confirms: { id: number; command: string }[];
  pathAccess: { id: number; path: string; intent: "read" | "write" }[];
  choices: { id: number; question: string }[];
  plans: { id: number; summary?: string; plan: string }[];
  checkpoints: { id: number; title?: string; result: string }[];
  revisions: { id: number; summary?: string; reason: string }[];
};

export type DesktopNotification =
  | { kind: "approval"; title: string; body: string }
  | { kind: "turn_complete"; title: string; body: string };

export const COMPLETION_NOTIFY_MIN_MS = 15_000;

function newestApproval(snapshot: ApprovalSnapshot): DesktopNotification | null {
  const confirm = snapshot.confirms.at(-1);
  if (confirm) {
    return {
      kind: "approval",
      title: t("notifications.approvalTitle"),
      body: t("notifications.commandBody", { command: confirm.command }),
    };
  }
  const pathAccess = snapshot.pathAccess.at(-1);
  if (pathAccess) {
    return {
      kind: "approval",
      title: t("notifications.approvalTitle"),
      body: t(
        pathAccess.intent === "write" ? "notifications.writeBody" : "notifications.readBody",
        { path: pathAccess.path },
      ),
    };
  }
  const choice = snapshot.choices.at(-1);
  if (choice) {
    return {
      kind: "approval",
      title: t("notifications.inputTitle"),
      body: choice.question,
    };
  }
  const plan = snapshot.plans.at(-1);
  if (plan) {
    return {
      kind: "approval",
      title: t("notifications.planApprovalTitle"),
      body: plan.summary ?? plan.plan,
    };
  }
  const checkpoint = snapshot.checkpoints.at(-1);
  if (checkpoint) {
    return {
      kind: "approval",
      title: t("notifications.checkpointApprovalTitle"),
      body: checkpoint.title ?? checkpoint.result,
    };
  }
  const revision = snapshot.revisions.at(-1);
  if (revision) {
    return {
      kind: "approval",
      title: t("notifications.revisionTitle"),
      body: revision.summary ?? revision.reason,
    };
  }
  return null;
}

export function totalPending(snapshot: ApprovalSnapshot): number {
  return (
    snapshot.confirms.length +
    snapshot.pathAccess.length +
    snapshot.choices.length +
    snapshot.plans.length +
    snapshot.checkpoints.length +
    snapshot.revisions.length
  );
}

export function deriveDesktopNotifications(args: {
  previous: ApprovalSnapshot;
  current: ApprovalSnapshot;
  wasBusy: boolean;
  isBusy: boolean;
  busyDurationMs: number;
  focused: boolean;
}): DesktopNotification[] {
  const notifications: DesktopNotification[] = [];
  if (args.focused) return notifications;

  if (totalPending(args.current) > totalPending(args.previous)) {
    const approval = newestApproval(args.current);
    if (approval) notifications.push(approval);
  }

  if (args.wasBusy && !args.isBusy && args.busyDurationMs >= COMPLETION_NOTIFY_MIN_MS) {
    notifications.push({
      kind: "turn_complete",
      title: t("notifications.turnCompleteTitle"),
      body: t("notifications.turnCompleteBody"),
    });
  }

  return notifications;
}

export function shouldShowCompletionToast(args: {
  wasBusy: boolean;
  isBusy: boolean;
  busyDurationMs: number;
  focused: boolean;
}): boolean {
  return (
    args.focused &&
    args.wasBusy &&
    !args.isBusy &&
    args.busyDurationMs >= COMPLETION_NOTIFY_MIN_MS
  );
}

let notificationPermissionAttempted = false;

export async function dispatchDesktopNotifications(
  notifications: DesktopNotification[],
  deps: {
    isFocused: () => Promise<boolean>;
    isPermissionGranted: () => Promise<boolean>;
    requestPermission: () => Promise<"default" | "denied" | "granted">;
    sendNotification: (note: { title: string; body: string }) => void;
  },
): Promise<void> {
  if (notifications.length === 0) return;
  const focused = await deps.isFocused().catch(() => true);
  if (focused) return;

  let granted = await deps.isPermissionGranted().catch(() => false);
  if (!granted && !notificationPermissionAttempted) {
    notificationPermissionAttempted = true;
    const permission = await deps.requestPermission().catch(() => "denied" as const);
    granted = permission === "granted";
  }
  if (!granted) return;

  for (const note of notifications) {
    deps.sendNotification({ title: note.title, body: note.body });
  }
}
