// @vitest-environment jsdom
import ExcelJS from "exceljs";
import JSZip from "jszip";
import { expect, it } from "vitest";
import { displayText, readWorkbook, SHEET_ROW_LIMIT } from "./officeSheet";
import { fitZoom, readWordDocument } from "./officeWord";
import { staticDocument } from "./staticDocument";

async function workbook(build: (book: ExcelJS.Workbook) => void): Promise<ArrayBuffer> {
  const book = new ExcelJS.Workbook();
  build(book);
  const buf = await book.xlsx.writeBuffer();
  return new Uint8Array(buf as ArrayBuffer).slice().buffer;
}

const parse = (html: string) => new DOMParser().parseFromString(staticDocument(html), "text/html");

it("shows a cell the way its own number format writes it", () => {
  expect(displayText(1100, "#,##0")).toBe("1,100");
  expect(displayText(0.31, "0.0%")).toBe("31.0%");
  expect(displayText(-0.04, "0.0%")).toBe("-4.0%");
  expect(displayText(1100)).toBe("1100");
  expect(displayText({ formula: "SUM(A1:A2)", result: 2500 } as ExcelJS.CellValue, "#,##0")).toBe("2,500");
  expect(displayText({ richText: [{ text: "华" }, { text: "东" }] } as ExcelJS.CellValue)).toBe("华东");
  expect(displayText(new Date(Date.UTC(2026, 9, 1)), "yyyy-mm-dd")).toBe("2026-10-01");
  expect(displayText(12, "[bogus")).toBe("12");
});

it("renders sheets with merges, styles and escaped text, skipping hidden ones", async () => {
  const data = await workbook((book) => {
    const ws = book.addWorksheet("Q3");
    ws.addRow(["区域", "7月"]);
    ws.addRow(["<script>alert(1)</script>", 1100]);
    ws.getCell("B2").numFmt = "#,##0";
    ws.getCell("A1").font = { bold: true, color: { argb: "FFFFFFFF" } };
    ws.getCell("A1").fill = { type: "pattern", pattern: "solid", fgColor: { argb: "FF1F4E79" } };
    ws.mergeCells("A3:B3");
    ws.getCell("A3").value = "备注";
    book.addWorksheet("secret", { state: "hidden" }).addRow(["hidden"]);
  });
  const sheets = await readWorkbook(data);
  expect(sheets.map((s) => s.name)).toEqual(["Q3"]);
  const doc = parse(sheets[0].html);
  expect(doc.querySelector("script")).toBeNull();
  expect(doc.querySelector('[data-cell="A2"]')?.textContent).toBe("<script>alert(1)</script>");
  expect(doc.querySelector('[data-cell="B2"]')?.textContent).toBe("1,100");
  const header = doc.querySelector<HTMLElement>('[data-cell="A1"]')!;
  expect(header.style.background).toContain("rgb(31, 78, 121)");
  expect(header.style.fontWeight).toBe("bold");
  expect(doc.querySelector('[data-cell="A3"]')?.getAttribute("colspan")).toBe("2");
  expect(doc.querySelector('[data-cell="B3"]')).toBeNull();
});

it("clips a sheet past the row limit and says so", async () => {
  const data = await workbook((book) => {
    const ws = book.addWorksheet("big");
    for (let i = 0; i < SHEET_ROW_LIMIT + 5; i++) ws.addRow([i]);
  });
  const [sheet] = await readWorkbook(data);
  expect(sheet.clipped).toBe(true);
  expect(sheet.rows).toBe(SHEET_ROW_LIMIT + 5);
  expect(parse(sheet.html).querySelectorAll("tbody tr")).toHaveLength(SHEET_ROW_LIMIT);
});

async function wordFile(body: string): Promise<ArrayBuffer> {
  const zip = new JSZip();
  zip.file("[Content_Types].xml", '<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>');
  zip.file("_rels/.rels", '<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>');
  zip.file("word/document.xml", `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>${body}</w:body></w:document>`);
  return zip.generateAsync({ type: "arraybuffer" });
}

it("renders a Word body to static HTML that keeps its text and runs nothing", async () => {
  const data = await wordFile('<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>季度报告</w:t></w:r><w:r><w:t xml:space="preserve"> &lt;img src=x onerror=alert(1)&gt;</w:t></w:r></w:p>');
  const word = await readWordDocument(data);
  const doc = parse(word.html);
  expect(doc.body.textContent).toContain("季度报告");
  expect(doc.body.textContent).toContain("<img src=x onerror=alert(1)>");
  expect(doc.querySelector("img,script,[onerror]")).toBeNull();
  expect(doc.querySelector('meta[http-equiv="Content-Security-Policy"]')).not.toBeNull();
});

it("refuses something that is not an Office package", async () => {
  await expect(readWorkbook(new TextEncoder().encode("not a zip").buffer)).rejects.toThrow();
  await expect(readWordDocument(new TextEncoder().encode("not a zip").buffer)).rejects.toThrow();
});

it("draws Symbol-font bullets as the glyphs they stand for", async () => {
  const data = await wordFile('<w:p><w:r><w:t>\uF0B7 one \uF0A7 two</w:t></w:r></w:p>');
  const { html } = await readWordDocument(data);
  expect(html).toContain("• one ▪ two");
  expect(html).not.toMatch(/[\uF020-\uF0FF]/);
});

it("fits a page to the frame in 5% steps and never past 100%", () => {
  expect(fitZoom(848, 816)).toBe(1);
  expect(fitZoom(2000, 816)).toBe(1);
  expect(fitZoom(520, 816)).toBe(0.55);
  expect(fitZoom(100, 816)).toBe(0.25);
  expect(fitZoom(520, 0)).toBe(1);
  expect(fitZoom(0, 816)).toBe(1);
});
