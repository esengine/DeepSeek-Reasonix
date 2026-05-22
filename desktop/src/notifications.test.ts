import { describe, expect, it } from "vitest";
import { setLang } from "./i18n";
import {
  COMPLETION_NOTIFY_MIN_MS,
  deriveDesktopNotifications,
  type ApprovalSnapshot,
} from "./notifications";

function emptySnapshot(): ApprovalSnapshot {
  return {
    confirms: [],
    pathAccess: [],
    choices: [],
    plans: [],
    checkpoints: [],
    revisions: [],
  };
}

describe("desktop notifications", () => {
  it("uses the active UI language for notification copy", () => {
    setLang("zh-CN");
    const next = emptySnapshot();
    next.confirms.push({ id: 1, command: "git push" });

    const notifications = deriveDesktopNotifications({
      previous: emptySnapshot(),
      current: next,
      wasBusy: true,
      isBusy: true,
      busyDurationMs: 1_000,
      focused: false,
    });

    expect(notifications).toEqual([
      {
        kind: "approval",
        title: "等待批准",
        body: "命令：git push",
      },
    ]);

    setLang("en");
  });

  it("notifies when a new approval request appears while unfocused", () => {
    const next = emptySnapshot();
    next.confirms.push({ id: 1, command: "git push" });

    const notifications = deriveDesktopNotifications({
      previous: emptySnapshot(),
      current: next,
      wasBusy: true,
      isBusy: true,
      busyDurationMs: 1_000,
      focused: false,
    });

    expect(notifications).toEqual([
      {
        kind: "approval",
        title: "Reasonix is waiting for approval",
        body: "Command: git push",
      },
    ]);
  });

  it("does not notify approvals when the window is focused", () => {
    const next = emptySnapshot();
    next.pathAccess.push({ id: 2, path: "/tmp/out.txt", intent: "write" });

    const notifications = deriveDesktopNotifications({
      previous: emptySnapshot(),
      current: next,
      wasBusy: true,
      isBusy: true,
      busyDurationMs: 1_000,
      focused: true,
    });

    expect(notifications).toEqual([]);
  });

  it("notifies on long task completion when the window is unfocused", () => {
    const notifications = deriveDesktopNotifications({
      previous: emptySnapshot(),
      current: emptySnapshot(),
      wasBusy: true,
      isBusy: false,
      busyDurationMs: COMPLETION_NOTIFY_MIN_MS,
      focused: false,
    });

    expect(notifications).toEqual([
      {
        kind: "turn_complete",
        title: "Reasonix task complete",
        body: "The current task finished and is ready for review.",
      },
    ]);
  });

  it("does not notify on short task completion", () => {
    const notifications = deriveDesktopNotifications({
      previous: emptySnapshot(),
      current: emptySnapshot(),
      wasBusy: true,
      isBusy: false,
      busyDurationMs: COMPLETION_NOTIFY_MIN_MS - 1,
      focused: false,
    });

    expect(notifications).toEqual([]);
  });
});
