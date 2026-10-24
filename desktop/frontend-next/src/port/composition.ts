import definitions from "../../../../internal/contract/agentcomposition/builtins.json";

export type CompositionKind = "tools" | "skills" | "mcp" | "extensions" | "hooks" | "commands";
export interface CompositionModule { id: string; parameters?: Record<string, unknown>; optional?: boolean }
// A module the editor offers: the id is what a preset stores; the description is what the module
// says about itself, for the person choosing, and is never saved.
export interface CompositionChoice { id: string; description?: string; source?: "builtin" | "custom" }
export interface CompositionSelection { mode: "inherit" | "select"; members: CompositionModule[] }
export interface AgentComposition {
  schemaVersion: number;
  id: string;
  name: string;
  description: string;
  persona: { prefix: string; suffix: string; include_runtime_context: boolean };
  tools: CompositionSelection;
  skills: CompositionSelection;
  mcp: CompositionSelection;
  extensions: CompositionSelection;
  hooks: CompositionSelection;
  commands: CompositionSelection;
}
export interface AgentPresetVersion { definition: AgentComposition; revision: string }
export interface AgentPresetCatalog {
  presets: AgentPresetVersion[];
  default: string;
  diagnostics: { code: string; module?: string; detail: string }[];
  modules?: Partial<Record<CompositionKind,CompositionChoice[]>>;
}
export interface AgentPresetImport {definition:AgentComposition;diagnostics:AgentPresetCatalog["diagnostics"]}
export interface CompositionSummary { id: string; name: string; revision: string; digest: string; started: boolean; unavailable?: string; formatVersion?: number; resourceMode?: "live" }
export const COMPOSITION_KINDS: CompositionKind[] = ["tools", "skills", "mcp", "extensions", "hooks", "commands"];
export const builtinCompositions = (): AgentComposition[] => structuredClone(definitions) as AgentComposition[];
