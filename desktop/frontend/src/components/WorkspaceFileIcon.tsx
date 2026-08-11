import type { CSSProperties } from "react";
import setiTheme from "../assets/file-icons/seti/vs-seti-icon-theme.json";

type IconDefinition = { fontCharacter?: string; fontColor?: string };
type IconAssociations = {
  file?: string;
  fileExtensions?: Record<string, string>;
  fileNames?: Record<string, string>;
  languageIds?: Record<string, string>;
};

const theme = setiTheme as typeof setiTheme & IconAssociations & {
  iconDefinitions: Record<string, IconDefinition>;
  light?: IconAssociations;
};

const languageIdByExtension: Record<string, string> = {
  bash: "shellscript",
  c: "c",
  cc: "cpp",
  cpp: "cpp",
  cs: "csharp",
  css: "css",
  dart: "dart",
  dockerfile: "dockerfile",
  go: "go",
  gradle: "groovy",
  h: "c",
  hpp: "cpp",
  html: "html",
  java: "java",
  js: "javascript",
  json: "json",
  jsonl: "jsonl",
  jsx: "javascriptreact",
  kt: "kotlin",
  kts: "kotlin",
  less: "less",
  lua: "lua",
  md: "markdown",
  markdown: "markdown",
  php: "php",
  properties: "properties",
  ps1: "powershell",
  py: "python",
  rb: "ruby",
  rs: "rust",
  scss: "scss",
  sh: "shellscript",
  sql: "sql",
  svelte: "svelte",
  swift: "swift",
  toml: "toml",
  ts: "typescript",
  tsx: "typescriptreact",
  vue: "vue",
  xml: "xml",
  yaml: "yaml",
  yml: "yaml",
};

function extensionCandidates(fileName: string): string[] {
  const parts = fileName.split(".");
  if (parts.length < 2) return [fileName];
  const candidates: string[] = [];
  for (let index = 1; index < parts.length; index += 1) {
    candidates.push(parts.slice(index).join("."));
  }
  return candidates;
}

function associationFor(fileName: string, associations: IconAssociations | undefined): string | undefined {
  if (!associations) return undefined;
  const normalized = fileName.toLowerCase();
  const exact = associations.fileNames?.[normalized];
  if (exact) return exact;
  for (const extension of extensionCandidates(normalized)) {
    const icon = associations.fileExtensions?.[extension];
    if (icon) return icon;
  }
  const finalExtension = normalized.includes(".") ? normalized.slice(normalized.lastIndexOf(".") + 1) : normalized;
  const languageId = languageIdByExtension[finalExtension];
  return (languageId ? associations.languageIds?.[languageId] : undefined) ?? associations.file;
}

function glyphFor(definition: IconDefinition | undefined): string {
  const cssEscape = definition?.fontCharacter;
  if (!cssEscape?.startsWith("\\")) return "";
  const codePoint = Number.parseInt(cssEscape.slice(1), 16);
  return Number.isFinite(codePoint) ? String.fromCodePoint(codePoint) : "";
}

export function workspaceFileIcon(fileName: string): {
  glyph: string;
  darkColor: string;
  lightColor: string;
} {
  const darkId = associationFor(fileName, theme) ?? theme.file ?? "_default";
  const lightId = associationFor(fileName, theme.light) ?? `${darkId}_light`;
  const darkDefinition = theme.iconDefinitions[darkId] ?? theme.iconDefinitions._default;
  const lightDefinition = theme.iconDefinitions[lightId] ?? darkDefinition;
  return {
    glyph: glyphFor(darkDefinition),
    darkColor: darkDefinition?.fontColor ?? "currentColor",
    lightColor: lightDefinition?.fontColor ?? darkDefinition?.fontColor ?? "currentColor",
  };
}

export function WorkspaceFileIcon({ fileName, className = "" }: { fileName: string; className?: string }) {
  const icon = workspaceFileIcon(fileName);
  const style = {
    "--workspace-file-icon-dark": icon.darkColor,
    "--workspace-file-icon-light": icon.lightColor,
  } as CSSProperties;
  return (
    <span
      aria-hidden="true"
      className={`workspace-file-icon${className ? ` ${className}` : ""}`}
      style={style}
    >
      {icon.glyph}
    </span>
  );
}
