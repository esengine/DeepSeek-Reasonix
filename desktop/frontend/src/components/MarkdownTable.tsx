import { memo, type ReactNode } from "react";
import type { VirtualMarkdownTableData } from "../lib/largeMarkdownTable";

export const MarkdownTable = memo(function MarkdownTable({ children }: { children?: ReactNode }) {
  return <div className="md-table-scroll"><table>{children}</table></div>;
});

/** Large plain tables retain the fast worker parse, with ordinary DOM rows. */
export const MarkdownSourceTable = memo(function MarkdownSourceTable({ data }: { data: VirtualMarkdownTableData }) {
  return <div className="md-table-scroll" data-markdown-source-rows={data.rows.length}><table>
    <thead><tr>{data.header.map((cell, index) => <th key={index} align={data.align[index] ?? undefined}>{cell}</th>)}</tr></thead>
    <tbody>{data.rows.map((row, index) => <tr key={index}>{row.map((cell, column) =>
      <td key={column} align={data.align[column] ?? undefined}>{cell}</td>)}</tr>)}</tbody>
  </table></div>;
});
