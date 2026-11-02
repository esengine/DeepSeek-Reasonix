// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
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
  await screen.findByText("Office 文档预览已在设置中关闭");
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
