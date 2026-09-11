export type StructuredFieldValue = string | number | boolean | string[];

export type StructuredFieldIssue =
  | "required"
  | "invalid"
  | "tooShort"
  | "tooLong"
  | "belowMinimum"
  | "aboveMaximum"
  | "tooFewItems"
  | "tooManyItems"
  | "unsupported";

export type StructuredField = {
  key: string;
  label: string;
  kind: "string" | "number" | "integer" | "boolean" | "enum" | "multi-enum" | "unsupported";
  required: boolean;
  defaultValue?: StructuredFieldValue;
  options?: { label: string; value: string }[];
  format?: "email" | "uri" | "date" | "date-time";
  minLength?: number;
  maxLength?: number;
  minimum?: number;
  maximum?: number;
  minItems?: number;
  maxItems?: number;
  pattern?: string;
  description?: string;
};

export type StructuredSchemaResult = {
  fields: StructuredField[];
  unsupported: boolean;
};

function titledOptions(raw: unknown, keyword: "oneOf" | "anyOf"): StructuredField["options"] {
  const choices = (raw as Record<string, unknown>)[keyword];
  if (!Array.isArray(choices)) return undefined;
  const options = choices.flatMap((choice) => {
    if (!choice || typeof choice !== "object") return [];
    const entry = choice as Record<string, unknown>;
    if (typeof entry.const !== "string") return [];
    return [{ value: entry.const, label: typeof entry.title === "string" && entry.title ? entry.title : entry.const }];
  });
  return options.length === choices.length && options.length > 0 ? options : undefined;
}

function legacyOptions(raw: Record<string, unknown>): StructuredField["options"] {
  if (!Array.isArray(raw.enum) || raw.enum.length === 0 || !raw.enum.every((value) => typeof value === "string")) {
    return undefined;
  }
  const names = Array.isArray(raw.enumNames) ? raw.enumNames : [];
  return raw.enum.map((value, index) => ({
    value: value as string,
    label: typeof names[index] === "string" && names[index] ? String(names[index]) : (value as string),
  }));
}

function optionalNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

// MCP form elicitation deliberately supports a flat, bounded subset of JSON
// Schema. This accepts both the 2025-06-18 enumNames form and the 2025-11-25
// oneOf/anyOf select forms while failing closed for nested or unknown fields.
export function parseStructuredSchema(schema: unknown): StructuredSchemaResult {
  if (schema === undefined || schema === null) return { fields: [], unsupported: false };
  if (typeof schema !== "object" || Array.isArray(schema)) return { fields: [], unsupported: true };
  const root = schema as Record<string, unknown>;
  if (!root.properties || typeof root.properties !== "object" || Array.isArray(root.properties)) {
    return { fields: [], unsupported: true };
  }
  const required = new Set(
    Array.isArray(root.required) ? root.required.filter((value): value is string => typeof value === "string") : [],
  );
  let unsupported = false;
  const fields: StructuredField[] = [];

  for (const [key, raw] of Object.entries(root.properties as Record<string, unknown>)) {
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
      unsupported = true;
      continue;
    }
    const prop = raw as Record<string, unknown>;
    const label = typeof prop.title === "string" && prop.title ? prop.title : key;
    const base = {
      key,
      label,
      required: required.has(key),
      description: typeof prop.description === "string" ? prop.description : undefined,
    };
    const singleOptions = titledOptions(prop, "oneOf") ?? legacyOptions(prop);
    const itemSchema = prop.items && typeof prop.items === "object" && !Array.isArray(prop.items)
      ? (prop.items as Record<string, unknown>)
      : undefined;
    const multiOptions = itemSchema ? titledOptions(itemSchema, "anyOf") ?? legacyOptions(itemSchema) : undefined;

    let kind: StructuredField["kind"];
    if (prop.type === "array" && multiOptions) kind = "multi-enum";
    else if (singleOptions) kind = "enum";
    else if (prop.type === "string" || prop.type === "number" || prop.type === "integer" || prop.type === "boolean") {
      kind = prop.type;
    } else {
      kind = "unsupported";
      unsupported = true;
    }

    const allowedFormat = prop.format === "email" || prop.format === "uri" || prop.format === "date" || prop.format === "date-time"
      ? prop.format
      : undefined;
    let defaultValue: StructuredFieldValue | undefined;
    if (kind === "boolean" && typeof prop.default === "boolean") defaultValue = prop.default;
    else if ((kind === "number" || kind === "integer") && typeof prop.default === "number") defaultValue = prop.default;
    else if ((kind === "string" || kind === "enum") && typeof prop.default === "string") defaultValue = prop.default;
    else if (kind === "multi-enum" && Array.isArray(prop.default) && prop.default.every((value) => typeof value === "string")) {
      defaultValue = [...prop.default] as string[];
    }

    fields.push({
      ...base,
      kind,
      defaultValue,
      options: kind === "multi-enum" ? multiOptions : singleOptions,
      format: kind === "string" ? allowedFormat : undefined,
      minLength: optionalNumber(prop.minLength),
      maxLength: optionalNumber(prop.maxLength),
      minimum: optionalNumber(prop.minimum),
      maximum: optionalNumber(prop.maximum),
      minItems: optionalNumber(prop.minItems),
      maxItems: optionalNumber(prop.maxItems),
      pattern: typeof prop.pattern === "string" ? prop.pattern : undefined,
    });
  }
  return { fields, unsupported };
}

