// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ThemeImport } from "./ThemeImport";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, ThemeImport as ImportResult } from "../port/port";

const result: ImportResult = {
  pack: { id: "returned-id", name: "Dusk", tokens: {} }, ignored: ["unused.png"],
};
const imported = () => t("已导入「{name}」。", { name: "Dusk" }) + " " + t("未读取：{names}", { names: "unused.png" });
const folderNote = (path: string) => t("主题目录：{path}", { path });

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

function pending<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function draw(port: AgentPort, onImported = vi.fn(), onUse = vi.fn()) {
  render(<ThemeImport port={port} empty={false} onImported={onImported} onUse={onUse} />);
  return { onImported, onUse };
}

function choose() {
  fireEvent.change(document.querySelector('input[data-action="theme.import"]')!, {
    target: { files: [new File(["fixture"], "dusk.zip")] },
  });
}

function settleFolder(call: ReturnType<typeof pending<string>>, outcome: string) {
  if (outcome === "success") call.resolve("/older/themes");
  else call.reject(new Error("older folder request failed"));
}

it.each(["success", "failure"].flatMap((importOutcome) => [
  { timing: "pending", outcome: "success", importOutcome }, { timing: "pending", outcome: "failure", importOutcome },
  { timing: "settled", outcome: "success", importOutcome }, { timing: "settled", outcome: "failure", importOutcome },
]))("keeps import $importOutcome feedback current when an older folder $outcome arrives with the import $timing", async ({ timing, outcome, importOutcome }) => {
  const port = new MockPort() as unknown as AgentPort;
  const folder = pending<string>();
  const upload = pending<ImportResult>();
  vi.spyOn(port, "openThemeFolder").mockReturnValue(folder.promise);
  vi.spyOn(port, "importTheme").mockReturnValue(upload.promise);
  const { onImported, onUse } = draw(port);
  await userEvent.click(screen.getByRole("button", { name: t("打开主题目录") }));
  choose();
  if (timing === "pending") {
    await act(async () => settleFolder(folder, outcome));
    expect(screen.queryByText(folderNote("/older/themes"))).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(onImported).not.toHaveBeenCalled();
  }
  await act(async () => importOutcome === "success" ? upload.resolve(result) : upload.reject(new Error("current import failed")));
  if (timing === "settled") await act(async () => settleFolder(folder, outcome));
  expect(screen.queryByText(folderNote("/older/themes"))).toBeNull();
  if (importOutcome === "success") {
    expect(screen.getByText(imported())).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(onImported).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole("button", { name: t("立即使用") }));
    expect(onUse).toHaveBeenCalledExactlyOnceWith("returned-id");
  } else {
    expect(screen.getByRole("alert").textContent).toBe("current import failed");
    expect(screen.queryByRole("button", { name: t("立即使用") })).toBeNull();
    expect(onImported).not.toHaveBeenCalled();
    expect(onUse).not.toHaveBeenCalled();
  }
});

it.each(["success", "failure"])("holds the folder action during an import and restores it after %s", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const upload = pending<ImportResult>();
  vi.spyOn(port, "importTheme").mockReturnValue(upload.promise);
  const open = vi.spyOn(port, "openThemeFolder").mockResolvedValue("/current/themes");
  draw(port);
  choose();
  const folder = screen.getByRole<HTMLButtonElement>("button", { name: t("打开主题目录") });
  expect(folder.disabled).toBe(true);
  await userEvent.click(folder);
  expect(open).not.toHaveBeenCalled();
  await act(async () => outcome === "success" ? upload.resolve(result) : upload.reject(new Error("import failed")));
  expect(folder.disabled).toBe(false);
  if (outcome === "failure") expect(screen.getByRole("alert").textContent).toBe("import failed");
  await userEvent.click(folder);
  expect(await screen.findByText(folderNote("/current/themes"))).toBeTruthy();
  expect(open).toHaveBeenCalledTimes(1);
});

it.each(["success", "failure"])("retains the latest folder result when an older request ends with %s", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const old = pending<string>();
  const current = pending<string>();
  vi.spyOn(port, "openThemeFolder").mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise);
  draw(port);
  const button = screen.getByRole("button", { name: t("打开主题目录") });
  await userEvent.click(button);
  await userEvent.click(button);
  await act(async () => current.resolve("/current/themes"));
  await act(async () => settleFolder(old, outcome));
  expect(screen.getByText(folderNote("/current/themes"))).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
});

it("exposes the complete import result and folder result as status updates", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "importTheme").mockResolvedValue(result);
  vi.spyOn(port, "openThemeFolder").mockResolvedValue("/current/themes");
  draw(port);
  choose();
  await screen.findByText(imported());
  expect(screen.getByRole("status").getAttribute("aria-atomic")).toBe("true");
  expect(screen.getByRole("status").textContent).toBe(imported());
  await userEvent.click(screen.getByRole("button", { name: t("打开主题目录") }));
  expect(screen.getByRole("status").textContent).toBe(folderNote("/current/themes"));
});

it("reports a current folder failure through the existing alert", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "openThemeFolder").mockRejectedValue(new Error("current folder request failed"));
  draw(port);
  await userEvent.click(screen.getByRole("button", { name: t("打开主题目录") }));
  expect(screen.getByRole("alert").textContent).toBe("current folder request failed");
});

it("blocks same-turn duplicate selections and folder reveal before the busy render", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const upload = pending<ImportResult>();
  const take = vi.spyOn(port, "importTheme").mockReturnValue(upload.promise);
  const reveal = vi.spyOn(port, "openThemeFolder").mockResolvedValue("/current/themes");
  draw(port);
  const folder = screen.getByRole("button", { name: t("打开主题目录") });
  act(() => {
    choose();
    choose();
    fireEvent.click(folder);
  });
  expect(take).toHaveBeenCalledTimes(1);
  expect(reveal).not.toHaveBeenCalled();
  await act(async () => upload.resolve(result));
  expect(screen.getByRole("status").textContent).toBe(imported());
});
