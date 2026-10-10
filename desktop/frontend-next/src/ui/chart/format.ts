import { count, plain } from "../../i18n/format";
import type { Cell, ChartAxis } from "./spec";

export function formatValue(v: number, axis?: ChartAxis): string {
  const body = axis?.format === "integer" ? count(v) : plain(v, 2);
  if (axis?.format === "percent") return `${body}%`;
  return axis?.unit ? `${body} ${axis.unit}` : body;
}

export const cellText = (c: Cell): string => (c === null ? "" : typeof c === "number" ? plain(c, 6) : c);

export function clip(s: string, max: number): string {
  const chars = [...s];
  return chars.length > max ? `${chars.slice(0, max - 1).join("")}…` : s;
}
