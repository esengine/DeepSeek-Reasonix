// @vitest-environment jsdom
import { expect, it } from "vitest";
import { cellRange, markCells, sheetSelection, wordSelection } from "./officeSelection";

it("normalises two cell addresses into one rectangle", () => {
  expect(cellRange("D4", "B2")?.text).toBe("B2:D4");
  expect(cellRange("B4", "D2")?.text).toBe("B2:D4");
  expect(cellRange("C3", "C3")?.text).toBe("C3");
  expect(cellRange("Z1", "AB2")?.text).toBe("Z1:AB2");
  expect(cellRange("B2", "nope")).toBeUndefined();
});

function sheet(): Document {
  const doc = document.implementation.createHTMLDocument();
  doc.body.innerHTML = '<table><tbody>'
    + '<tr><th>1</th><td data-cell="A1">区域</td><td data-cell="B1">7月</td></tr>'
    + '<tr><th>2</th><td data-cell="A2">华东</td><td data-cell="B2">1,100</td></tr>'
    + '</tbody></table>';
  return doc;
}

it("quotes a cell range as tab-separated rows with its sheet and range", () => {
  const doc = sheet();
  const picked = sheetSelection(doc, "Q3", "B2", "A1");
  expect(picked?.text).toBe("区域\t7月\n华东\t1,100");
  expect(picked?.position).toBe('sheet "Q3", cells A1:B2');
  markCells(doc, "A2", "B2");
  expect([...doc.querySelectorAll(".sel")].map((el) => el.getAttribute("data-cell"))).toEqual(["A2", "B2"]);
  markCells(doc, "A1", "A1");
  expect([...doc.querySelectorAll(".sel")].map((el) => el.getAttribute("data-cell"))).toEqual(["A1"]);
});

function word(): Document {
  const doc = document;
  doc.body.innerHTML = '<section class="docx"><header><p>页眉</p></header><article>'
    + '<p id="b1">标题</p><p id="b2">第一段</p><table id="b3"><tr><td>表格</td></tr></table></article></section>'
    + '<section class="docx"><article><p id="b4">第二页</p></article></section>';
  return doc;
}

it("names the body blocks a Word selection covers, across pages and past the header", () => {
  const doc = word();
  const select = (from: string, to: string) => {
    const range = doc.createRange();
    range.setStart(doc.getElementById(from)!.firstChild!, 0);
    const end = doc.getElementById(to)!;
    range.setEnd(end.querySelector("td")?.firstChild ?? end.firstChild!, 1);
    doc.getSelection()!.removeAllRanges();
    doc.getSelection()!.addRange(range);
  };
  select("b2", "b2");
  expect(wordSelection(doc)?.position).toBe("body block 2");
  select("b2", "b4");
  expect(wordSelection(doc)?.position).toBe("body blocks 2-4");
  select("b3", "b3");
  expect(wordSelection(doc)?.position).toBe("body block 3");
  const triple = doc.createRange();
  triple.setStart(doc.getElementById("b2")!.firstChild!, 0);
  triple.setEnd(doc.getElementById("b3")!, 0);
  doc.getSelection()!.removeAllRanges();
  doc.getSelection()!.addRange(triple);
  expect(wordSelection(doc)?.position).toBe("body block 2");
  doc.getSelection()!.removeAllRanges();
  expect(wordSelection(doc)).toBeUndefined();
});
