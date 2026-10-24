// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockPort } from "../port/mock";
import { HttpError, type AgentPort, type WorkspaceChange } from "../port/port";
import type { GitRepoRow } from "../port/workspace";
import { WorkbenchChanges } from "./WorkbenchChanges";
import { WorkbenchGit } from "./WorkbenchGit";

afterEach(cleanup);

const mock = () => new MockPort();
const row = (dir: string, name = dir || "ws", branch = "main"): GitRepoRow => ({ dir, name, branch, detached: false });
const NO_COMMIT = { button: null, card: null };

describe("the changes are a folder tree", () => {
  const list: WorkspaceChange[] = [
    { path: "internal/provider/retry.go", status: "M", unstaged: true, insertions: 3, deletions: 1 },
    { path: "internal/provider/openai/chunk.go", status: "M", staged: true },
    { path: "docs/notes.md", status: "??", unstaged: true },
    { path: "main.go", status: "D", unstaged: true },
  ];
  const draw = (port: AgentPort, onChanged = vi.fn()) => {
    render(<WorkbenchChanges port={port} changes={list} current="" onOpen={() => {}} onChanged={onChanged} commit={NO_COMMIT} />);
    return onChanged;
  };

  it("groups files under folders that are joined when one holds only another, with a count each", () => {
    draw(mock() as unknown as AgentPort);
    const folder = screen.getByRole("button", { name: /^internal\/provider/ });
    expect(within(folder).getByText("2")).toBeTruthy();
    expect(screen.getByRole("button", { name: /^docs/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /^M\s?retry\.go/ })).toBeTruthy();
  });

  it("collapses a folder to hide its files and opens it again", async () => {
    draw(mock() as unknown as AgentPort);
    const folder = screen.getByRole("button", { name: /^internal\/provider/ });
    expect(folder.getAttribute("aria-expanded")).toBe("true");
    await userEvent.click(folder);
    expect(folder.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByRole("button", { name: /^M\s?retry\.go/ })).toBeNull();
    await userEvent.click(folder);
    expect(screen.getByRole("button", { name: /^M\s?retry\.go/ })).toBeTruthy();
  });

  it("stages every file under a folder with one tick, and says it is partly staged when only some are", async () => {
    const port = mock() as unknown as AgentPort;
    port.stageChanges = vi.fn(async () => {});
    const onChanged = draw(port);
    const folderBox = screen.getByRole("checkbox", { name: "暂存 internal/provider/" });
    expect(folderBox.getAttribute("aria-checked")).toBe("mixed");
    await userEvent.click(folderBox);
    expect(port.stageChanges).toHaveBeenLastCalledWith({ paths: ["internal/provider/openai/chunk.go", "internal/provider/retry.go"].sort((a, b) => a.localeCompare(b)), unstage: false });
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });
});

describe("one change can be thrown away", () => {
  const list: WorkspaceChange[] = [
    { path: "src/edited.go", status: "M", unstaged: true },
    { path: "src/removed.go", status: "D", unstaged: true },
    { path: "src/new.go", status: "A", staged: true },
    { path: "notes.md", status: "??", unstaged: true },
    { path: "src/renamed.go", oldPath: "src/was.go", status: "R", staged: true },
  ];
  const draw = (port: AgentPort, repo?: string) => {
    const onChanged = vi.fn();
    render(<WorkbenchChanges port={port} changes={list} repo={repo} current="" onOpen={() => {}} onChanged={onChanged} commit={NO_COMMIT} />);
    return onChanged;
  };
  const dropOf = (path: string) => screen.queryByRole("button", { name: `丢弃 ${path} 的改动` });

  it("offers it only for a modified or deleted file the last commit holds", () => {
    draw(mock() as unknown as AgentPort);
    expect(dropOf("src/edited.go")).toBeTruthy();
    expect(dropOf("src/removed.go")).toBeTruthy();
    expect(dropOf("src/new.go")).toBeNull();
    expect(dropOf("notes.md")).toBeNull();
    expect(dropOf("src/renamed.go")).toBeNull();
  });

  it("asks first, names the file and says it cannot be undone, and does nothing on cancel", async () => {
    const port = mock() as unknown as AgentPort;
    port.discardChange = vi.fn(async () => {});
    draw(port);
    await userEvent.click(dropOf("src/edited.go")!);
    const ask = screen.getByRole("group", { name: "确认丢弃" });
    expect(ask.textContent).toContain("src/edited.go");
    expect(ask.textContent).toContain("无法恢复");
    await userEvent.click(within(ask).getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("group", { name: "确认丢弃" })).toBeNull();
    expect(port.discardChange).not.toHaveBeenCalled();
  });

  it("discards on confirm, asks the list again, and carries the repository", async () => {
    const port = mock() as unknown as AgentPort;
    port.discardChange = vi.fn(async () => {});
    const onChanged = draw(port, "api");
    await userEvent.click(dropOf("src/edited.go")!);
    await userEvent.click(within(screen.getByRole("group", { name: "确认丢弃" })).getByRole("button", { name: "丢弃" }));
    expect(port.discardChange).toHaveBeenCalledWith({ repo: "api", path: "src/edited.go" });
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("group", { name: "确认丢弃" })).toBeNull();
  });

  it("says why a discard was refused and keeps the question open", async () => {
    const port = mock() as unknown as AgentPort;
    port.discardChange = vi.fn(async () => { throw new HttpError(409, "x", { code: "discard.turn_running" }); });
    const onChanged = draw(port);
    await userEvent.click(dropOf("src/edited.go")!);
    await userEvent.click(within(screen.getByRole("group", { name: "确认丢弃" })).getByRole("button", { name: "丢弃" }));
    expect((await screen.findByRole("alert")).textContent).toContain("对话正在运行");
    expect(onChanged).not.toHaveBeenCalled();
    expect(screen.getByRole("group", { name: "确认丢弃" })).toBeTruthy();
  });

  it("offers no discard where the list cannot be refreshed", () => {
    render(<WorkbenchChanges port={mock() as unknown as AgentPort} changes={list} current="" onOpen={() => {}} commit={NO_COMMIT} />);
    expect(dropOf("src/edited.go")).toBeNull();
  });
});

