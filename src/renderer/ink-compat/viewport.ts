import { createContext, useContext } from "react";

export interface ViewportSize {
  readonly columns: number;
  readonly rows: number;
}

export const ViewportContext = createContext<ViewportSize>({ columns: 80, rows: 24 });

export function useStdout(): { stdout: { columns: number; rows: number } } {
  const v = useContext(ViewportContext);
  return { stdout: { columns: v.columns, rows: v.rows } };
}
