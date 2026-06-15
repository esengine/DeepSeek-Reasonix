export type ProjectColorKey = "" | "red" | "orange" | "amber" | "green" | "teal" | "blue" | "purple" | "pink";

export interface ProjectColorOption {
  key: ProjectColorKey;
  value?: string;
}

export const PROJECT_COLOR_OPTIONS: ProjectColorOption[] = [
  { key: "" },
  { key: "red", value: "#e5534b" },
  { key: "orange", value: "#d66e4b" },
  { key: "amber", value: "#d59a2f" },
  { key: "green", value: "#4f9f64" },
  { key: "teal", value: "#1f9d93" },
  { key: "blue", value: "#3d7be0" },
  { key: "purple", value: "#8b6de8" },
  { key: "pink", value: "#cf6ca5" },
];

const colorValues = new Map(PROJECT_COLOR_OPTIONS.map((option) => [option.key, option.value]));

export function projectColorValue(key?: string): string | undefined {
  return colorValues.get((key || "") as ProjectColorKey);
}
