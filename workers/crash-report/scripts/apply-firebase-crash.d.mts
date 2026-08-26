export type FirebaseCrashSchemaState = {
  state: "absent" | "partial" | "complete";
  missing: string[];
};

export const firebaseCrashSchemaEntries: readonly string[];
export const firebaseCrashV1SchemaEntries: readonly string[];
export const firebaseCrashV2SchemaEntries: readonly string[];
export const firebaseCrashSchemaQuery: string;
export const firebaseCrashCapacityQuery: string;

export function classifyFirebaseCrashSchema(
  rows: Array<Record<string, unknown>>,
): FirebaseCrashSchemaState & {
  v1: FirebaseCrashSchemaState;
  v2: FirebaseCrashSchemaState;
};
