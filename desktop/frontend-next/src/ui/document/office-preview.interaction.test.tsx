// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import ExcelJS from "exceljs";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MockPort } from "../../port/mock";
import { setPreviewsOffice } from "../../state/prefs";
import DocumentPreview from "./DocumentPreview";
import { previewKind } from "./registry";

beforeEach(() => localStorage.clear());
afterEach(() => cleanup());

async function sheetBytes(): Promise<ArrayBuffer> {
  const book = new ExcelJS.Workbook();
  book.addWorksheet("Q3").addRow(["区域", 1100]);
  return new Uint8Array((await book.xlsx.writeBuffer()) as ArrayBuffer).slice().buffer;
}

function officePort(data: ArrayBuffer) {
  const port = new MockPort();
  vi.spyOn(port, "workspacePreviewInfo").mockResolvedValue({ ref: { path: "budget.xlsx" }, kind: "office", version: "v1", size: data.byteLength, limit: 32 << 20, readOnly: false });
  const content = vi.spyOn(port, "workspacePreviewContent").mockResolvedValue({ data, version: "v1" });
  return { port, content };
}

it("classes the formats it can read as office and leaves the rest unsupported", () => {
  expect(previewKind("报告.docx")).toBe("office");
  expect(previewKind("budget.XLSX")).toBe("office");
  for (const name of ["old.doc", "old.xls", "deck.pptx", "old.ppt"]) expect(previewKind(name)).toBe("unsupported");
});

it("renders a workbook in a sandboxed frame", async () => {
  const { port } = officePort(await sheetBytes());
  render(<DocumentPreview port={port} path="budget.xlsx" shown refresh="" view={{}} />);
  const frame = await screen.findByTitle("Office 文档预览", undefined, { timeout: 5000 });
  expect(frame.getAttribute("sandbox")).toBe("");
  expect(frame.getAttribute("srcdoc")).toContain("1100");
  expect(frame.getAttribute("srcdoc")).toContain("Content-Security-Policy");
});

it("with the setting off reads no content, and turning it on loads the preview", async () => {
  setPreviewsOffice(false);
  const { port, content } = officePort(await sheetBytes());
  render(<DocumentPreview port={port} path="budget.xlsx" shown refresh="" view={{}} />);
  await screen.findByText(/没有开启 Office 文档预览/);
  expect(content).not.toHaveBeenCalled();
  act(() => setPreviewsOffice(true));
  await waitFor(() => expect(content).toHaveBeenCalled());
  expect(await screen.findByTitle("Office 文档预览", undefined, { timeout: 5000 })).toBeTruthy();
});

it("says so instead of breaking when the file is not a real workbook", async () => {
  const { port } = officePort(new TextEncoder().encode("not a zip").buffer);
  render(<DocumentPreview port={port} path="budget.xlsx" shown refresh="" view={{}} />);
  expect(await screen.findByText("无法解析这个文件，可以用系统应用打开", undefined, { timeout: 5000 })).toBeTruthy();
});

async function twoSheets(): Promise<ArrayBuffer> {
  const book = new ExcelJS.Workbook();
  book.addWorksheet("用量").addRow(["月份", 9]);
  book.addWorksheet("明细").addRow(["请求", 12480]);
  return new Uint8Array((await book.xlsx.writeBuffer()) as ArrayBuffer).slice().buffer;
}

it("keeps the read-only note at the top and switches sheets from the bottom", async () => {
  const { port } = officePort(await twoSheets());
  const { container } = render(<DocumentPreview port={port} path="budget.xlsx" shown refresh="" view={{}} />);
  const frame = await screen.findByTitle("Office 文档预览", undefined, { timeout: 5000 });
  const head = container.querySelector(".office-head")!;
  expect(within(head as HTMLElement).getByText("只读")).toBeTruthy();
  expect(head.textContent).toContain("只读预览，宏、脚本和外部资源未加载");
  const tabs = screen.getByRole("tablist", { name: "工作表" });
  expect(frame.compareDocumentPosition(tabs) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  fireEvent.click(within(tabs).getByRole("tab", { name: "明细" }));
  await waitFor(() => expect(screen.getByTitle("Office 文档预览").getAttribute("srcdoc")).toContain("12480"));
});

it("with the setting off names the file and offers both ways out", async () => {
  setPreviewsOffice(false);
  const { port } = officePort(await sheetBytes());
  const onOpenExternal = vi.fn();
  const onSettings = vi.fn();
  render(<DocumentPreview port={port} path="docs/report.docx" shown refresh="" view={{}} onOpenExternal={onOpenExternal} onSettings={onSettings} />);
  const card = (await screen.findByText(/没有开启 Office 文档预览/)).closest(".office-off")!;
  expect(card.textContent).toContain("report.docx");
  fireEvent.click(within(card as HTMLElement).getByRole("button", { name: "用系统应用打开" }));
  expect(onOpenExternal).toHaveBeenCalled();
  fireEvent.click(within(card as HTMLElement).getByRole("button", { name: "去设置开启预览" }));
  expect(onSettings).toHaveBeenCalledWith("appearance");
});
