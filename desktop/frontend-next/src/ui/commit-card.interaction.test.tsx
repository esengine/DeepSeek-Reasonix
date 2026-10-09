// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import "./testkit";
import { useCommitCard } from "./CommitCard";
import { MockPort } from "../port/mock";
import { HttpError, type AgentPort, type CommitProposal } from "../port/port";

afterEach(cleanup);

function Harness({ port, onCommitted }: { port: AgentPort; onCommitted: () => void }) {
  const c = useCommitCard(port, onCommitted);
  return <div>{c.button}{c.card}</div>;
}

const proposal = (over: Partial<CommitProposal> = {}): CommitProposal => ({
  message: "feat: add x",
  fingerprint: "fp1",
  files: [{ path: "x.go", status: "A", insertions: 1, deletions: 0 }],
  truncated: false,
  contentSecrets: false,
  ...over,
});

function draw(p: CommitProposal) {
  const port = new MockPort() as unknown as AgentPort;
  port.proposeCommit = vi.fn(async () => p);
  port.commitStaged = vi.fn(async (req) => ({ hash: "abcdef0123456789", subject: req.message }));
  const onCommitted = vi.fn();
  const r = render(<Harness port={port} onCommitted={onCommitted} />);
  const act = (name: string) => r.container.querySelector(`[data-action="${name}"]`) as HTMLButtonElement;
  return { ...r, port, onCommitted, act };
}

describe("committing the staged changes", () => {
  it("drafts on request and commits nothing until the person confirms the edited text", async () => {
    const { container, port, act, onCommitted } = draw(proposal());
    fireEvent.click(act("commit.draft"));
    await waitFor(() => expect(container.querySelector("textarea")).not.toBeNull());
    expect(port.commitStaged).not.toHaveBeenCalled();

    const box = container.querySelector("textarea") as HTMLTextAreaElement;
    expect(box.value).toBe("feat: add x");
    fireEvent.change(box, { target: { value: "feat: add x (edited)" } });
    fireEvent.click(act("commit.confirm"));
    await waitFor(() => expect(container.querySelector('[data-phase="done"]')).not.toBeNull());
    expect(port.commitStaged).toHaveBeenCalledWith({ message: "feat: add x (edited)", fingerprint: "fp1", acknowledgeSecrets: false });
    expect(onCommitted).toHaveBeenCalledTimes(1);
    expect(container.textContent).toMatch(/abcdef0/);
  });

  it("shows what each staged file changed by", async () => {
    const { container, act } = draw(proposal());
    fireEvent.click(act("commit.draft"));
    await waitFor(() => expect(container.querySelector(".commit-files")).not.toBeNull());
    expect(container.querySelector(".commit-files")?.textContent).toMatch(/\+1/);
    expect(container.querySelector(".commit-files")?.textContent).toMatch(/-0/);
  });

  it("keeps the file name in its own span so only the directory can be cut", async () => {
    const { container, act } = draw(
      proposal({ files: [{ path: "internal/provider/retry/backoff.go", status: "M" }] }),
    );
    fireEvent.click(act("commit.draft"));
    await waitFor(() => expect(container.querySelector(".commit-path")).not.toBeNull());
    expect(container.querySelector(".commit-path .commit-dir")?.textContent).toBe("internal/provider/retry/");
    expect(container.querySelector(".commit-path .commit-base")?.textContent).toBe("backoff.go");
  });

  it("holds the confirm button until a secrets warning is acknowledged", async () => {
    const { container, act, port } = draw(
      proposal({ files: [{ path: ".env", status: "A", sensitive: true }], contentSecrets: true }),
    );
    fireEvent.click(act("commit.draft"));
    await waitFor(() => expect(container.querySelector(".commit-warn")).not.toBeNull());
    expect(container.querySelector(".commit-warn")?.textContent).toMatch(/\.env/);
    expect(act("commit.confirm").disabled).toBe(true);
    fireEvent.click(container.querySelector('.commit-warn input[type="checkbox"]') as HTMLInputElement);
    expect(act("commit.confirm").disabled).toBe(false);
    fireEvent.click(act("commit.confirm"));
    await waitFor(() => expect(port.commitStaged).toHaveBeenCalledWith(expect.objectContaining({ acknowledgeSecrets: true })));
  });

  it("says why a draft or a commit was refused and keeps the text", async () => {
    const port = new MockPort() as unknown as AgentPort;
    port.proposeCommit = vi.fn(async () => proposal());
    port.commitStaged = vi.fn(async () => {
      throw new HttpError(409, "stale", { code: "commit.staged_changed", error: "stale" }, true);
    });
    const r = render(<Harness port={port} onCommitted={vi.fn()} />);
    fireEvent.click(r.container.querySelector('[data-action="commit.draft"]') as HTMLElement);
    await waitFor(() => expect(r.container.querySelector("textarea")).not.toBeNull());
    fireEvent.click(r.container.querySelector('[data-action="commit.confirm"]') as HTMLElement);
    await waitFor(() => expect(r.container.querySelector('[role="alert"]')).not.toBeNull());
    expect(r.container.querySelector('[role="alert"]')?.textContent).toMatch(/暂存区/);
    expect((r.container.querySelector("textarea") as HTMLTextAreaElement).value).toBe("feat: add x");
  });
});
