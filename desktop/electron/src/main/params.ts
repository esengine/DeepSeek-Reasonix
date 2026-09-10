export type Params = Record<string, unknown>;

export function record(value: unknown): Params {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? (value as Params) : {};
}

export function str(params: Params, key: string, fallback = ""): string {
  const value = params[key];
  return typeof value === "string" ? value : fallback;
}

export function num(params: Params, key: string, fallback = 0): number {
  const value = params[key];
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

export function bool(params: Params, key: string, fallback = false): boolean {
  const value = params[key];
  return typeof value === "boolean" ? value : fallback;
}

export function strList(params: Params, key: string): string[] {
  const value = params[key];
  return Array.isArray(value) ? value.filter((entry): entry is string => typeof entry === "string") : [];
}

export function finite(value: unknown, fallback = 0): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}
