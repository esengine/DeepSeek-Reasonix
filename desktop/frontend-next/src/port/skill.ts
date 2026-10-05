import type { CapabilityScope, ScopeLayer } from "./mcp";

// Skills, which are prompts a project brought with it — named ones the user can
// call, and the rest the model decides about per task.
// One entry of GET /skills — every skill that may run, which is a larger set
// than /slash: a skill with no slash name still fires on model discovery.
export interface SkillEntry {
  name: string;
  slashName?: string;
  description?: string;
  scope?: string;
  plugin?: string;
  path?: string;
  subagent?: boolean;
  readOnly?: boolean;
  model?: string;
  effort?: string;
  allowedTools?: string[];
  manual?: boolean;
  enabled: boolean;
  // Where the decision behind enabled lives, or absent when nothing overrides
  // what the skill itself declares.
  switchScope?: ScopeLayer;
}

// implicit is the session-wide switch for model-initiated discovery. With it
// off every "auto" skill is manual in practice, so the rows have to say so.
export interface SkillCatalog {
  implicit: boolean;
  skills: SkillEntry[];
  scope?: CapabilityScope;
}
