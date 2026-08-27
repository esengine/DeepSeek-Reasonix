export const firebaseOAuthGrantType: string;
export const wranglerD1MaxBufferBytes: number;

export function assessMigrationCapacity(
  rows: Array<Record<string, unknown>>,
  now?: Date,
): {
  statusTotals: Record<"open" | "resolved" | "ignored" | "other", number>;
  ageByStatus: Record<
    "open" | "resolved" | "ignored" | "other",
    Record<"under30d" | "days30to59d" | "days60plus" | "invalid", number>
  >;
  activeReasons: {
    open: number;
    otherStatus: number;
    recentResolvedOrIgnored: number;
    invalidResolvedOrIgnored: number;
  };
  automaticRetention: { compacted: number; archived: number };
  manualReview: { open30to59d: number; open60dPlus: number; otherStatus30dPlus: number };
};

export function runWrangler(
  projectDir: string,
  database: string,
  query: string,
  spawn?: (
    command: string,
    args: string[],
    options: { maxBuffer: number; [key: string]: unknown },
  ) => { error?: Error; status: number | null; stdout?: string },
): Array<Record<string, unknown>>;

export function classifyMigrationGroup(
  row: Record<string, unknown>,
  now?: Date,
): "active" | "compacted" | "archived";

export function canonicalJSONString(value: unknown): string;
export function contentDigest(value: unknown): string;

export function buildFirebaseGroups(
  groupRows: Array<Record<string, unknown>>,
  reportRows: Array<Record<string, unknown>>,
  now?: Date,
): Map<string, {
  state: "active" | "compacted" | "archived";
  value: Record<string, unknown> | null;
  firstEventId: string;
  reservedBytes: number;
}>;
