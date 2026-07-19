export type NavItem = {
  href: string;
  label: string;
  eyebrow: string;
  description: string;
};

export type SearchRecord = {
  title: string;
  description: string;
  href: string;
  section: string;
  keywords: string;
};

export const repositoryUrl = "https://github.com/esengine/DeepSeek-Reasonix";
export const baseline = "main-v2 @ 988190f3";

export const navItems: NavItem[] = [
  {
    href: "/",
    label: "项目入口",
    eyebrow: "START",
    description: "先建立全局地图，再选择自己的阅读路线。",
  },
  {
    href: "/architecture",
    label: "总体架构",
    eyebrow: "01",
    description: "分层、启动装配、模块边界与依赖方向。",
  },
  {
    href: "/runtime",
    label: "运行时回合",
    eyebrow: "02",
    description: "一次请求如何经过模型、工具并落到会话。",
  },
  {
    href: "/state-safety",
    label: "状态与安全",
    eyebrow: "03",
    description: "身份键、sidecar、权限以及 Guard / Safe Mode 恢复边界。",
  },
  {
    href: "/desktop",
    label: "桌面端",
    eyebrow: "04",
    description: "Guard 启动、Wails、React 前端与 Go 控制器如何协作。",
  },
  {
    href: "/extensions",
    label: "扩展体系",
    eyebrow: "05",
    description: "Provider、Tool、MCP、Skill、Agent 与插件包。",
  },
  {
    href: "/ecosystem",
    label: "生态与交付",
    eyebrow: "06",
    description: "本地核心、云服务、Worker、安装与发布边界。",
  },
  {
    href: "/develop",
    label: "开发上手",
    eyebrow: "07",
    description: "环境、练习、测试矩阵与首个 PR。",
  },
  {
    href: "/reference",
    label: "索引与术语",
    eyebrow: "REF",
    description: "权威资料、模块索引、术语与继续深挖入口。",
  },
];

