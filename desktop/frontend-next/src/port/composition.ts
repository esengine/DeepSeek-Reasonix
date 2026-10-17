import definitions from "../../../../internal/contract/agentcomposition/builtins.json";

export type CompositionKind = "tools" | "skills" | "mcp" | "extensions" | "hooks" | "commands";
export interface CompositionModule { id: string; parameters?: Record<string, unknown> }
export interface CompositionSelection { mode: "inherit" | "select"; members: CompositionModule[] }
export interface AgentComposition {
  schemaVersion: number;
  id: string;
  name: string;
  description: string;
  persona: { prefix: string; suffix: string; complete: boolean; include_runtime_context: boolean };
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
  modules?: Partial<Record<CompositionKind,CompositionModule[]>>;
}
export interface AgentPresetImport {definition:AgentComposition;diagnostics:AgentPresetCatalog["diagnostics"]}
export interface CompositionSummary { id: string; name: string; revision: string; digest: string; started: boolean; unavailable?: string }
export const COMPOSITION_KINDS: CompositionKind[] = ["tools", "skills", "mcp", "extensions", "hooks", "commands"];
export const builtinCompositions = (): AgentComposition[] => structuredClone(definitions) as AgentComposition[];
