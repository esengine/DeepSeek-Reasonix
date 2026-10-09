// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import "./testkit";
import { Backup } from "./Backup";
import { MockPort } from "../port/mock";
import type { AgentPort, BackupApplyResult, BackupCatalog, BackupCreated, BackupEntry, BackupPlan } from "../port/port";

afterEach(cleanup);

function entry(id: string): BackupEntry {
  return { id, label: `backup ${id}`, format: 1, appVersion: "test", platform: "darwin/arm64", categories: ["memory", "automation"], ciphertextBytes: 100, createdAt: "2026-09-27T00:00:00Z" };
}

function catalog(...ids: string[]): BackupCatalog {
  return {
    categories: [{ id: "memory", defaultOn: true, consent: false }, { id: "secrets", defaultOn: false, consent: false }],
    backups: ids.map(entry), limits: { maxCount: 10, maxBytes: 1 << 20 }, minPassphrase: 10,
  };
}

function plan(id: string): BackupPlan {
  return {
    planId: `ticket-${id}`, createdAt: "2026-09-27T00:00:00Z", platform: "darwin/arm64", samePlatform: true,
    categories: ["memory", "automation"], items: [
      { id: "memory:test", name: `${id} memory`, category: "memory", kind: "memory", status: "new", recommended: true },
      { id: "hook:test", name: `${id} hook`, category: "automation", kind: "hook", status: "new", recommended: false, consent: "executes", summary: `${id} command` },
    ],
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function makePort(...ids: string[]) {
  const port = new MockPort() as unknown as AgentPort;
  const list = vi.spyOn(port, "backups").mockResolvedValue(catalog(...ids));
  const preview = vi.spyOn(port, "previewBackup").mockImplementation(async (id) => plan(id));
  const apply = vi.spyOn(port, "applyBackup").mockResolvedValue({ applied: ["memory:test"] });
  const create = vi.spyOn(port, "createBackup").mockResolvedValue({ backup: entry("created") });
  const drop = vi.spyOn(port, "deleteBackup").mockResolvedValue();
  return { port, list, preview, apply, create, drop };
}

function control<T extends HTMLElement = HTMLButtonElement>(action: string, target?: string) {
  return document.querySelector<T>(`[data-action="backup.${action}"]${target ? `[data-target="${target}"]` : ""}`)!;
}

function change(action: string, value: string, target?: string) {
  fireEvent.change(control<HTMLInputElement>(action, target), { target: { value } });
}

function draft(label: string) {
  change("label", label);
  change("passphrase", "private passphrase", "new");
  fireEvent.change(screen.getByLabelText("再输一次"), { target: { value: "private passphrase" } });
}

async function open(id: string) {
  fireEvent.click(control("restore", id));
  change("passphrase", "restore passphrase", "open");
  fireEvent.click(control("preview"));
  await screen.findByText(`${id} command`);
}

function consent() {
  fireEvent.click(control("pick", "hook:test"));
  expect(control<HTMLButtonElement>("apply").disabled).toBe(true);
  fireEvent.click(control("allow", "hook:test"));
  expect(control<HTMLButtonElement>("apply").disabled).toBe(false);
}

describe("backup connection ownership", () => {
  it("does not send an old visible restoration ticket and consent to the replacement port", async () => {
    const a = makePort("A");
    const b = makePort("B");
    const ui = render(<Backup port={a.port} />);
    await screen.findByText("backup A");
    await open("A");
    consent();
    ui.rerender(<Backup port={b.port} />);
    await screen.findByText("backup B");
    const stale = control("apply");
    if (stale) fireEvent.click(stale);
    expect(b.apply).not.toHaveBeenCalled();
    expect(control("apply")).toBeNull();
    await open("B");
    consent();
    fireEvent.click(control("apply"));
    await screen.findByText("已恢复 1 项。");
    expect(a.apply).not.toHaveBeenCalled();
    expect(b.apply).toHaveBeenCalledExactlyOnceWith("ticket-B", ["memory:test", "hook:test"], ["hook:test"]);
  });

  it.each(["success", "failure"])("ignores an old initial catalog's %s after changing connection", async (outcome) => {
    const a = makePort("A");
    const b = makePort("B");
    const pending = deferred<BackupCatalog>();
    a.list.mockImplementationOnce(() => pending.promise);
    const ui = render(<Backup port={a.port} />);
    ui.rerender(<Backup port={b.port} />);
    await screen.findByText("backup B");
    await act(async () => {
      if (outcome === "success") pending.resolve(catalog("A"));
      else pending.reject(new Error("old catalog failed"));
      await pending.promise.catch(() => {});
    });
    expect(screen.queryByText("backup A")).toBeNull();
    expect(screen.queryByText("old catalog failed")).toBeNull();
    expect(screen.getByText("backup B")).toBeTruthy();
  });

  it("clears the old create draft, optional secrets choice and delete confirmation", async () => {
    const a = makePort("shared");
    const b = makePort("shared");
    const ui = render(<Backup port={a.port} />);
    await screen.findByText("backup shared");
    draft("old laptop");
    fireEvent.click(control("category", "secrets"));
    fireEvent.click(control("delete", "shared"));
    ui.rerender(<Backup port={b.port} />);
    await waitFor(() => expect(b.list).toHaveBeenCalledOnce());
    expect(control<HTMLInputElement>("label").value).toBe("");
    expect(control<HTMLInputElement>("passphrase", "new").value).toBe("");
    expect((screen.getByLabelText("再输一次") as HTMLInputElement).value).toBe("");
    expect(control<HTMLInputElement>("category", "secrets").checked).toBe(false);
    fireEvent.click(control("delete", "shared"));
    expect(b.drop).not.toHaveBeenCalled();
    expect(control("delete", "shared").textContent).toBe("确认删除");
    fireEvent.click(control("delete", "shared"));
    await waitFor(() => expect(b.drop).toHaveBeenCalledExactlyOnceWith("shared"));
  });

  it.each(["success", "failure"])("keeps a later creation busy when an old creation returns %s", async (outcome) => {
    const a = makePort("A");
    const b = makePort("B");
    const old = deferred<BackupCreated>();
    const current = deferred<BackupCreated>();
    a.create.mockImplementationOnce(() => old.promise);
    b.create.mockImplementationOnce(() => current.promise);
    const ui = render(<Backup port={a.port} />);
    await screen.findByText("backup A");
    draft("A draft");
    fireEvent.click(control("create"));
    ui.rerender(<Backup port={b.port} />);
    await screen.findByText("backup B");
    draft("B draft");
    expect(control<HTMLButtonElement>("create").disabled).toBe(false);
    fireEvent.click(control("create"));
    await act(async () => {
      if (outcome === "success") old.resolve({ backup: entry("old-created") });
      else old.reject(new Error("old create failed"));
      await old.promise.catch(() => {});
    });
    expect(control<HTMLInputElement>("label").value).toBe("B draft");
    expect(control<HTMLButtonElement>("create").disabled).toBe(true);
    expect(screen.queryByText("old create failed")).toBeNull();
    expect(screen.queryByText("已备份。")).toBeNull();
    expect(a.create).toHaveBeenCalledOnce();
    expect(b.create).toHaveBeenCalledExactlyOnceWith({ label: "B draft", categories: ["memory"], passphrase: "private passphrase" });
    await act(async () => { current.resolve({ backup: entry("new-created") }); await current.promise; });
    expect(screen.getByText("已备份。")).toBeTruthy();
  });

  it.each(["success", "failure"])("discards an old pending preview's %s after changing connection", async (outcome) => {
    const a = makePort("A");
    const b = makePort("B");
    const pending = deferred<BackupPlan>();
    a.preview.mockImplementationOnce(() => pending.promise);
    const ui = render(<Backup port={a.port} />);
    await screen.findByText("backup A");
    fireEvent.click(control("restore", "A"));
    change("passphrase", "restore passphrase", "open");
    fireEvent.click(control("preview"));
    ui.rerender(<Backup port={b.port} />);
    await screen.findByText("backup B");
    await act(async () => {
      if (outcome === "success") pending.resolve(plan("A"));
      else pending.reject(new Error("old preview failed"));
      await pending.promise.catch(() => {});
    });
    expect(control("apply")).toBeNull();
    expect(screen.queryByText("old preview failed")).toBeNull();
    await open("B");
    expect(a.preview).toHaveBeenCalledExactlyOnceWith("A", "restore passphrase");
    expect(b.preview).toHaveBeenCalledExactlyOnceWith("B", "restore passphrase");
  });

  it.each(["success", "failure"])("keeps a new restore preview when an already confirmed old apply returns %s", async (outcome) => {
    const a = makePort("A");
    const b = makePort("B");
    const pending = deferred<BackupApplyResult>();
    a.apply.mockImplementationOnce(() => pending.promise);
    const ui = render(<Backup port={a.port} />);
    await screen.findByText("backup A");
    await open("A");
    fireEvent.click(control("apply"));
    ui.rerender(<Backup port={b.port} />);
    await screen.findByText("backup B");
    expect(control("apply")).toBeNull();
    await open("B");
    await act(async () => {
      if (outcome === "success") pending.resolve({ applied: ["memory:test"], plugins: [{ name: "old plugin", source: "/old-package" }] });
      else pending.reject(new Error("old apply failed"));
      await pending.promise.catch(() => {});
    });
    expect(screen.getByText("B command")).toBeTruthy();
    expect(screen.queryByText("old plugin")).toBeNull();
    expect(screen.queryByText("old apply failed")).toBeNull();
    expect(control<HTMLButtonElement>("apply").disabled).toBe(false);
    expect(a.apply).toHaveBeenCalledExactlyOnceWith("ticket-A", ["memory:test"], []);
    expect(b.apply).not.toHaveBeenCalled();
  });

  it("preserves create input and restore selection/consent on a same-port rerender", async () => {
    const a = makePort("A");
    const ui = render(<Backup port={a.port} />);
    await screen.findByText("backup A");
    draft("keep draft");
    await open("A");
    consent();
    ui.rerender(<Backup port={a.port} />);
    expect(control<HTMLInputElement>("label").value).toBe("keep draft");
    expect(control<HTMLInputElement>("allow", "hook:test").checked).toBe(true);
    fireEvent.click(control("apply"));
    await screen.findByText("已恢复 1 项。");
    expect(a.list).toHaveBeenCalledOnce();
    expect(a.apply).toHaveBeenCalledExactlyOnceWith("ticket-A", ["memory:test", "hook:test"], ["hook:test"]);
  });

  it("requires a new passphrase/preview when choosing a different backup on the same connection", async () => {
    const a = makePort("A", "other");
    render(<Backup port={a.port} />);
    await screen.findByText("backup A");
    await open("A");
    consent();
    fireEvent.click(control("restore", "other"));
    expect(control("apply")).toBeNull();
    expect(control<HTMLInputElement>("passphrase", "open").value).toBe("");
    change("passphrase", "other passphrase", "open");
    fireEvent.click(control("preview"));
    await screen.findByText("other command");
    expect(control<HTMLInputElement>("pick", "hook:test").checked).toBe(false);
    fireEvent.click(control("apply"));
    await screen.findByText("已恢复 1 项。");
    expect(a.apply).toHaveBeenCalledExactlyOnceWith("ticket-other", ["memory:test"], []);
  });

  it("does not recover discarded restoration consent after A to B to A", async () => {
    const a = makePort("A");
    const b = makePort("B");
    const ui = render(<Backup port={a.port} />);
    await screen.findByText("backup A");
    await open("A");
    consent();
    ui.rerender(<Backup port={b.port} />);
    await screen.findByText("backup B");
    ui.rerender(<Backup port={a.port} />);
    await screen.findByText("backup A");
    expect(control("apply")).toBeNull();
    await open("A");
    expect(control<HTMLInputElement>("pick", "hook:test").checked).toBe(false);
    expect(a.apply).not.toHaveBeenCalled();
  });

  it.each(["success", "failure"])("ignores an already confirmed old deletion's %s on the replacement connection", async (outcome) => {
    const a = makePort("A");
    const b = makePort("B");
    const pending = deferred<void>();
    a.drop.mockImplementationOnce(() => pending.promise);
    const ui = render(<Backup port={a.port} />);
    await screen.findByText("backup A");
    fireEvent.click(control("delete", "A"));
    fireEvent.click(control("delete", "A"));
    ui.rerender(<Backup port={b.port} />);
    await screen.findByText("backup B");
    draft("B draft");
    await act(async () => {
      if (outcome === "success") pending.resolve();
      else pending.reject(new Error("old delete failed"));
      await pending.promise.catch(() => {});
    });
    expect(screen.queryByText("backup A")).toBeNull();
    expect(screen.queryByText("old delete failed")).toBeNull();
    expect(screen.getByText("backup B")).toBeTruthy();
    expect(control<HTMLInputElement>("label").value).toBe("B draft");
    expect(a.drop).toHaveBeenCalledExactlyOnceWith("A");
    expect(b.drop).not.toHaveBeenCalled();
  });

  it.each(["success", "failure"])("ignores a previous backup's pending preview %s on the same connection", async (outcome) => {
    const a = makePort("A", "other");
    const pending = deferred<BackupPlan>();
    a.preview.mockImplementationOnce(() => pending.promise);
    render(<Backup port={a.port} />);
    await screen.findByText("backup A");
    fireEvent.click(control("restore", "A"));
    change("passphrase", "first passphrase", "open");
    fireEvent.click(control("preview"));
    fireEvent.click(control("restore", "other"));
    await act(async () => {
      if (outcome === "success") pending.resolve(plan("A"));
      else pending.reject(new Error("old selected preview failed"));
      await pending.promise.catch(() => {});
    });
    expect(control("apply")).toBeNull();
    expect(screen.queryByText("old selected preview failed")).toBeNull();
    expect(control<HTMLInputElement>("passphrase", "open").value).toBe("");
    change("passphrase", "other passphrase", "open");
    fireEvent.click(control("preview"));
    await screen.findByText("other command");
    expect(a.preview).toHaveBeenNthCalledWith(1, "A", "first passphrase");
    expect(a.preview).toHaveBeenNthCalledWith(2, "other", "other passphrase");
  });

  it("preserves consent when the same backup entry is selected again", async () => {
    const a = makePort("A");
    render(<Backup port={a.port} />);
    await screen.findByText("backup A");
    await open("A");
    consent();
    fireEvent.click(control("restore", "A"));
    expect(control<HTMLInputElement>("allow", "hook:test").checked).toBe(true);
    expect(a.preview).toHaveBeenCalledOnce();
  });
});