export const searchIndex: SearchRecord[] = [
  {
    title: "五层总体架构",
    description: "入口、编排、能力、状态与基础设施之间的边界。",
    href: "/architecture#layers",
    section: "总体架构",
    keywords: "layer cli app service provider tool session config",
  },
  {
    title: "启动与依赖装配",
    description: "cmd/reasonix 如何装配配置、Provider、Tool 与会话。",
    href: "/architecture#bootstrap",
    section: "总体架构",
    keywords: "bootstrap main root command registry dependency",
  },
  {
    title: "改动落点导航",
    description: "从需求类型反查最先阅读的目录与验证命令。",
    href: "/architecture#change-map",
    section: "总体架构",
    keywords: "change feature locate directory maintainer",
  },
  {
    title: "一次用户回合",
    description: "输入、上下文、模型流、工具调用、持久化的完整链路。",
    href: "/runtime#turn",
    section: "运行时回合",
    keywords: "turn stream message model response loop",
  },
  {
    title: "工具调用循环",
    description: "工具参数、权限检查、执行结果与二次推理。",
    href: "/runtime#tools",
    section: "运行时回合",
    keywords: "tool call permission approval execute result",
  },
  {
    title: "提示词缓存",
    description: "缓存键、稳定前缀与命中率的维护注意事项。",
    href: "/runtime#cache",
    section: "运行时回合",
    keywords: "prompt cache provider prefix token",
  },
  {
    title: "计划与进度状态机",
    description: "两级 Todo、唯一 in_progress、complete_step 与自适应进度租约。",
    href: "/runtime#progress",
    section: "运行时回合",
    keywords: "todo plan phase child complete_step progress lease max steps",
  },
  {
    title: "状态身份矩阵",
    description: "tabId、sessionPath、topic identity 应分别用于什么状态。",
    href: "/state-safety#identity",
    section: "状态与安全",
    keywords: "tabId sessionPath topic identity key state",
  },
  {
    title: "会话 sidecar",
    description: "会话族、权威 transcript 与辅助状态如何围绕 sessionPath 协同。",
    href: "/state-safety#sidecars",
    section: "状态与安全",
    keywords: "sidecar todo btw plan session metadata",
  },
  {
    title: "权限与审批",
    description: "工具白名单、审批策略与执行沙箱的层级关系。",
    href: "/state-safety#permissions",
    section: "状态与安全",
    keywords: "permission approval sandbox policy allow deny",
  },
  {
    title: "记忆与压缩",
    description: "短期上下文、会话压缩与长期记忆的不同职责。",
    href: "/state-safety#memory",
    section: "状态与安全",
    keywords: "memory compact context summary persistence",
  },
  {
    title: "Guard 与 Safe Mode",
    description: "独立恢复进程、启动健康、可撤销修复和最小化安全启动。",
    href: "/state-safety#recovery",
    section: "状态与安全",
    keywords: "guard recovery repair safe mode rollback undo startup health",
  },
  {
    title: "Wails 桥接",
    description: "前端调用 Go 控制器、事件回传与生成绑定。",
    href: "/desktop#bridge",
    section: "桌面端",
    keywords: "wails bridge binding event react go controller",
  },
  {
    title: "桌面状态归属",
    description: "组件、控制器、会话与原生能力分别拥有何种状态。",
    href: "/desktop#ownership",
    section: "桌面端",
    keywords: "desktop frontend state ownership controller store",
  },
  {
    title: "桌面端验证矩阵",
    description: "前端检查与原生 Wails 构建是两条独立验证线。",
    href: "/desktop#validation",
    section: "桌面端",
    keywords: "pnpm typecheck css test wails build native windows",
  },
  {
    title: "Provider 扩展",
    description: "新增模型供应商时的接口、配置和注册路径。",
    href: "/extensions#provider",
    section: "扩展体系",
    keywords: "provider model anthropic openai registry config",
  },
  {
    title: "Tool 与 MCP",
    description: "内置工具和外部 MCP Server 的边界与注册方式。",
    href: "/extensions#tools",
    section: "扩展体系",
    keywords: "tool mcp server protocol registry extension",
  },
  {
    title: "Skill 与插件包",
    description: "提示能力、插件 Agent、可分发包与结构化兼容性报告。",
    href: "/extensions#skills",
    section: "扩展体系",
    keywords: "skill plugin package agent claude compatibility install source registry",
  },
  {
    title: "本地与云端边界",
    description: "哪些能力属于本地核心，哪些属于账户、下载和社区服务。",
    href: "/ecosystem#boundary",
    section: "生态与交付",
    keywords: "local cloud account download forum registry worker",
  },
  {
    title: "发布流水线",
    description: "CLI、桌面端、官网与服务分别如何构建交付。",
    href: "/ecosystem#delivery",
    section: "生态与交付",
    keywords: "release ci github actions build deploy desktop cli",
  },
  {
    title: "新人阅读顺序",
    description: "90 分钟建立全貌、半天跟踪回合、一天完成练习。",
    href: "/develop#route",
    section: "开发上手",
    keywords: "onboarding learn reading route new maintainer",
  },
  {
    title: "测试命令矩阵",
    description: "按改动范围选择 Go、前端、Wails 与文档检查。",
    href: "/develop#tests",
    section: "开发上手",
    keywords: "test go pnpm wails lint build matrix",
  },
  {
    title: "首个 PR 清单",
    description: "从小改动、边界验证到变更说明的完整检查表。",
    href: "/develop#first-pr",
    section: "开发上手",
    keywords: "pull request pr checklist contribution",
  },
  {
    title: "权威资料顺序",
    description: "代码、测试、CI、配置和设计文档发生冲突时如何判断。",
    href: "/reference#authority",
    section: "索引与术语",
    keywords: "source truth docs authority code test ci config",
  },
  {
    title: "核心模块索引",
    description: "cmd、app、provider、tools、session、desktop 等目录速查。",
    href: "/reference#modules",
    section: "索引与术语",
    keywords: "module directory package index source",
  },
  {
    title: "术语表",
    description: "Turn、Sidecar、Provider、Skill、MCP 等项目语义。",
    href: "/reference#glossary",
    section: "索引与术语",
    keywords: "glossary term turn sidecar provider mcp skill",
  },
];

export function sourceUrl(path: string, line?: number) {
  const anchor = line ? `#L${line}` : "";
  return `${repositoryUrl}/blob/main-v2/${path}${anchor}`;
}
