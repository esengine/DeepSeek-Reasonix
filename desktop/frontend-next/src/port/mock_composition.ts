import { HttpError } from "./http_error";
import revisions from "../../../../internal/contract/agentcomposition/builtin-revisions.json";
import { builtinCompositions } from "./composition";
import type { AgentComposition, AgentPresetVersion, AgentPresetCatalog, CompositionSummary } from "./composition";

type Library = { revision:number; user:AgentPresetVersion[]; projects:Map<string,AgentPresetVersion[]>; personalDefault:string; workspaceDefaults:Map<string,string> };

export class MockCompositions {
  private current: CompositionSummary;
  constructor(private readonly library:Library = {revision:0,user:[],projects:new Map(),personalDefault:"builtin:general",workspaceDefaults:new Map()},private readonly workspace = "") {
    const catalog=this.catalog;
    const v=catalog.presets.find((v)=>v.definition.id===catalog.default) ?? catalog.presets[0];
    this.current=this.summary(v);
  }
  fork(workspace:string) { return new MockCompositions(this.library,workspace); }
  private get catalog():AgentPresetCatalog {
    return {presets:[...builtinCompositions().map((definition)=>({definition,revision:revisions[definition.id as keyof typeof revisions]})),...this.library.user,...(this.library.projects.get(this.workspace) ?? [])],default:this.library.workspaceDefaults.get(this.workspace) ?? this.library.personalDefault,diagnostics:[]};
  }
  private summary(v:AgentPresetVersion):CompositionSummary { return {id:v.definition.id,name:v.definition.name,revision:v.revision,digest:`mock-${v.revision}`,started:false}; }
  private fail(status:number,code:string):never { throw new HttpError(status,"",{code:`agent_composition.${code}`}); }
  async agentPresets() { return structuredClone(this.catalog); }
  async saveAgentPreset(definition: AgentComposition, expectedRevision: string): Promise<AgentPresetVersion> {
    if (definition.id.startsWith("builtin:")) this.fail(403,"read_only");
    const prior=this.catalog.presets.find((v)=>v.definition.id===definition.id);
    if ((prior?.revision ?? "")!==expectedRevision) this.fail(409,"conflict");
    if (!/^(user|project):[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/.test(definition.id) || !definition.name.trim() || definition.persona.complete && !definition.persona.prefix.trim()) this.fail(400,"invalid");
    const version={definition:structuredClone(definition),revision:`mock-${++this.library.revision}`};
    const items=definition.id.startsWith("project:") ? this.library.projects.get(this.workspace) ?? [] : this.library.user;
    const next=[...items.filter((v)=>v.definition.id!==definition.id),version];
    if (definition.id.startsWith("project:")) this.library.projects.set(this.workspace,next); else this.library.user=next;
    return structuredClone(version);
  }
  async deleteAgentPreset(id:string,revision:string) {
    if (id.startsWith("builtin:")) this.fail(403,"read_only");
    const old=this.catalog.presets.find((v)=>v.definition.id===id);
    if (!old || old.revision!==revision) this.fail(409,"conflict");
    this.library.user=this.library.user.filter((v)=>v.definition.id!==id);
    this.library.projects.set(this.workspace,(this.library.projects.get(this.workspace) ?? []).filter((v)=>v.definition.id!==id));
  }
  async setDefaultAgentPreset(id:string,workspace:boolean) {
    if (!this.catalog.presets.some((v)=>v.definition.id===id)) this.fail(404,"not_found");
    if (!workspace && id.startsWith("project:")) this.fail(400,"invalid");
    if (workspace) this.library.workspaceDefaults.set(this.workspace,id); else this.library.personalDefault=id;
  }
  async exportAgentPreset(id:string) {
    const v=this.catalog.presets.find((v)=>v.definition.id===id);
    if (!v) this.fail(404,"not_found");
    return structuredClone(v.definition);
  }
  async importAgentPreset(content:AgentComposition) {
    if (content.schemaVersion!==1 || !content.name || !content.persona) this.fail(400,"invalid");
    return {definition:structuredClone(content),diagnostics:[]};
  }
  async agentComposition() { return structuredClone(this.current); }
  async selectAgentComposition(id:string) {
    const v=this.catalog.presets.find((v)=>v.definition.id===id);
    if (!v) this.fail(404,"not_found");
    if (this.current.id===id && this.current.revision===v.revision) return structuredClone(this.current);
    if (this.current.started) this.fail(409,"locked");
    this.current=this.summary(v);
    return structuredClone(this.current);
  }
  markCompositionStarted() { this.current.started=true; }
}
