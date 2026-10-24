import { HttpError } from "./http_error";
import revisions from "../../../../internal/contract/agentcomposition/builtin-revisions.json";
import { builtinCompositions } from "./composition";
import type { AgentComposition, AgentPresetVersion, AgentPresetCatalog, CompositionSummary } from "./composition";

type Library = { revision:number; user:AgentPresetVersion[]; projects:Map<string,AgentPresetVersion[]>; personalDefault:string; workspaceDefaults:Map<string,string> };

const MOCK_MODULES: AgentPresetCatalog["modules"] = {
  tools: [
    { source: "builtin", id: "bash", description: "Run a shell command in the workspace and return its output." },
    { source: "builtin", id: "bash_output", description: "Read new output from a shell command that is still running." },
    { source: "builtin", id: "browser_act", description: "Click, type or scroll on the page open in the built-in browser." },
    { source: "builtin", id: "edit_file", description: "Replace an exact piece of text in a file." },
    { source: "builtin", id: "task" },
    { source: "builtin", id: "view_image", description: "Read an image file." },
  ],
  skills: [
    { source: "builtin", id: "review", description: "Review the changes on this branch and report problems by severity." },
    { source: "builtin", id: "explore", description: "Explore the codebase and trace how a feature works." },
    { source: "builtin", id: "reasonix-guide", description: "Explain Reasonix configuration and workflows." },
    { source: "custom", id: "tidy-notes", description: "Tidy the notes folder" },
    { source: "custom", id: "api-check", description: "Validate project endpoints and response contracts." },
    { source: "custom", id: "release-check", description: "Run the team's release checklist." },
  ],
};

export class MockCompositions {
  private current: CompositionSummary | null = null;
  private started = false;
  constructor(private readonly library:Library = {revision:0,user:[],projects:new Map(),personalDefault:"builtin:general",workspaceDefaults:new Map()},private readonly workspace = "") {
  }
  get compositionStarted() { return this.started; }
  fork(workspace:string) { return new MockCompositions(this.library,workspace); }
  private get catalog():AgentPresetCatalog {
    return {presets:[...builtinCompositions().map((definition)=>({definition,revision:revisions[definition.id as keyof typeof revisions]})),...this.library.user,...(this.library.projects.get(this.workspace) ?? [])],default:this.library.workspaceDefaults.get(this.workspace) ?? this.library.personalDefault,diagnostics:[],modules:MOCK_MODULES};
  }
  private summary(v:AgentPresetVersion):CompositionSummary { return {id:v.definition.id,name:v.definition.name,revision:v.revision,digest:`mock-${v.revision}`,started:false,formatVersion:2,resourceMode:"live"}; }
  private fail(status:number,code:string):never { throw new HttpError(status,"",{code:`agent_composition.${code}`}); }
  private requireModules(definition:AgentComposition) {
    for (const kind of ["tools"] as const) for (const member of definition[kind]?.members ?? []) {
      if (definition[kind]?.mode==="select" && !member.optional && !(MOCK_MODULES?.[kind] ?? []).some((m)=>m.id===member.id)) {
        throw new HttpError(409,"",{code:"agent_composition.unavailable",params:{module:`${kind}:${member.id}`,detail:"Required module is unavailable or was revoked."}});
      }
    }
  }
  private validateDefinition(definition:AgentComposition) {
    for (const kind of ["skills","mcp","extensions","hooks","commands"] as const) {
      if (definition[kind]?.members.some((member)=>member.optional)) this.fail(400,"invalid");
    }
    if (!definition.id.startsWith("project:")) return;
    for (const kind of ["mcp","extensions"] as const) for (const member of definition[kind]?.members ?? []) {
      if (Object.entries(member.parameters ?? {}).some(([key,value])=>["args","url","env","headers"].includes(key.toLowerCase()) && value!=null)) this.fail(400,"invalid");
    }
  }
  async agentPresets() { return structuredClone(this.catalog); }
  async saveAgentPreset(definition: AgentComposition, expectedRevision: string): Promise<AgentPresetVersion> {
    if (definition.id.startsWith("builtin:")) this.fail(403,"read_only");
    const prior=this.catalog.presets.find((v)=>v.definition.id===definition.id);
    if ((prior?.revision ?? "")!==expectedRevision) this.fail(409,"conflict");
    if (!/^(user|project):[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/.test(definition.id) || !definition.name.trim()) this.fail(400,"invalid");
    this.validateDefinition(definition);
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
    this.validateDefinition(content);
    const diagnostics:AgentPresetCatalog["diagnostics"]=[];
    for (const kind of ["tools","skills","mcp","extensions","hooks","commands"] as const) for (const member of content[kind]?.members ?? []) {
      if (!(kind==="tools" && member.optional) && !(MOCK_MODULES?.[kind] ?? []).some((m)=>m.id===member.id)) diagnostics.push({code:"agent_composition.unavailable",module:`${kind}:${member.id}`,detail:"Dependency is not available on this host."});
    }
    return {definition:structuredClone(content),diagnostics};
  }
  async agentComposition() { return structuredClone(this.current); }
  async selectAgentComposition(id:string) {
    const v=this.catalog.presets.find((v)=>v.definition.id===id);
    if (!v) this.fail(404,"not_found");
    if (this.current?.id===id && this.current.revision===v.revision || !this.current && id==="builtin:general") return structuredClone(this.current);
    if (this.started) this.fail(409,"locked");
    this.requireModules(v.definition);
    this.current=id==="builtin:general" ? null : this.summary(v);
    return structuredClone(this.current);
  }
  markCompositionStarted() { this.started=true; if(this.current) this.current.started=true; }
}
