// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("./cards", () => ({
  AssistantText: () => null,
  PlanCardView: () => null,
  ShellCard: () => null,
  ToolCard: () => null,
  ReasoningCard: () => null,
}));

import { ConfirmApprovalCard, PathAccessApprovalCard, PlanApprovalCard } from "./thread";

function makeShellPrompt(command: string): import("@reasonix/core-utils").ApprovalPrompt {
  return {
    id: 1,
    kind: "shell",
    tone: "warn",
    title: "Run command",
    subtitle: command,
    preview: command,
    meta: {},
    actions: [
      { id: "run_once", label: "Run once", kind: "allow_once" },
      { id: "always_allow", label: "Always allow — git", kind: "allow_always" },
      {
        id: "deny",
        label: "Deny",
        kind: "reject",
        secondaryInput: { hint: "Reason", required: false },
      },
    ],
    data: { prefix: command.split(" ")[0] ?? "" },
  };
}

function makePathPrompt(
  path: string,
  intent: "read" | "write",
): import("@reasonix/core-utils").ApprovalPrompt {
  return {
    id: 2,
    kind: "path",
    tone: "warn",
    title: `Access path — ${intent}`,
    subtitle: path,
    preview: `read_file → ${path}`,
    meta: { sandboxRoot: "/workspace" },
    actions: [
      {
        id: "run_once",
        label: intent === "write" ? "Allow write" : "Allow read",
        kind: "allow_once",
      },
      { id: "always_allow", label: "Always allow — /workspace", kind: "allow_always" },
      {
        id: "deny",
        label: "Deny",
        kind: "reject",
        secondaryInput: { hint: "Reason", required: false },
      },
    ],
    data: { prefix: "/workspace", intent },
  };
}

afterEach(cleanup);

describe("PlanApprovalCard", () => {
  const plan = {
    id: 42,
    plan: "1. Inspect\n2. Fix",
    summary: "Fix approval buttons",
    steps: [
      { id: "inspect", title: "Inspect", action: "read" },
      { id: "fix", title: "Fix", action: "edit" },
    ],
  };

  it("fires approve directly", () => {
    const onApprove = vi.fn();
    render(
      <PlanApprovalCard
        p={plan}
        onApprove={onApprove}
        onCancel={() => {}}
        onRefine={() => {}}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Approve" }));

    expect(onApprove).toHaveBeenCalledTimes(1);
  });

  it("asks for optional feedback before canceling", () => {
    const onCancel = vi.fn();
    render(
      <PlanApprovalCard
        p={plan}
        onApprove={() => {}}
        onCancel={onCancel}
        onRefine={() => {}}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.getByLabelText("Tell Reasonix why you want to cancel this plan")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Skip feedback" }));

    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onCancel).toHaveBeenCalledWith();
  });

  it("sends refine feedback when provided", () => {
    const onRefine = vi.fn();
    render(
      <PlanApprovalCard
        p={plan}
        onApprove={() => {}}
        onCancel={() => {}}
        onRefine={onRefine}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Refine" }));
    fireEvent.change(screen.getByLabelText("Tell Reasonix what to refine in this plan"), {
      target: { value: "Split the risky step first" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send feedback" }));

    expect(onRefine).toHaveBeenCalledTimes(1);
    expect(onRefine).toHaveBeenCalledWith("Split the risky step first");
  });
});

describe("ConfirmApprovalCard — ApprovalPrompt rendering", () => {
  it("renders title, subtitle, and action buttons from prompt", () => {
    const { container } = render(
      <ConfirmApprovalCard
        prompt={makeShellPrompt("git status")}
        onAllow={() => {}}
        onAlwaysAllow={() => {}}
        onDeny={() => {}}
      />,
    );
    expect(container.querySelector(".ap-title")?.textContent).toBe("Run command");
    expect(container.querySelector(".ap-sub")?.textContent).toBe("git status");
    expect(screen.getByRole("button", { name: "Run once" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Always allow — git" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Deny" })).toBeTruthy();
  });

  it("fires onAllow when primary button is clicked", () => {
    const onAllow = vi.fn();
    render(
      <ConfirmApprovalCard
        prompt={makeShellPrompt("echo hi")}
        onAllow={onAllow}
        onAlwaysAllow={() => {}}
        onDeny={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Run once" }));
    expect(onAllow).toHaveBeenCalledTimes(1);
  });

  it("fires onAlwaysAllow with prefix when tertiary button is clicked", () => {
    const onAlwaysAllow = vi.fn();
    render(
      <ConfirmApprovalCard
        prompt={makeShellPrompt("npm test")}
        onAllow={() => {}}
        onAlwaysAllow={onAlwaysAllow}
        onDeny={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /Always allow/ }));
    expect(onAlwaysAllow).toHaveBeenCalledTimes(1);
    expect(onAlwaysAllow).toHaveBeenCalledWith("npm");
  });

  it("fires onDeny when secondary button is clicked", () => {
    const onDeny = vi.fn();
    render(
      <ConfirmApprovalCard
        prompt={makeShellPrompt("rm -rf /")}
        onAllow={() => {}}
        onAlwaysAllow={() => {}}
        onDeny={onDeny}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    expect(onDeny).toHaveBeenCalledTimes(1);
  });
});

describe("PathAccessApprovalCard — ApprovalPrompt rendering", () => {
  it("renders title, subtitle, and action buttons from prompt", () => {
    const { container } = render(
      <PathAccessApprovalCard
        prompt={makePathPrompt("/etc/passwd", "read")}
        onAllow={() => {}}
        onAlwaysAllow={() => {}}
        onDeny={() => {}}
      />,
    );
    expect(container.querySelector(".ap-title")?.textContent).toBe("Access path — read");
    expect(container.querySelector(".ap-sub")?.textContent).toBe("/etc/passwd");
    expect(screen.getByRole("button", { name: "Allow read" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Deny" })).toBeTruthy();
  });

  it("fires onAlwaysAllow with prefix for path access", () => {
    const onAlwaysAllow = vi.fn();
    render(
      <PathAccessApprovalCard
        prompt={makePathPrompt("/tmp", "write")}
        onAllow={() => {}}
        onAlwaysAllow={onAlwaysAllow}
        onDeny={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /Always allow/ }));
    expect(onAlwaysAllow).toHaveBeenCalledTimes(1);
    expect(onAlwaysAllow).toHaveBeenCalledWith("/workspace");
  });
});
