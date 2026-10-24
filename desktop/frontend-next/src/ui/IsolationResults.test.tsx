// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { IsolationResults } from "./IsolationResults";
import { DiscardIsolatedDialog } from "./DiscardIsolatedDialog";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/http_error";
import type { AgentPort, IsolatedDiscardPreview } from "../port/port";
import type { WireEvent } from "../port/wire";

afterEach(cleanup);
const fixture = () => new MockPort() as unknown as AgentPort;
const openResult = async () => userEvent.click(await screen.findByText("iso_55bb1122"));

describe("retained isolated results", () => {
  it("recovers the displayed descendant's journal and ignores completion after a workspace change", async () => {
    const port = fixture();
    const catalog = await port.isolatedResults();
    const recovery = { resultId: "iso_55bb1122", workspace: "/parent/candidate", runtime: "current", revision: "journal", paths: ["/parent/candidate/child.txt"] };
    catalog.results[0] = { ...catalog.results[0], parentId: "iso_parent", state: "applying", actions: ["recover"], recovery };
    port.isolatedResults = vi.fn(async () => catalog);
    let finish!: () => void;
    const recover = vi.spyOn(port, "recoverIsolatedIntegration").mockImplementation(() => new Promise<void>((resolve) => { finish = resolve; }));
    let receive!: (event: WireEvent) => void;
    vi.spyOn(port, "subscribe").mockImplementation((listener) => { receive = listener; return () => {}; });
    render(<IsolationResults port={port} />);
    await openResult();
    expect(screen.getByText("/parent/candidate/child.txt")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "恢复未完成的整合" }));
    expect(recover).toHaveBeenCalledExactlyOnceWith({ resultId: recovery.resultId, workspace: recovery.workspace, runtime: recovery.runtime, revision: recovery.revision });
    expect(screen.getByRole("button", { name: "恢复未完成的整合" })).toHaveProperty("disabled", true);
    const next = { ...catalog, results: [] };
    port.isolatedResults = vi.fn(async () => next);
    act(() => receive({ kind: "workspace_changed" } as WireEvent));
    await screen.findByText("没有待处理的隔离成果。");
    const calls = vi.mocked(port.isolatedResults).mock.calls.length;
    await act(async () => finish());
    expect(vi.mocked(port.isolatedResults).mock.calls.length).toBe(calls);
    expect(screen.queryByText("/parent/candidate/child.txt")).toBeNull();
  });
  it("shows the direct parent and actual integration target for a retained descendant", async () => {
    const port = fixture();
    const catalog = await port.isolatedResults();
    catalog.results[0] = { ...catalog.results[0], parentId: "iso_parent", targetWorkspace: "/parent/candidate" };
    port.isolatedResults = async () => catalog;
    render(<IsolationResults port={port} />);
    await openResult();
    expect(screen.getByText("直接父任务：iso_parent")).toBeTruthy();
    expect(screen.getByText("整合目标：/parent/candidate")).toBeTruthy();
  });
  it("binds root actions to the displayed scene and exposes cancellation without discarding live files", async () => {
    const port = fixture();
    const rootScene = { environmentId: "env", generation: "generation", runtime: "runtime", owner: "session", sourceWorkspace: "/source", liveWorkspace: "/live", publicationId: "pub", resultId: "iso_root", state: "pending", verification: "unknown", actions: ["recheck", "retry"] };
    port.isolatedResults = async () => ({ enabled: true, recoveryRequired: false, results: [], diagnostics: [], rootScene });
    let finish!: () => void;
    const manage = vi.spyOn(port, "manageRootScene").mockImplementation(() => new Promise<void>((resolve) => { finish = resolve; }));
    const cancel = vi.spyOn(port, "cancel").mockResolvedValue();
    const discard = vi.spyOn(port, "discardIsolated");
    render(<IsolationResults port={port} />);
    await screen.findByText("执行目录：/live");
    expect(screen.getByText("整合目标：/source")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "重新验证并整合" }));
    expect(manage).toHaveBeenCalledExactlyOnceWith({ environmentId: "env", generation: "generation", runtime: "runtime", owner: "session", publicationId: "pub", action: "recheck" });
    expect(screen.getByRole("button", { name: "准备重新导出" })).toHaveProperty("disabled", true);
    await userEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(cancel).toHaveBeenCalledOnce();
    expect(discard).not.toHaveBeenCalled();
    await act(async () => finish());
    await waitFor(() => expect(screen.queryByRole("button", { name: "取消" })).toBeNull());
    expect(screen.getByText("执行目录：/live")).toBeTruthy();
  });

  it("sends the exact source recovery revision and shows its affected files", async () => {
    const port = fixture();
    const rootScene = { environmentId: "env", generation: "generation", runtime: "runtime", owner: "session", sourceWorkspace: "/source", liveWorkspace: "/live", publicationId: "pub", resultId: "iso_root", state: "pending", verification: "unknown", actions: ["recover"], recovery: { workspace: "/source", runtime: "", revision: "journal-preview", paths: ["/source/file"] } };
    port.isolatedResults = async () => ({ enabled: true, recoveryRequired: false, results: [], diagnostics: [], rootScene });
    const manage = vi.spyOn(port, "manageRootScene").mockResolvedValue();
    render(<IsolationResults port={port} />);
    await screen.findByText("/source/file");
    await userEvent.click(screen.getByRole("button", { name: "恢复源工作区整合" }));
    expect(manage).toHaveBeenCalledExactlyOnceWith({ environmentId: "env", generation: "generation", runtime: "runtime", owner: "session", publicationId: "pub", action: "recover", revision: "journal-preview" });
  });
  it("places focus on cancel once preview preparation stops disabling the dialog", async () => {
    const props = { preview: { id: "iso_55bb1122", revision: "1".repeat(64), candidateRoot: "/candidate", entries: 4, state: "pending" }, error: "", onConfirm: vi.fn(), onClose: vi.fn() };
    const view = render(<DiscardIsolatedDialog {...props} busy />);
    expect(screen.getByRole("button", { name: "取消" })).toHaveProperty("disabled", true);
    view.rerender(<DiscardIsolatedDialog {...props} busy={false} />);
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "取消" }));
  });
  it("previews the candidate and deletes only after explicit confirmation with the exact revision", async () => {
    const port = fixture();
    const preview = vi.spyOn(port, "previewIsolatedDiscard");
    const discard = vi.spyOn(port, "discardIsolated");
    render(<IsolationResults port={port} />);
    await openResult();
    expect(screen.getByText("验证状态未知")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "预览丢弃" }));
    const dialog = await screen.findByRole("dialog", { name: "丢弃隔离成果 iso_55bb1122？" });
    expect(preview).toHaveBeenCalledWith("iso_55bb1122");
    expect(discard).not.toHaveBeenCalled();
    expect(within(dialog).getByText(/包括忽略文件及任务输出/)).toBeTruthy();
    expect(document.activeElement).toBe(within(dialog).getByRole("button", { name: "取消" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "确认丢弃" }));
    expect(discard).toHaveBeenCalledExactlyOnceWith({ id: "iso_55bb1122", revision: "1".repeat(64) });
    expect(await screen.findByText("没有待处理的隔离成果。")).toBeTruthy();
  });

  it("cancels the preview without deleting, and keeps a stale candidate on conflict", async () => {
    const port = fixture();
    const discard = vi.spyOn(port, "discardIsolated").mockRejectedValue(new HttpError(409, "changed", { code: "isolation.discard_changed" }));
    render(<IsolationResults port={port} />);
    await openResult();
    await userEvent.click(screen.getByRole("button", { name: "预览丢弃" }));
    let dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "取消" }));
    expect(discard).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "预览丢弃" }));
    dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "确认丢弃" }));
    await within(dialog).findByRole("alert");
    expect(screen.getByText("iso_55bb1122")).toBeTruthy();
    expect(screen.queryByText("没有待处理的隔离成果。")).toBeNull();
  });

  it("does not offer discard for an active run or invent verification success", async () => {
    const port = fixture();
    const catalog = await port.isolatedResults();
    catalog.results[0] = { ...catalog.results[0], activity: "active", state: "running", verification: "valid" };
    port.isolatedResults = async () => catalog;
    render(<IsolationResults port={port} />);
    await openResult();
    expect(screen.getByRole("button", { name: "预览丢弃" })).toHaveProperty("disabled", true);
    expect(screen.getByText("验证状态未知")).toBeTruthy();
  });

  it("offers the host-owned discard action for a stopped integration attempt", async () => {
    const port = fixture();
    const catalog = await port.isolatedResults();
    catalog.results[0] = { ...catalog.results[0], kind: "integration", id: "int_ABCDEFGHIJKLMNOPQRSTUVWXYZ", resultId: "iso_55bb1122", state: "recovery_required", reason: "verification was interrupted" };
    port.isolatedResults = async () => catalog;
    const preview = vi.spyOn(port, "previewIsolatedDiscard").mockResolvedValue({ id: catalog.results[0].id, revision: "2".repeat(64), candidateRoot: "/integration-candidate", entries: 4, state: "recovery_required" });
    render(<IsolationResults port={port} />);
    await userEvent.click(await screen.findByText(catalog.results[0].id));
    await userEvent.click(screen.getByRole("button", { name: "预览丢弃" }));
    expect(preview).toHaveBeenCalledWith(catalog.results[0].id);
    expect(await screen.findByRole("dialog")).toBeTruthy();
  });

  it("ignores a late preview from a previous workspace", async () => {
    const oldPort = fixture();
    let resolve!: (preview: IsolatedDiscardPreview) => void;
    oldPort.previewIsolatedDiscard = () => new Promise((finish) => { resolve = finish; });
    const newPort = fixture();
    const catalog = await newPort.isolatedResults();
    catalog.results = [];
    newPort.isolatedResults = async () => catalog;
    const view = render(<IsolationResults port={oldPort} />);
    await openResult();
    await userEvent.click(screen.getByRole("button", { name: "预览丢弃" }));
    view.rerender(<IsolationResults port={newPort} />);
    await screen.findByText("没有待处理的隔离成果。");
    resolve({ id: "iso_55bb1122", revision: "1".repeat(64), candidateRoot: "/old/candidate", entries: 4, state: "pending" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.queryByText("/old/candidate")).toBeNull();
  });

  it("offers source recovery only when the source journal requires it", async () => {
    const port = fixture();
    const catalog = await port.isolatedResults();
    catalog.results[0].state = "recovery_required";
    port.isolatedResults = async () => catalog;
    const recover = vi.spyOn(port, "recoverIsolatedIntegration");
    render(<IsolationResults port={port} />);
    await openResult();
    expect(screen.queryByRole("button", { name: "恢复未完成的整合" })).toBeNull();
    catalog.recoveryRequired = true;
    catalog.recovery = { workspace: "/workspace", runtime: "runtime-one", revision: "2".repeat(64), paths: ["/workspace/a.txt"] };
    await userEvent.click(screen.getByRole("button", { name: "刷新" }));
    await userEvent.click(await screen.findByRole("button", { name: "恢复未完成的整合" }));
    expect(recover).toHaveBeenCalledExactlyOnceWith({ workspace: "/workspace", runtime: "runtime-one", revision: "2".repeat(64) });
  });

  it("invalidates an outstanding preview when the same port changes workspace", async () => {
    const port = fixture();
    let receive!: (event: WireEvent) => void;
    vi.spyOn(port, "subscribe").mockImplementation((listener) => { receive = listener; return () => {}; });
    let resolve!: (preview: IsolatedDiscardPreview) => void;
    port.previewIsolatedDiscard = () => new Promise((finish) => { resolve = finish; });
    const view = render(<IsolationResults port={port} />);
    await openResult();
    await userEvent.click(screen.getByRole("button", { name: "预览丢弃" }));
    const replacement = await port.isolatedResults();
    replacement.results = [];
    port.isolatedResults = async () => replacement;
    act(() => receive({ kind: "workspace_changed" } as WireEvent));
    await screen.findByText("没有待处理的隔离成果。");
    act(() => resolve({ id: "iso_55bb1122", revision: "1".repeat(64), candidateRoot: "/old/candidate", entries: 4, state: "pending" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    view.unmount();
  });

  it("retains compatibility with a kernel that predates the results route", async () => {
    const port = fixture();
    port.isolatedResults = async () => { throw new HttpError(404, "not found"); };
    render(<IsolationResults port={port} />);
    expect(await screen.findByText("当前内核未开放隔离成果管理。")).toBeTruthy();
  });
});
