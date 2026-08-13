export type DiagnosticsV2SchemaState = {
  state: "absent" | "partial" | "complete";
  missing: string[];
};

export const diagnosticsV2SchemaEntries: readonly string[];
export const diagnosticsV2SchemaQuery: string;

export function parseWranglerRows(output: string): Array<Record<string, unknown>>;
export function classifyDiagnosticsV2Schema(
  rows: Array<Record<string, unknown>>,
): DiagnosticsV2SchemaState;
