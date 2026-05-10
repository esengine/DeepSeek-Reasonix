// Reasonix landing — i18n auto-switch (en / zh).
// Detection precedence: ?lang=xx → localStorage → navigator.language → "en".
// Falls back gracefully when localStorage is unavailable (private mode, etc).

(function () {
  "use strict";

  const STORAGE_KEY = "reasonix.lang";
  const DEFAULT_LANG = "en";
  const SUPPORTED = ["en", "zh"];

  const translations = {
    en: {
      "nav.why": "Why",
      "nav.features": "Features",
      "nav.quickstart": "Quick start",
      "nav.guide": "Guide",
      "nav.community": "Community",
      "nav.github": "GitHub",

      "hero.status": "live · v{version}",
      "hero.badge": "DeepSeek · cache-first · MIT",
      "hero.title.line1": "DeepSeek-native",
      "hero.title.line2": "AI coding agent in your terminal",
      "hero.sub":
        "Engineered around DeepSeek's prefix-cache so token costs stay low across long sessions. Custom cell-diff renderer. MCP first-class. Open source.",
      "hero.copy": "Copy",
      "hero.copy.done": "Copied",
      "hero.cta.start": "Get started →",
      "hero.cta.star": "Star on GitHub",

      "metric.hit": "Cache hit, single day",
      "metric.tokens": "Input tokens served",
      "metric.cost": "Cost vs. no-cache",
      "metric.lic": "Open, community-built",
      "metric.src": "Source: real-world cache case study (2026-05-01) →",

      "term.user":
        "users.ts findByEmail is case-sensitive — login fails for users with uppercase emails",
      "term.found":
        "▸ Found it. findByEmail uses === directly. Switch to lowercase normalization.",
      "term.pending": "▸ 1 pending edit · /apply to write · /discard to drop",

      "why.title": "Why Reasonix",
      "why.sub":
        "The loop is organized around four architectural pillars. Each one solves a problem generic agent frameworks don't even see — because they were designed for a different cache mechanic.",
      "why.cache.title": "Cache-first loop",
      "why.cache.body":
        "Append-only history. No in-place mutation, no marker-based compaction. The byte prefix survives every tool call — DeepSeek's prefix-cache keeps hitting turn after turn.",
      "why.r1.title": "R1 thought harvesting",
      "why.r1.body":
        "Distills <code>reasoning_content</code> into a typed plan state — subgoals, hypotheses, uncertainties, rejected paths. Signal kept, noise dropped.",
      "why.repair.title": "Tool-call repair",
      "why.repair.body":
        "Schema flatten · JSON repair · scavenge from <code>&lt;think&gt;</code> · truncation. Four strategies that handle DeepSeek-specific quirks generic loops mistake for model errors.",
      "why.cost.title": "Cost control",
      "why.cost.body":
        "Cache-safe folding · aggressive-fold tier · summary-on-exit · model-aware budgets. The loop manages context size without breaking prefix stability.",
      "why.deepseek.title": "DeepSeek-only by design",
      "why.deepseek.body":
        "Every layer is tuned around DeepSeek's specific cache mechanic and pricing. Coupling to one backend is the feature, not a limitation.",
      "why.oss.title": "Open community",
      "why.oss.body":
        "MIT licensed and community-developed. Scoped <code>good first issue</code> tickets with code pointers and acceptance criteria. Real PRs from real contributors.",

      "qs.title": "Quick start (60 seconds)",
      "qs.step1.title": "Get a DeepSeek API key",
      "qs.step1.body":
        'Sign up at <a href="https://platform.deepseek.com/api_keys" target="_blank" rel="noopener">platform.deepseek.com</a> and create a key.',
      "qs.step2.title": "Point it at a project",
      "qs.step2.body": "No install needed.",
      "qs.step2.note":
        "First run walks you through a short wizard — paste API key, pick a preset, optionally attach MCP servers.",
      "qs.step3.title": "Review and apply",
      "qs.step3.body":
        "The agent proposes edits as reviewable blocks — nothing hits disk until you <code>/apply</code>. Plan mode lets you stage multi-file changes before committing any.",
      "qs.req":
        "Requires Node ≥ 22. macOS, Linux, Windows (PowerShell · Git Bash · Windows Terminal). Press <kbd>Esc</kbd> anytime to abort; <code>/help</code> for the full slash-command list.",

      "feat.title": "In the box",
      "feat.sub":
        "Twelve concrete capabilities. The loop is the foundation; everything below is what you get on top of it.",
      "feat.renderer.title": "Cell-diff renderer",
      "feat.renderer.body":
        "Custom TUI runtime built on Yoga. No Ink dependency. Wide-char, emoji, bracketed paste, and resize handled cleanly across platforms.",
      "feat.mcp.title": "MCP first-class",
      "feat.mcp.body":
        'Stdio and Streamable HTTP transports. Tools, resources, and prompts. In-app browser to inspect any server\'s surface, plus <code>--mcp "name=cmd"</code> on the fly.',
      "feat.plan.title": "Plan mode",
      "feat.plan.body":
        "Review proposed edits before they touch disk. Approve, refine, or reject. Plan checkpoints persist across runs so you can resume mid-review.",
      "feat.perm.title": "Permissions",
      "feat.perm.body":
        "<code>allow</code> · <code>ask</code> · <code>deny</code> per-tool. Granular shell command rules. Interactive prompts you can teach.",
      "feat.dash.title": "Embedded dashboard",
      "feat.dash.body":
        "Companion web view at <code>localhost</code>. Live cache hit rate, cost ticker, session timeline, MCP health — all in one place.",
      "feat.sess.title": "Persistent sessions",
      "feat.sess.body":
        "Per-workspace, named, resumable. <code>--resume</code> picks up exactly where you left off — system prompt, history, plan state.",
      "feat.hooks.title": "Hooks · skills · memory",
      "feat.hooks.body":
        "Shell commands fire on lifecycle events. Drop-in skill packs spawn sub-agents. Project memory the agent reads on every turn.",
      "feat.search.title": "Semantic search",
      "feat.search.body":
        "<code>reasonix index</code> builds an embedding index your agent can query. Local Ollama or DeepSeek-hosted embeddings.",
      "feat.ckpt.title": "Auto-checkpoints",
      "feat.ckpt.body":
        "Cursor-style session-scoped rollback for AI edits. Never pollutes git history; the checkpoint stack is yours alone.",
      "feat.effort.title": "<code>/effort</code> knob",
      "feat.effort.body":
        "Switch reasoning depth per turn. <code>max</code> for the gnarly, <code>low</code> for routine. Slash command and CLI flag.",
      "feat.replay.title": "Transcript replay",
      "feat.replay.body":
        "<code>reasonix replay</code> plays a recorded session back through the renderer — useful for bug reports, demos, and audits.",
      "feat.events.title": "Event log",
      "feat.events.body":
        "<code>events.jsonl</code> sidecar with reducers and a <code>reasonix events</code> CLI. Build dashboards, audits, or your own analytics.",

      "conf.title": "Configure in five minutes",
      "conf.sub":
        "One JSON file at <code>~/.reasonix/config.json</code>, plus per-project overrides under <code>.reasonix/</code>. Point. Click. Wire in your stack.",
      "conf.read": "Read →",
      "conf.mcp.title": "MCP servers",
      "conf.mcp.body":
        "stdio · SSE · Streamable HTTP. One spec format for both <code>config.json</code> and <code>--mcp</code>.",
      "conf.sk.title": "Skills",
      "conf.sk.body":
        "Markdown playbooks the model invokes. Inline or sub-agent. Project overrides global.",
      "conf.mem.title": "Memory",
      "conf.mem.body":
        "User-private knowledge pinned into the prefix. Global + project scopes. Four typed shapes.",
      "conf.hk.title": "Hooks",
      "conf.hk.body":
        "Shell commands on lifecycle events. Pre/post tool, prompt submit, stop. Exit-2 to block.",
      "conf.perm.title": "Permissions",
      "conf.perm.body":
        "Per-workspace shell allowlist. Exact-prefix match. Interactive \"always allow\" persists.",
      "conf.ws.title": "Web search",
      "conf.ws.body":
        "Mojeek by default, no setup. Switch to self-hosted SearXNG with <code>/search-engine</code>.",
      "conf.cta": "Open the configuration guide →",

      "cli.title": "CLI at a glance",
      "cli.code": "coding mode scoped to path",
      "cli.chat": "interactive chat (saved config)",
      "cli.run": "one-shot, streams to stdout",
      "cli.doctor": "environment health check",
      "cli.replay": "re-render a recorded session",
      "cli.diff": "compare two transcripts",
      "cli.events": "query the event log",
      "cli.stats": "cross-session usage",
      "cli.index": "build semantic embedding index",
      "cli.mcp": "probe one MCP server",
      "cli.mcplist": "list configured MCP servers",
      "cli.prune": "clean up old sessions",
      "cli.flags.intro": "Common flags:",
      "cli.f.effort": "reasoning depth for the run",
      "cli.f.model": "explicit DeepSeek model id",
      "cli.f.mcp": "attach an MCP server (repeatable)",
      "cli.f.session": "named session",
      "cli.f.resume": "pick up the latest session for this workspace",
      "cli.f.new": "force a fresh session, preserve old",
      "cli.f.noconf": "ignore ~/.reasonix/config.json (CI)",

      "comm.title": "Built by the community",
      "comm.sub":
        "Reasonix is open source and community-developed. Every avatar on the wall below is a real PR that shipped — not a sponsorship slot.",
      "comm.gfi": "good first issue →",
      "comm.disc": "Discussions",
      "comm.contrib": "Contributing guide",

      "ctab.title": "Ready to try?",
      "ctab.sub": "One <code>npx</code> away. Sandboxed. Reviewable. Open source.",
      "ctab.gh": "GitHub repository →",
      "ctab.npm": "npm package",

      "foot.tag": "DeepSeek does deep, deeply.",
      "foot.col.project": "Project",
      "foot.col.docs": "Docs",
      "foot.col.community": "Community",
      "foot.releases": "Releases",
      "foot.readme": "README",
      "foot.readme.zh": "中文 README",
      "foot.arch": "Architecture",
      "foot.bench": "Benchmarks",
      "foot.issues": "Issues",
      "foot.discuss": "Discussions",
      "foot.contributors": "Contributors",
      "foot.copyright": "© 2026 Reasonix · MIT License",
    },

    zh: {
      "nav.why": "为什么",
      "nav.features": "特性",
      "nav.quickstart": "快速上手",
      "nav.guide": "配置指南",
      "nav.community": "社区",
      "nav.github": "GitHub",

      "hero.status": "运行中 · v{version}",
      "hero.badge": "DeepSeek · 缓存优先 · MIT",
      "hero.title.line1": "DeepSeek 原生",
      "hero.title.line2": "终端里的 AI 编程代理",
      "hero.sub":
        "围绕 DeepSeek 前缀缓存设计，长会话下 token 成本始终维持在低位。自研 cell-diff 渲染器，MCP 一等公民，完全开源。",
      "hero.copy": "复制",
      "hero.copy.done": "已复制",
      "hero.cta.start": "开始使用 →",
      "hero.cta.star": "在 GitHub 加星",

      "metric.hit": "单日缓存命中率",
      "metric.tokens": "单日输入 token",
      "metric.cost": "对比无缓存成本",
      "metric.lic": "开源 · 社区共建",
      "metric.src": "数据来源：2026-05-01 真实用户缓存命中案例 →",

      "term.user":
        "users.ts 里 findByEmail 对大小写敏感导致登录失败，帮我改",
      "term.found":
        "▸ 找到了。findByEmail 直接用 === 比对。改成小写规范化并补一条测试。",
      "term.pending": "▸ 1 处待应用编辑 · /apply 写入 · /discard 丢弃",

      "why.title": "为什么选 Reasonix",
      "why.sub":
        "整个循环围绕四根架构支柱组织。每一根解决的都是通用 agent 框架根本看不见的问题——因为它们是为另一种缓存机制设计的。",
      "why.cache.title": "缓存优先循环",
      "why.cache.body":
        "只追加历史。不就地修改，不依赖标记的 compaction。字节前缀跨过每一次工具调用都活着——DeepSeek 的前缀缓存一轮一轮持续命中。",
      "why.r1.title": "R1 思维提取",
      "why.r1.body":
        "把 <code>reasoning_content</code> 蒸馏成结构化 plan state——子目标、假设、不确定性、被否决的路径。留信号，去噪声。",
      "why.repair.title": "工具调用修复",
      "why.repair.body":
        "Schema 扁平化 · JSON 修复 · <code>&lt;think&gt;</code> 内 scavenge · 截断处理。四种策略对付 DeepSeek 专属怪癖——通用循环会把这些当模型错误。",
      "why.cost.title": "成本控制",
      "why.cost.body":
        "缓存安全 fold · 激进 fold 层 · 退出时摘要 · 模型感知预算。循环管理上下文规模时不破坏前缀稳定。",
      "why.deepseek.title": "故意只做 DeepSeek",
      "why.deepseek.body":
        "每一层都为 DeepSeek 特定的缓存机制和定价调过。绑死一个后端是 feature，不是限制。",
      "why.oss.title": "开放社区",
      "why.oss.body":
        "MIT 协议，社区共建。<code>good first issue</code> 入门 issue 都带代码定位和验收标准。真实贡献者的真实 PR。",

      "qs.title": "60 秒快速上手",
      "qs.step1.title": "获取 DeepSeek API Key",
      "qs.step1.body":
        '到 <a href="https://platform.deepseek.com/api_keys" target="_blank" rel="noopener">platform.deepseek.com</a> 注册并创建 Key。',
      "qs.step2.title": "切到项目目录运行",
      "qs.step2.body": "无需安装。",
      "qs.step2.note":
        "首次运行会走一个短向导：粘贴 API key、选预设、可选挂载 MCP 服务器。",
      "qs.step3.title": "审阅再应用",
      "qs.step3.body":
        "代理会把改动以可审阅的块呈现——你不 <code>/apply</code>，磁盘不会被改。Plan 模式可以让你先把多文件改动整理好，再统一落盘。",
      "qs.req":
        "需要 Node ≥ 22。支持 macOS、Linux、Windows（PowerShell · Git Bash · Windows Terminal）。任何时候按 <kbd>Esc</kbd> 中断；<code>/help</code> 查看完整斜杠命令。",

      "feat.title": "开箱即用",
      "feat.sub":
        "12 个具体能力。循环是地基，下面是地基之上你能直接拿来用的东西。",
      "feat.renderer.title": "自研 cell-diff 渲染器",
      "feat.renderer.body":
        "基于 Yoga 的 TUI 运行时，不依赖 Ink。宽字符、emoji、bracketed paste、resize 跨平台都干净。",
      "feat.mcp.title": "MCP 一等公民",
      "feat.mcp.body":
        'stdio 与 Streamable HTTP 双传输。工具、资源、提示词全套。内置浏览器查看任意服务器的接口，也能用 <code>--mcp "name=cmd"</code> 现挂。',
      "feat.plan.title": "计划模式",
      "feat.plan.body":
        "改动落盘前先 review。批准、调整、拒绝。Plan checkpoint 跨运行持久化，中途中断也能续。",
      "feat.perm.title": "权限系统",
      "feat.perm.body":
        "每个工具 <code>allow</code> · <code>ask</code> · <code>deny</code>。shell 命令粒度规则。交互式提示，可以教它。",
      "feat.dash.title": "内嵌仪表盘",
      "feat.dash.body":
        "<code>localhost</code> 的伴生 web 面板。实时缓存命中、成本计数、会话时间线、MCP 健康，一处看全。",
      "feat.sess.title": "持久化会话",
      "feat.sess.body":
        "按工作区组织，命名、可恢复。<code>--resume</code> 完全还原——系统提示、历史、plan state。",
      "feat.hooks.title": "Hooks · Skills · Memory",
      "feat.hooks.body":
        "生命周期事件触发 shell 命令。drop-in 的 skill 包能拉子代理。每回合自动读入的项目级 memory。",
      "feat.search.title": "语义检索",
      "feat.search.body":
        "<code>reasonix index</code> 构建 embedding 索引供 agent 查询。本地 Ollama 或 DeepSeek 托管 embedding 任选。",
      "feat.ckpt.title": "自动 checkpoint",
      "feat.ckpt.body":
        "Cursor 风格的会话级 AI 编辑回滚。不污染 git 历史；checkpoint 栈完全是你自己的。",
      "feat.effort.title": "<code>/effort</code> 旋钮",
      "feat.effort.body":
        "每回合切换 reasoning 深度。难活 <code>max</code>、日常 <code>low</code>。斜杠命令 + CLI flag 双入口。",
      "feat.replay.title": "Transcript 重放",
      "feat.replay.body":
        "<code>reasonix replay</code> 把录制好的会话用渲染器重放一遍——bug 复现、演示、审计都好用。",
      "feat.events.title": "事件日志",
      "feat.events.body":
        "<code>events.jsonl</code> 旁路日志，附带 reducer 和 <code>reasonix events</code> CLI。自己搭仪表盘、审计、分析都行。",

      "conf.title": "五分钟配置完",
      "conf.sub":
        "一个全局 JSON <code>~/.reasonix/config.json</code>，加上项目级 <code>.reasonix/</code> 下的覆盖。点几下，把你的工具链接进来。",
      "conf.read": "阅读 →",
      "conf.mcp.title": "MCP 服务器",
      "conf.mcp.body":
        "stdio · SSE · Streamable HTTP。<code>config.json</code> 和 <code>--mcp</code> 共用同一种 spec 格式。",
      "conf.sk.title": "Skills",
      "conf.sk.body":
        "模型可调用的 markdown 剧本。Inline 或 subagent。同名时项目级覆盖全局。",
      "conf.mem.title": "Memory",
      "conf.mem.body":
        "用户私有的知识，钉进前缀。全局 + 项目两个 scope，四种结构化类型。",
      "conf.hk.title": "Hooks",
      "conf.hk.body":
        "生命周期事件触发的 shell 命令。pre/post 工具、prompt 提交、退出。exit 2 即拦截。",
      "conf.perm.title": "权限",
      "conf.perm.body":
        "按工作区的 shell 白名单。精确前缀匹配。交互式“永久允许”会持久化。",
      "conf.ws.title": "Web 搜索",
      "conf.ws.body":
        "默认 Mojeek，零配置。要完全控制就用 <code>/search-engine</code> 切自托管 SearXNG。",
      "conf.cta": "打开配置指南 →",

      "cli.title": "CLI 速览",
      "cli.code": "针对指定路径的编程模式",
      "cli.chat": "交互式聊天（读取已保存配置）",
      "cli.run": "一次性运行，结果流到 stdout",
      "cli.doctor": "环境健康检查",
      "cli.replay": "重渲染一段录制的会话",
      "cli.diff": "比较两段 transcript",
      "cli.events": "查询事件日志",
      "cli.stats": "跨会话用量统计",
      "cli.index": "构建语义 embedding 索引",
      "cli.mcp": "探测单个 MCP 服务器",
      "cli.mcplist": "列出已配置的 MCP 服务器",
      "cli.prune": "清理旧会话",
      "cli.flags.intro": "常用 flag：",
      "cli.f.effort": "本次运行的 reasoning 深度",
      "cli.f.model": "显式指定 DeepSeek 模型 ID",
      "cli.f.mcp": "挂载 MCP 服务器（可重复）",
      "cli.f.session": "命名会话",
      "cli.f.resume": "恢复本工作区的最近一个会话",
      "cli.f.new": "强制开新会话，旧会话保留",
      "cli.f.noconf": "忽略 ~/.reasonix/config.json（CI 友好）",

      "comm.title": "由社区共建",
      "comm.sub":
        "Reasonix 是开源、社区共建的项目。下面墙上每一个头像都对应一次真实合并的 PR——不是赞助位。",
      "comm.gfi": "good first issue →",
      "comm.disc": "Discussions",
      "comm.contrib": "贡献指南",

      "ctab.title": "准备好了吗？",
      "ctab.sub": "一条 <code>npx</code> 即可开始。沙箱、可审阅、完全开源。",
      "ctab.gh": "GitHub 仓库 →",
      "ctab.npm": "npm 包",

      "foot.tag": "Reasonix 只做 DeepSeek，做到底。",
      "foot.col.project": "项目",
      "foot.col.docs": "文档",
      "foot.col.community": "社区",
      "foot.releases": "Releases",
      "foot.readme": "英文 README",
      "foot.readme.zh": "中文 README",
      "foot.arch": "架构文档",
      "foot.bench": "Benchmarks",
      "foot.issues": "问题反馈",
      "foot.discuss": "讨论区",
      "foot.contributors": "贡献者",
      "foot.copyright": "© 2026 Reasonix · MIT 协议",
    },
  };

  function safeStorageGet(key) {
    try {
      return window.localStorage.getItem(key);
    } catch (_) {
      return null;
    }
  }

  function safeStorageSet(key, value) {
    try {
      window.localStorage.setItem(key, value);
    } catch (_) {
      /* ignore */
    }
  }

  function detectLang() {
    const params = new URLSearchParams(window.location.search);
    const queryLang = params.get("lang");
    if (queryLang && SUPPORTED.includes(queryLang)) return queryLang;

    const stored = safeStorageGet(STORAGE_KEY);
    if (stored && SUPPORTED.includes(stored)) return stored;

    const navLang = (navigator.language || navigator.userLanguage || "").toLowerCase();
    if (navLang.startsWith("zh")) return "zh";

    if (Array.isArray(navigator.languages)) {
      for (const l of navigator.languages) {
        if (l && l.toLowerCase().startsWith("zh")) return "zh";
      }
    }

    return DEFAULT_LANG;
  }

  let currentLang = DEFAULT_LANG;
  const langListeners = [];

  // Version is rendered into translation strings via a `{version}` token
  // (see hero.badge). Source of truth is npm — `loadVersion()` fetches
  // it on page load and re-applies translations. Until that resolves
  // we fall back to the most recently cached value, then to a baked-in
  // default. Only places this constant matters: the user is offline AND
  // visits the site for the first time. Bumping it occasionally on
  // major version cuts is fine; the npm fetch handles everything else.
  const VERSION_STORAGE_KEY = "reasonix.version";
  const VERSION_FALLBACK = "0.26";
  const versionListeners = [];
  let currentVersion = VERSION_FALLBACK;

  function applyVersion(v) {
    if (typeof v !== "string" || !v || v === currentVersion) return;
    currentVersion = v;
    safeStorageSet(VERSION_STORAGE_KEY, v);
    applyLang(currentLang); // re-render any `{version}` tokens
    for (const fn of versionListeners) {
      try {
        fn(v);
      } catch (_) {
        /* ignore */
      }
    }
  }

  async function loadVersion() {
    try {
      const r = await fetch("https://registry.npmjs.org/reasonix/latest", {
        cache: "no-cache",
      });
      if (!r.ok) return;
      const data = await r.json();
      if (data && typeof data.version === "string") applyVersion(data.version);
    } catch (_) {
      /* offline / firewall — keep cached or fallback */
    }
  }

  function fillVersion(s) {
    return typeof s === "string" ? s.replace(/\{version\}/g, currentVersion) : s;
  }

  function applyLang(lang) {
    if (!SUPPORTED.includes(lang)) lang = DEFAULT_LANG;
    const changed = lang !== currentLang;
    currentLang = lang;
    const dict = translations[lang];

    document.documentElement.lang = lang === "zh" ? "zh-CN" : "en";
    document.documentElement.setAttribute("data-lang", lang);

    document.querySelectorAll("[data-i18n]").forEach((el) => {
      const key = el.getAttribute("data-i18n");
      if (dict[key] !== undefined) {
        el.innerHTML = fillVersion(dict[key]);
      }
    });

    document.querySelectorAll("[data-lang-btn]").forEach((btn) => {
      const isActive = btn.getAttribute("data-lang-btn") === lang;
      btn.setAttribute("aria-pressed", isActive ? "true" : "false");
    });

    safeStorageSet(STORAGE_KEY, lang);

    if (changed) {
      for (const fn of langListeners) {
        try {
          fn(lang);
        } catch (_) {
          /* ignore */
        }
      }
    }
  }

  // Public API for sibling scripts (term-anim.js).
  window.Reasonix = window.Reasonix || {};
  window.Reasonix.t = function (key) {
    const dict = translations[currentLang] || translations[DEFAULT_LANG];
    return dict[key];
  };
  window.Reasonix.lang = function () {
    return currentLang;
  };
  window.Reasonix.onLangChange = function (fn) {
    if (typeof fn === "function") langListeners.push(fn);
  };
  window.Reasonix.version = function () {
    return currentVersion;
  };
  window.Reasonix.onVersionChange = function (fn) {
    if (typeof fn === "function") versionListeners.push(fn);
  };

  function wireLangButtons() {
    document.querySelectorAll("[data-lang-btn]").forEach((btn) => {
      btn.addEventListener("click", () => {
        applyLang(btn.getAttribute("data-lang-btn"));
      });
    });
  }

  function wireCopyButtons() {
    document.querySelectorAll("[data-copy]").forEach((btn) => {
      btn.addEventListener("click", async () => {
        const text = btn.getAttribute("data-copy");
        try {
          await navigator.clipboard.writeText(text);
        } catch (_) {
          const ta = document.createElement("textarea");
          ta.value = text;
          ta.style.position = "fixed";
          ta.style.opacity = "0";
          document.body.appendChild(ta);
          ta.select();
          try {
            document.execCommand("copy");
          } catch (_) {
            /* ignore */
          }
          document.body.removeChild(ta);
        }
        const lang = document.documentElement.getAttribute("data-lang") || DEFAULT_LANG;
        const original = translations[lang]["hero.copy"] || "Copy";
        const done = translations[lang]["hero.copy.done"] || "Copied";
        btn.textContent = done;
        btn.classList.add("copied");
        setTimeout(() => {
          btn.textContent = original;
          btn.classList.remove("copied");
        }, 1600);
      });
    });
  }

  function init() {
    // Use the cached npm version (if any) so the badge isn't visibly
    // wrong on first paint; fall back to the baked-in default. Then
    // fire off the live fetch — when it resolves, applyVersion()
    // re-applies translations and notifies subscribers (term-anim).
    const cached = safeStorageGet(VERSION_STORAGE_KEY);
    if (typeof cached === "string" && /^\d+\.\d+/.test(cached)) currentVersion = cached;
    applyLang(detectLang());
    wireLangButtons();
    wireCopyButtons();
    loadVersion();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
