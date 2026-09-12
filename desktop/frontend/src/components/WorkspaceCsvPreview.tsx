import { useMemo } from "react";

const MAX_ROWS = 2_000;
const MAX_COLUMNS = 200;

export function parseDelimitedText(input: string, delimiter: "," | "\t"): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let quoted = false;
  for (let index = 0; index < input.length; index += 1) {
    const char = input[index];
    if (quoted) {
      if (char === '"') {
        if (input[index + 1] === '"') {
          cell += '"';
          index += 1;
        } else {
          quoted = false;
        }
      } else {
        cell += char;
      }
      continue;
    }
    if (char === '"' && cell === "") {
      quoted = true;
    } else if (char === delimiter) {
      row.push(cell);
      cell = "";
    } else if (char === "\n") {
      row.push(cell.replace(/\r$/, ""));
      rows.push(row.slice(0, MAX_COLUMNS));
      row = [];
      cell = "";
      if (rows.length >= MAX_ROWS) break;
    } else {
      cell += char;
    }
  }
  if (rows.length < MAX_ROWS && (cell !== "" || row.length > 0)) {
    row.push(cell.replace(/\r$/, ""));
    rows.push(row.slice(0, MAX_COLUMNS));
  }
  return rows;
}

export function WorkspaceCsvPreview({ body, delimiter }: { body: string; delimiter: "," | "\t" }) {
  const rows = useMemo(() => parseDelimitedText(body, delimiter), [body, delimiter]);
  if (rows.length === 0) return <div className="workspace-empty">—</div>;
  const [header, ...data] = rows;
  return (
    <div className="workspace-csv-preview">
      <table>
        <thead><tr>{header.map((value, index) => <th key={index}>{value}</th>)}</tr></thead>
        <tbody>
          {data.map((values, rowIndex) => (
            <tr key={rowIndex}>{header.map((_, columnIndex) => <td key={columnIndex}>{values[columnIndex] ?? ""}</td>)}</tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