describe("a folder of projects shows each repository on its own", () => {
  const draw = (port: AgentPort, repos = [row("api", "api", "main"), row("web", "web", "dev")]) => {
    const onOpen = vi.fn();
    render(<WorkbenchGit port={port} changes={[]} repos={repos} wrote={0} current="" onOpen={onOpen} onChanged={() => {}} remote={false} />);
    return onOpen;
  };

  it("lists one section per repository with its branch and its own changes", async () => {
    const port = mock();
    port.repoChanges = { api: [{ path: "main.go", status: "M", unstaged: true }], web: [] };
    draw(port as unknown as AgentPort);
    const api = screen.getByRole("button", { name: /api/ });
    expect(api.textContent).toContain("main");
    expect(screen.getByRole("button", { name: /web/ }).textContent).toContain("dev");
    expect(await screen.findByRole("button", { name: /^M\s?main\.go/ })).toBeTruthy();
  });

  it("stages in the repository it was ticked in, and the diff opens at the workspace-relative path", async () => {
    const port = mock();
    port.repoChanges = { api: [{ path: "main.go", status: "M", unstaged: true }], web: [{ path: "app.ts", status: "M", unstaged: true }] };
    const stage = vi.spyOn(port, "stageChanges");
    const onOpen = draw(port as unknown as AgentPort);
    await userEvent.click(await screen.findByRole("checkbox", { name: "暂存 app.ts" }));
    expect(stage).toHaveBeenLastCalledWith({ repo: "web", paths: ["app.ts"], unstage: false });
    await userEvent.click(screen.getByRole("button", { name: /^M\s?main\.go/ }));
    expect(onOpen).toHaveBeenCalledWith("api/main.go");
  });

  it("collapses a repository to its heading", async () => {
    const port = mock();
    port.repoChanges = { api: [{ path: "main.go", status: "M", unstaged: true }], web: [] };
    draw(port as unknown as AgentPort);
    await screen.findByRole("button", { name: /^M\s?main\.go/ });
    await userEvent.click(screen.getByRole("button", { name: /api/ }));
    expect(screen.queryByRole("button", { name: /^M\s?main\.go/ })).toBeNull();
  });

  it("commits in the repository whose section asked", async () => {
    const port = mock();
    port.repoChanges = { api: [{ path: "main.go", status: "M", staged: true }], web: [] };
    const propose = vi.spyOn(port, "proposeCommit");
    const commit = vi.spyOn(port, "commitStaged");
    draw(port as unknown as AgentPort);
    await screen.findByRole("button", { name: /^M\s?main\.go/ });
    await userEvent.click(screen.getAllByRole("button", { name: "提交…" })[0]);
    await screen.findByRole("button", { name: "提交到本地" });
    expect(propose).toHaveBeenCalledWith(expect.anything(), "api");
    await userEvent.click(screen.getByRole("button", { name: "提交到本地" }));
    expect(commit).toHaveBeenCalledWith(expect.objectContaining({ repo: "api" }));
  });

  it("shows a workspace that is itself the repository as one section with no repository headings", () => {
    const port = mock();
    render(<WorkbenchGit port={port as unknown as AgentPort} changes={[{ path: "a.go", status: "M", unstaged: true }]} repos={[row("")]} wrote={0} current="" onOpen={() => {}} onChanged={() => {}} remote={false} />);
    expect(screen.queryByRole("button", { name: /^ws/ })).toBeNull();
    expect(screen.getByRole("button", { name: /^M\s?a\.go/ })).toBeTruthy();
  });

  it("shows nothing for a folder git knows nothing about", () => {
    const { container } = render(<WorkbenchGit port={mock() as unknown as AgentPort} changes={[]} repos={[]} wrote={0} current="" onOpen={() => {}} remote={false} />);
    expect(container.textContent).toBe("");
  });
});

