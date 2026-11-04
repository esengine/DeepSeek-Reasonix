// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import "../testkit";
import { afterEach, describe, expect, it, vi } from "vitest";

const pdf = vi.hoisted(() => ({ load: vi.fn(), destroy: vi.fn() }));
vi.mock("pdfjs-dist", () => ({
  GlobalWorkerOptions: {}, TextLayer: class { render() { return Promise.resolve(); } cancel() {} }, getDocument: pdf.load,
}));
import PdfPreview from "./PdfPreview";

afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.clearAllMocks(); });

it("keeps XFA off and releases the loading task when the file changes or closes", () => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  pdf.load.mockImplementation(() => ({ promise: new Promise(() => {}), destroy: pdf.destroy }));
  const data = new ArrayBuffer(16);
  const { rerender, unmount } = render(<PdfPreview data={data} view={{}} />);
  expect(pdf.load).toHaveBeenCalledWith(expect.objectContaining({ enableXfa: false }));
  expect(pdf.load.mock.calls[0][0].data).not.toBe(data);
  rerender(<PdfPreview data={new ArrayBuffer(32)} view={{}} />);
  expect(pdf.destroy).toHaveBeenCalledTimes(1);
  expect(pdf.load).toHaveBeenLastCalledWith(expect.objectContaining({ enableXfa: false }));
  unmount();
  expect(pdf.destroy).toHaveBeenCalledTimes(2);
});

describe("the PDF toolbar", () => {
  const page = { getViewport: ({ scale }: { scale: number }) => ({ width: 600 * scale, height: 800 * scale }), render: () => ({ promise: Promise.resolve(), cancel() {} }), streamTextContent: () => ({}) };
  function open(numPages = 12, onOpenExternal = vi.fn()) {
    vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
    pdf.load.mockImplementation(() => ({ promise: Promise.resolve({ numPages, getPage: async () => page }), destroy: pdf.destroy }));
    const view = {} as { page?: number; zoom?: number };
    render(<PdfPreview data={new ArrayBuffer(8)} view={view} onOpenExternal={onOpenExternal} />);
    return { view, onOpenExternal };
  }

  it("moves page by page and takes a typed page number, held to the document", async () => {
    const { view } = open();
    const box = await screen.findByRole("textbox", { name: "页码" });
    await waitFor(() => expect(screen.getByText("/ 12")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect((box as HTMLInputElement).value).toBe("2");
    fireEvent.change(box, { target: { value: "40" } });
    fireEvent.keyDown(box, { key: "Enter" });
    expect((box as HTMLInputElement).value).toBe("12");
    expect(screen.getByRole("button", { name: "下一页" }).hasAttribute("disabled")).toBe(true);
    fireEvent.change(box, { target: { value: "abc" } });
    fireEvent.blur(box);
    expect((box as HTMLInputElement).value).toBe("12");
    await waitFor(() => expect(view.page).toBe(12));
  });

  it("zooms in steps from wherever fit-width left it, and goes back to fitting the width", async () => {
    const { view } = open();
    const fit = await screen.findByRole("button", { name: "适应宽度" });
    expect(fit.getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "放大" }));
    await waitFor(() => expect(view.zoom).toBeGreaterThan(0));
    expect(fit.getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(fit);
    await waitFor(() => expect(view.zoom).toBe(0));
  });

  it("offers the system application when the file cannot be shown", async () => {
    vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
    pdf.load.mockImplementation(() => ({ promise: Promise.reject(new Error("bad")), destroy: pdf.destroy }));
    const onOpenExternal = vi.fn();
    render(<PdfPreview data={new ArrayBuffer(8)} view={{}} onOpenExternal={onOpenExternal} />);
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("PDF 无法显示，文件可能损坏或需要密码");
    fireEvent.click(screen.getByRole("button", { name: "用系统应用打开" }));
    expect(onOpenExternal).toHaveBeenCalled();
  });
});