export function normalizeStructuredSchema(schema: unknown): StructuredField[] {
  return parseStructuredSchema(schema).fields;
}

export function initialStructuredValues(fields: StructuredField[]): Record<string, StructuredFieldValue> {
  const values: Record<string, StructuredFieldValue> = {};
  for (const field of fields) {
    if (field.defaultValue !== undefined) {
      values[field.key] = Array.isArray(field.defaultValue) ? [...field.defaultValue] : field.defaultValue;
    } else if (field.kind === "boolean") {
      // An unchecked checkbox is an explicit false, including for a required
      // JSON property; requiring it to be checked would change its meaning.
      values[field.key] = false;
    }
  }
  return values;
}

function matchesFormat(format: StructuredField["format"], value: string): boolean {
  if (!format) return true;
  if (format === "email") return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value);
  if (format === "date") return /^\d{4}-\d{2}-\d{2}$/.test(value) && !Number.isNaN(Date.parse(`${value}T00:00:00Z`));
  if (format === "date-time") return !Number.isNaN(Date.parse(value));
  try {
    new URL(value);
    return true;
  } catch {
    return false;
  }
}

export function structuredFieldIssue(field: StructuredField, value: StructuredFieldValue | undefined): StructuredFieldIssue | undefined {
  if (field.kind === "unsupported") return "unsupported";
  if (value === undefined || value === "") return field.required ? "required" : undefined;
  if (field.kind === "multi-enum") {
    if (!Array.isArray(value)) return "invalid";
    if (field.minItems !== undefined && value.length < field.minItems) return "tooFewItems";
    if (field.maxItems !== undefined && value.length > field.maxItems) return "tooManyItems";
    if (field.options && value.some((entry) => !field.options?.some((option) => option.value === entry))) return "invalid";
    return undefined;
  }
  if (field.kind === "boolean") return typeof value === "boolean" ? undefined : "invalid";
  if (field.kind === "number" || field.kind === "integer") {
    const number = typeof value === "number" ? value : Number(value);
    if (!Number.isFinite(number) || (field.kind === "integer" && !Number.isInteger(number))) return "invalid";
    if (field.minimum !== undefined && number < field.minimum) return "belowMinimum";
    if (field.maximum !== undefined && number > field.maximum) return "aboveMaximum";
    return undefined;
  }
  const text = String(value);
  if (field.minLength !== undefined && text.length < field.minLength) return "tooShort";
  if (field.maxLength !== undefined && text.length > field.maxLength) return "tooLong";
  if (field.kind === "enum" && field.options && !field.options.some((option) => option.value === text)) return "invalid";
  if (!matchesFormat(field.format, text)) return "invalid";
  if (field.pattern) {
    try {
      if (!new RegExp(field.pattern).test(text)) return "invalid";
    } catch {
      return "unsupported";
    }
  }
  return undefined;
}

export function missingStructuredRequired(fields: StructuredField[], values: Record<string, StructuredFieldValue>): string[] {
  return fields.filter((field) => structuredFieldIssue(field, values[field.key]) === "required").map((field) => field.label);
}

export function coerceStructuredValues(
  fields: StructuredField[],
  values: Record<string, StructuredFieldValue>,
): { content: Record<string, unknown>; invalid: string[] } {
  const content: Record<string, unknown> = {};
  const invalid: string[] = [];
  for (const field of fields) {
    const value = values[field.key];
    const issue = structuredFieldIssue(field, value);
    if (issue && issue !== "required") {
      invalid.push(field.label);
      continue;
    }
    if (value === undefined || value === "") continue;
    if (field.kind === "number" || field.kind === "integer") content[field.key] = Number(value);
    else if (Array.isArray(value)) content[field.key] = [...value];
    else content[field.key] = value;
  }
  return { content, invalid };
}