describe("a push is previewed, then confirmed, and says why when it is refused", () => {
  const draw = (port: MockPort, repos = [row("")]) =>
    render(<WorkbenchGit port={port as unknown as AgentPort} changes={[]} repos={repos} wrote={0} current="" onOpen={() => {}} onChanged={() => {}} remote={false} />);
  const open = async () => userEvent.click(screen.getByRole("button", { name: "推送…" }));

  it("shows where it goes, what it sends and what it will not do, before anything is sent", async () => {
    const port = mock();
    const push = vi.spyOn(port, "push");
    draw(port);
    await open();
    const card = await screen.findByRole("region", { name: "推送" });
    expect(card.textContent).toContain("github.com");
    expect(card.textContent).toContain("main → origin/main");
    expect(card.textContent).toContain("2 个提交");
    expect(card.textContent).toContain("从不强制推送");
    expect(card.textContent).toContain("pre-push");
    expect(push).not.toHaveBeenCalled();
  });

  it("sends exactly the previewed plan, with the repository, and reports it", async () => {
    const port = mock();
    const push = vi.spyOn(port, "push");
    draw(port, [row("api"), row("web")]);
    await userEvent.click(screen.getAllByRole("button", { name: "推送…" })[0]);
    await userEvent.click(await screen.findByRole("button", { name: "推送到 github.com" }));
    await waitFor(() => expect(push).toHaveBeenCalled());
    const sent = push.mock.calls[0][0];
    expect(sent.repo).toBe("api");
    expect(sent.plan).toEqual(await port.pushPreview("api").then((p) => ({ ...p, ahead: 2 })));
    expect((await screen.findByText(/已把 2 个提交推送到 github\.com/)).textContent).toBeTruthy();
  });

  it.each([
    ["rejected", "远端有这个分支没有的提交"],
    ["auth", "远端需要认证"],
  ] as const)("says why a push was refused (%s) and leaves it to try again", async (script, words) => {
    const port = mock();
    port.pushScript = script;
    draw(port);
    await open();
    await userEvent.click(await screen.findByRole("button", { name: "推送到 github.com" }));
    expect((await screen.findByRole("alert")).textContent).toContain(words);
    expect((screen.getByRole("button", { name: "推送到 github.com" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("keeps remote success visible when local tracking settlement was refused", async () => {
    const port = mock();
    const push = vi.spyOn(port, "push").mockImplementation(async ({ plan }) => ({ plan, new: false, trackingStatus: "not_updated" }));
    draw(port);
    await open();
    await userEvent.click(await screen.findByRole("button", { name: "推送到 github.com" }));
    expect((await screen.findByText(/远端推送成功，但本地跟踪引用未更新/)).textContent).toContain("无需重复推送");
    expect(screen.getByText(/已把 2 个提交推送到 github\.com/)).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(push).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["untrusted", "还没有被你信任"],
    ["protocol", "只支持 https、ssh 和 git://"],
  ] as const)("shows the refusal at preview (%s) with nothing to confirm", async (script, words) => {
    const port = mock();
    port.pushScript = script;
    draw(port);
    await open();
    expect((await screen.findByRole("alert")).textContent).toContain(words);
    expect(screen.queryByRole("button", { name: "推送到 github.com" })).toBeNull();
  });

  it("offers nothing to confirm when the upstream already has every commit", async () => {
    const port = mock();
    port.pushAhead = 0;
    draw(port);
    await open();
    expect(await screen.findByText(/没有可推送的内容/)).toBeTruthy();
    expect((screen.getByRole("button", { name: "推送到 github.com" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("offers no push where the workspace is remote", () => {
    render(<WorkbenchGit port={mock() as unknown as AgentPort} changes={[]} repos={[row("")]} wrote={0} current="" onOpen={() => {}} onChanged={() => {}} remote />);
    expect(screen.queryByRole("button", { name: "推送…" })).toBeNull();
  });
});

describe("a commit opens into its files and each diff", () => {
  it("lists a commit's files when its row is opened, and one file's diff when that is", async () => {
    const port = mock();
    const files = vi.spyOn(port, "commitFiles");
    const diff = vi.spyOn(port, "commitDiff");
    render(<WorkbenchGit port={port as unknown as AgentPort} changes={[]} repos={[row("")]} wrote={0} current="" onOpen={() => {}} onChanged={() => {}} remote={false} />);
    await userEvent.click(screen.getByText("提交历史"));
    const commit = await screen.findByRole("button", { name: /show the commit graph/ });
    await userEvent.click(commit);
    await waitFor(() => expect(files).toHaveBeenCalledWith("a1", undefined));
    const file = await screen.findByRole("button", { name: /retry\.go/ });
    await userEvent.click(file);
    await waitFor(() => expect(diff).toHaveBeenCalledWith("a1", "internal/provider/retry.go", undefined));
    expect(await screen.findByText(/time\.Sleep/)).toBeTruthy();
  });
});
