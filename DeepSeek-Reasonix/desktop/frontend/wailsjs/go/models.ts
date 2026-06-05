export namespace main {
	
	export class AgentView {
	    temperature: number;
	    maxSteps: number;
	    systemPrompt: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.temperature = source["temperature"];
	        this.maxSteps = source["maxSteps"];
	        this.systemPrompt = source["systemPrompt"];
	    }
	}
	export class BalanceInfo {
	    available: boolean;
	    display: string;
	    err?: string;
	
	    static createFrom(source: any = {}) {
	        return new BalanceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.display = source["display"];
	        this.err = source["err"];
	    }
	}
	export class SkillRootSkillView {
	    name: string;
	    description: string;
	    scope: string;
	    runAs: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillRootSkillView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.scope = source["scope"];
	        this.runAs = source["runAs"];
	    }
	}
	export class SkillRootView {
	    dir: string;
	    scope: string;
	    priority: number;
	    status: string;
	    configured: boolean;
	    skills: number;
	    skillItems?: SkillRootSkillView[];
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillRootView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.scope = source["scope"];
	        this.priority = source["priority"];
	        this.status = source["status"];
	        this.configured = source["configured"];
	        this.skills = source["skills"];
	        this.skillItems = this.convertValues(source["skillItems"], SkillRootSkillView);
	        this.warning = source["warning"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SkillView {
	    name: string;
	    description: string;
	    scope: string;
	    runAs: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.scope = source["scope"];
	        this.runAs = source["runAs"];
	    }
	}
	export class ToolView {
	    name: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	    }
	}
	export class ServerView {
	    name: string;
	    transport: string;
	    status: string;
	    builtIn?: boolean;
	    configured?: boolean;
	    autoStart: boolean;
	    tier?: string;
	    command?: string;
	    args?: string[];
	    url?: string;
	    envKeys?: string[];
	    tools: number;
	    prompts: number;
	    resources: number;
	    error?: string;
	    toolList?: ToolView[];
	    authStatus?: string;
	    authUrl?: string;
	    authConfigured?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ServerView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.transport = source["transport"];
	        this.status = source["status"];
	        this.builtIn = source["builtIn"];
	        this.configured = source["configured"];
	        this.autoStart = source["autoStart"];
	        this.tier = source["tier"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.url = source["url"];
	        this.envKeys = source["envKeys"];
	        this.tools = source["tools"];
	        this.prompts = source["prompts"];
	        this.resources = source["resources"];
	        this.error = source["error"];
	        this.toolList = this.convertValues(source["toolList"], ToolView);
	        this.authStatus = source["authStatus"];
	        this.authUrl = source["authUrl"];
	        this.authConfigured = source["authConfigured"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CapabilitiesView {
	    servers: ServerView[];
	    skills: SkillView[];
	    skillRoots: SkillRootView[];
	
	    static createFrom(source: any = {}) {
	        return new CapabilitiesView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.servers = this.convertValues(source["servers"], ServerView);
	        this.skills = this.convertValues(source["skills"], SkillView);
	        this.skillRoots = this.convertValues(source["skillRoots"], SkillRootView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CheckpointMeta {
	    turn: number;
	    prompt: string;
	    files: string[];
	    time: number;
	
	    static createFrom(source: any = {}) {
	        return new CheckpointMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.turn = source["turn"];
	        this.prompt = source["prompt"];
	        this.files = source["files"];
	        this.time = source["time"];
	    }
	}
	export class CommandInfo {
	    name: string;
	    description: string;
	    hint?: string;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CommandInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.hint = source["hint"];
	        this.kind = source["kind"];
	    }
	}
	export class ContextInfo {
	    used: number;
	    window: number;
	
	    static createFrom(source: any = {}) {
	        return new ContextInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.used = source["used"];
	        this.window = source["window"];
	    }
	}
	export class DirEntry {
	    name: string;
	    isDir: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DirEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.isDir = source["isDir"];
	    }
	}
	export class DroppedItem {
	    kind: string;
	    path: string;
	    isDir?: boolean;
	    previewUrl?: string;
	
	    static createFrom(source: any = {}) {
	        return new DroppedItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.previewUrl = source["previewUrl"];
	    }
	}
	export class EffortInfo {
	    supported: boolean;
	    current: string;
	    default: string;
	    levels: string[];
	
	    static createFrom(source: any = {}) {
	        return new EffortInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supported = source["supported"];
	        this.current = source["current"];
	        this.default = source["default"];
	        this.levels = source["levels"];
	    }
	}
	export class FilePreview {
	    path: string;
	    body: string;
	    size: number;
	    truncated: boolean;
	    binary: boolean;
	    encoding?: string;
	    err?: string;
	
	    static createFrom(source: any = {}) {
	        return new FilePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.body = source["body"];
	        this.size = source["size"];
	        this.truncated = source["truncated"];
	        this.binary = source["binary"];
	        this.encoding = source["encoding"];
	        this.err = source["err"];
	    }
	}
	export class HistoryMessage {
	    role: string;
	    content: string;
	    reasoning?: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	        this.reasoning = source["reasoning"];
	    }
	}
	export class JobView {
	    id: string;
	    kind: string;
	    label: string;
	    status: string;
	    startedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new JobView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.label = source["label"];
	        this.status = source["status"];
	        this.startedAt = source["startedAt"];
	    }
	}
	export class MCPServerInput {
	    name: string;
	    transport: string;
	    command: string;
	    args: string[];
	    url: string;
	    env: Record<string, string>;
	    tier: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPServerInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.transport = source["transport"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.url = source["url"];
	        this.env = source["env"];
	        this.tier = source["tier"];
	    }
	}
	export class MemoryDoc {
	    path: string;
	    scope: string;
	    body: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryDoc(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.scope = source["scope"];
	        this.body = source["body"];
	    }
	}
	export class MemoryFact {
	    name: string;
	    title?: string;
	    description: string;
	    type: string;
	    body: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryFact(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.type = source["type"];
	        this.body = source["body"];
	    }
	}
	export class MemoryScope {
	    scope: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryScope(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.path = source["path"];
	    }
	}
	export class MemoryView {
	    docs: MemoryDoc[];
	    facts: MemoryFact[];
	    scopes: MemoryScope[];
	    storeDir: string;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MemoryView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.docs = this.convertValues(source["docs"], MemoryDoc);
	        this.facts = this.convertValues(source["facts"], MemoryFact);
	        this.scopes = this.convertValues(source["scopes"], MemoryScope);
	        this.storeDir = source["storeDir"];
	        this.available = source["available"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Meta {
	    label: string;
	    ready: boolean;
	    startupErr?: string;
	    eventChannel: string;
	    cwd: string;
	    bypass: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Meta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.ready = source["ready"];
	        this.startupErr = source["startupErr"];
	        this.eventChannel = source["eventChannel"];
	        this.cwd = source["cwd"];
	        this.bypass = source["bypass"];
	    }
	}
	export class ModelInfo {
	    ref: string;
	    provider: string;
	    model: string;
	    current: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ModelInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ref = source["ref"];
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.current = source["current"];
	    }
	}
	export class NetworkProxyView {
	    type: string;
	    server: string;
	    port: number;
	    username: string;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new NetworkProxyView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.server = source["server"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.password = source["password"];
	    }
	}
	export class NetworkView {
	    proxyMode: string;
	    proxyUrl: string;
	    noProxy: string;
	    proxy: NetworkProxyView;
	
	    static createFrom(source: any = {}) {
	        return new NetworkView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proxyMode = source["proxyMode"];
	        this.proxyUrl = source["proxyUrl"];
	        this.noProxy = source["noProxy"];
	        this.proxy = this.convertValues(source["proxy"], NetworkProxyView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PermissionsView {
	    mode: string;
	    allow: string[];
	    ask: string[];
	    deny: string[];
	
	    static createFrom(source: any = {}) {
	        return new PermissionsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.allow = source["allow"];
	        this.ask = source["ask"];
	        this.deny = source["deny"];
	    }
	}
	export class ProviderView {
	    name: string;
	    kind: string;
	    baseUrl: string;
	    models: string[];
	    default: string;
	    apiKeyEnv: string;
	    keySet: boolean;
	    balanceUrl: string;
	    contextWindow: number;
	
	    static createFrom(source: any = {}) {
	        return new ProviderView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.baseUrl = source["baseUrl"];
	        this.models = source["models"];
	        this.default = source["default"];
	        this.apiKeyEnv = source["apiKeyEnv"];
	        this.keySet = source["keySet"];
	        this.balanceUrl = source["balanceUrl"];
	        this.contextWindow = source["contextWindow"];
	    }
	}
	export class QuestionAnswer {
	    questionId: string;
	    selected: string[];
	
	    static createFrom(source: any = {}) {
	        return new QuestionAnswer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.questionId = source["questionId"];
	        this.selected = source["selected"];
	    }
	}
	export class SandboxView {
	    bash: string;
	    network: boolean;
	    workspaceRoot: string;
	    allowWrite: string[];
	
	    static createFrom(source: any = {}) {
	        return new SandboxView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bash = source["bash"];
	        this.network = source["network"];
	        this.workspaceRoot = source["workspaceRoot"];
	        this.allowWrite = source["allowWrite"];
	    }
	}
	
	export class SessionMeta {
	    path: string;
	    preview: string;
	    title?: string;
	    turns: number;
	    createdAt: number;
	    lastActivityAt: number;
	    modTime: number;
	    current: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SessionMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.preview = source["preview"];
	        this.title = source["title"];
	        this.turns = source["turns"];
	        this.createdAt = source["createdAt"];
	        this.lastActivityAt = source["lastActivityAt"];
	        this.modTime = source["modTime"];
	        this.current = source["current"];
	    }
	}
	export class SettingsView {
	    defaultModel: string;
	    plannerModel: string;
	    fileEncoding: string;
	    providers: ProviderView[];
	    permissions: PermissionsView;
	    sandbox: SandboxView;
	    network: NetworkView;
	    agent: AgentView;
	    configPath: string;
	    providerKinds: string[];
	    bypass: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SettingsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.defaultModel = source["defaultModel"];
	        this.plannerModel = source["plannerModel"];
	        this.fileEncoding = source["fileEncoding"];
	        this.providers = this.convertValues(source["providers"], ProviderView);
	        this.permissions = this.convertValues(source["permissions"], PermissionsView);
	        this.sandbox = this.convertValues(source["sandbox"], SandboxView);
	        this.network = this.convertValues(source["network"], NetworkView);
	        this.agent = this.convertValues(source["agent"], AgentView);
	        this.configPath = source["configPath"];
	        this.providerKinds = source["providerKinds"];
	        this.bypass = source["bypass"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	export class SlashArgItem {
	    label: string;
	    insert: string;
	    hint: string;
	    descend: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SlashArgItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.insert = source["insert"];
	        this.hint = source["hint"];
	        this.descend = source["descend"];
	    }
	}
	export class SlashArgsResult {
	    items: SlashArgItem[];
	    from: number;
	
	    static createFrom(source: any = {}) {
	        return new SlashArgsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], SlashArgItem);
	        this.from = source["from"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class UpdateInfo {
	    available: boolean;
	    current: string;
	    latest: string;
	    notes: string;
	    canSelfUpdate: boolean;
	    downloadUrl: string;
	    assetSize: number;
	    err?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.notes = source["notes"];
	        this.canSelfUpdate = source["canSelfUpdate"];
	        this.downloadUrl = source["downloadUrl"];
	        this.assetSize = source["assetSize"];
	        this.err = source["err"];
	    }
	}
	export class WorkspaceChangeView {
	    path: string;
	    oldPath?: string;
	    sources: string[];
	    gitStatus?: string;
	    turns?: number[];
	    latestPrompt?: string;
	    latestTime?: number;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceChangeView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	        this.sources = source["sources"];
	        this.gitStatus = source["gitStatus"];
	        this.turns = source["turns"];
	        this.latestPrompt = source["latestPrompt"];
	        this.latestTime = source["latestTime"];
	    }
	}
	export class WorkspaceChangesView {
	    files: WorkspaceChangeView[];
	    gitAvailable: boolean;
	    gitErr?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceChangesView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.files = this.convertValues(source["files"], WorkspaceChangeView);
	        this.gitAvailable = source["gitAvailable"];
	        this.gitErr = source["gitErr"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkspaceMeta {
	    path: string;
	    name: string;
	    current: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.current = source["current"];
	    }
	}

}

