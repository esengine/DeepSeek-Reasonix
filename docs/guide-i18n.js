/* Configuration-guide translations + scrollspy. Layered on top of i18n.js. */

(function () {
  "use strict";

  var R = window.Reasonix;
  if (!R) return;

  var en = {
    "nav.guide": "Guide",

    "guide.badge": "Configuration · MCP · Skills · Memory",
    "guide.title.line1": "Configure Reasonix",
    "guide.title.line2": "in five minutes",
    "guide.sub":
      "One JSON file at <code>~/.reasonix/config.json</code> + per-project overrides under <code>.reasonix/</code>. This page documents every key, every slash command, and the on-disk shape of skills, memory, and hooks.",

    "guide.toc.title": "On this page",
    "guide.toc.config": "config.json",
    "guide.toc.mcp": "MCP servers",
    "guide.toc.skills": "Skills",
    "guide.toc.memory": "Memory",
    "guide.toc.hooks": "Hooks",
    "guide.toc.perms": "Permissions",
    "guide.toc.search": "Web search",
    "guide.toc.index": "Semantic index",

    "th.cmd": "Command",
    "th.what": "What it does",

    "cfg.title": "The config.json file",
    "cfg.body1":
      "Reasonix reads a single global config from <code>~/.reasonix/config.json</code> (Windows: <code>%USERPROFILE%\\.reasonix\\config.json</code>). The file is created automatically on first run; you can hand-edit it any time. The CLI flag <code>--no-config</code> bypasses it, useful in CI.",
    "cfg.body2":
      "Per-project overrides live under <code>&lt;project&gt;/.reasonix/</code> — skills, memory, settings.json (hooks). Project scope wins over global on name collision.",
    "cfg.shape": "Top-level keys",
    "cfg.k.lang": "UI language: en | zh",
    "cfg.k.preset": "auto | flash | pro",
    "cfg.k.editmode": "review | auto | yolo",
    "cfg.k.effort": "high | max",
    "cfg.k.theme": "light | dark | auto",
    "cfg.k.search": "enable web_search/web_fetch tools",
    "cfg.k.engine": "mojeek | searxng",
    "cfg.k.mcp": "MCP server specs",
    "cfg.k.mcpoff": "names skipped at startup",
    "cfg.k.projects": "per-workspace overrides",
    "cfg.k.semantic": "embedding provider for `reasonix index`",
    "cfg.callout.tag": "Trust dial",
    "cfg.callout.body":
      "<code>editMode</code> is the single trust dial for an entire session. <code>review</code> queues edits + gates shell. <code>auto</code> applies edits + still gates shell. <code>yolo</code> skips both gates — only use inside a sandbox.",

    "mcp.title": "MCP servers",
    "mcp.body1":
      "Reasonix speaks the Model Context Protocol natively. Every entry in <code>config.mcp</code> is a single string — the same format the <code>--mcp</code> CLI flag accepts — so one parser handles both. Three transports are supported.",
    "mcp.h.stdio": "Stdio (subprocess)",
    "mcp.body.stdio":
      "Format: <code>name=command arg1 arg2</code>. The <code>name=</code> prefix namespaces every tool the server exposes. Args use shell-style splitting; quote any with spaces.",
    "mcp.h.sse": "SSE (HTTP)",
    "mcp.body.sse":
      "Plain <code>http://</code> / <code>https://</code> URLs use HTTP+SSE for back-compat. Anonymous (no <code>name=</code>) entries work but can't be toggled by name later.",
    "mcp.h.streamable": "Streamable HTTP (2025-03 spec)",
    "mcp.body.streamable": "Opt in with the <code>streamable+</code> URL prefix.",
    "mcp.h.cli": "CLI flags &amp; slash commands",
    "mcp.cmd.hub": "Open the interactive MCP hub.",
    "mcp.cmd.disable":
      "Persist to <code>mcpDisabled</code>; effective on next launch.",
    "mcp.cmd.enable": "Re-enable a disabled server.",
    "mcp.cmd.recon": "Reconnect a live server and pick up newly-registered tools.",

    "sk.title": "Skills",
    "sk.body1":
      "A skill is a markdown playbook the model can invoke (<code>/skill &lt;name&gt;</code>). Names + descriptions are pinned in the prompt; bodies load on demand. Project skills override global ones with the same name.",
    "sk.h.layout": "Layout",
    "sk.body.layout":
      "Two equivalent shapes: a flat <code>&lt;name&gt;.md</code>, or a <code>&lt;name&gt;/SKILL.md</code> folder when you want to colocate attachments.",
    "sk.h.fm": "Frontmatter",
    "sk.fm.desc": "Review git log for security red flags.",
    "sk.fm.runas": "inline | subagent",
    "sk.fm.tools": "subagent tool allowlist",
    "sk.fm.model": "subagent model override",
    "sk.body.task": "Task",
    "sk.body.s1": "Fetch the last 20 commits.",
    "sk.body.s2":
      "Flag commits whose message mentions password / secret / token.",
    "sk.body.s3": "Report findings.",
    "sk.fm.f.name":
      "1–64 chars: alnum, <code>_</code>, <code>-</code>, interior <code>.</code>. Defaults to filename stem.",
    "sk.fm.f.desc": "One line. Shown in <code>/skill list</code>.",
    "sk.fm.f.runas":
      "<code>inline</code> (default): body enters parent log. <code>subagent</code>: isolated child loop, only the final answer returns.",
    "sk.fm.f.tools":
      "Comma-separated literal tool names. Subagent only — scopes the child's tool registry.",
    "sk.fm.f.model":
      "Subagent only. Must start with <code>deepseek-</code>; ignored otherwise.",
    "sk.h.cmds": "Slash commands",
    "sk.cmd.list": "List every skill, scope-tagged.",
    "sk.cmd.new":
      "Scaffold a stub at project scope. Add <code>--global</code> for <code>~/.reasonix/skills</code>.",
    "sk.cmd.show": "Print the full body.",
    "sk.cmd.run": "Run it. Args are appended to the body as a single string.",

    "mem.title": "Memory",
    "mem.body1":
      "Memory is user-private knowledge pinned into the immutable prefix — so the agent reads it on every turn without re-priming. Two scopes: <em>global</em> (cross-project facts about you) and <em>project</em> (per-workspace context). Distinct from a committable <code>REASONIX.md</code>, which lives in the repo.",
    "mem.h.layout": "Layout",
    "mem.idx": "index — pinned into the prefix",
    "mem.proj": "sha1(absRoot)[0..16]",
    "mem.h.entry": "Entry shape",
    "mem.f.desc": "User is a senior backend engineer; new to React.",
    "mem.f.type": "user | feedback | project | reference",
    "mem.f.body": "Body — the actual remembered fact, in plain markdown.",
    "mem.body.types":
      "<strong>Types:</strong> <code>user</code> (who they are), <code>feedback</code> (corrections / preferences), <code>project</code> (initiative / deadline / motivation), <code>reference</code> (where to look in external systems).",
    "mem.h.cmds": "Slash commands",
    "mem.cmd.list": "List all entries, both scopes.",
    "mem.cmd.show": "Display body. Scope is auto-resolved.",
    "mem.cmd.forget": "Delete one entry.",
    "mem.cmd.clear":
      "Wipe an entire scope. <code>confirm</code> is mandatory.",
    "mem.body.write":
      "<strong>Writing memories:</strong> say it in chat (\"remember I prefer Vitest over Jest\"). The model invokes the <code>scaffold_memory</code> tool, which proposes a file and waits for your <code>/apply</code>.",

    "hk.title": "Hooks",
    "hk.body1":
      "Hooks are shell commands the harness fires on lifecycle events. Configured in <code>settings.json</code>, not <code>config.json</code>. Project scope first, then global.",
    "hk.h.where": "Where to put them",
    "hk.path.proj": "project scope",
    "hk.path.glob": "global scope",
    "hk.h.shape": "Shape",
    "hk.ex.audit": "Audit risky tool calls before they run",
    "hk.h.events": "Events",
    "hk.ev.pre":
      "Before a tool runs. <strong>Gating:</strong> exit 2 blocks; exit 0 passes. 5 s default timeout.",
    "hk.ev.post":
      "After a tool runs. Non-gating; warn-only on non-zero. 30 s default.",
    "hk.ev.usr":
      "Before user input is processed. <strong>Gating</strong> (exit 2 blocks the message).",
    "hk.ev.stop":
      "On <code>/quit</code> or session exit. Non-gating.",
    "hk.h.payload": "Stdin payload",
    "hk.body.payload":
      "Each hook receives a JSON object on stdin describing the event:",

    "perm.title": "Permissions",
    "perm.body1":
      "Shell commands are gated per-workspace. The first time the agent runs a command, you get an interactive <em>allow once / allow always / deny</em> prompt; \"allow always\" persists the exact prefix to <code>config.json</code> under that project.",
    "perm.body.exact":
      "<strong>Exact match after trim.</strong> <code>git</code> alone does <em>not</em> cover <code>git push origin main</code>; list each prefix you actually want green-lit.",
    "perm.cmd.list": "Show this project's allowlist.",
    "perm.cmd.add": "Add a shell prefix.",
    "perm.cmd.rm": "Remove by name or list index.",
    "perm.cmd.clear":
      "Wipe everything. <code>confirm</code> is mandatory.",

    "ws.title": "Web search",
    "ws.body1":
      "<code>web_search</code> + <code>web_fetch</code> ship in the box. Default backend is <strong>Mojeek</strong> (no setup); switch to a self-hosted <strong>SearXNG</strong> when you want full control over upstream engines.",
    "ws.body.json": "Equivalent <code>config.json</code>:",
    "ws.body.start": "Start a local SearXNG:",

    "ix.title": "Semantic index",
    "ix.body1":
      "<code>reasonix index</code> builds an embedding index the agent can query. Pick an embedding provider:",
    "ix.body.swap":
      "Switch by changing <code>provider</code>. Local Ollama is free and air-gapped; OpenAI-compat lets you point at any hosted embedding API.",

    "cta.title": "Still stuck?",
    "cta.sub":
      "Open a discussion or drop into <code>good first issue</code>. Every avatar on the contributors wall started somewhere.",
    "cta.disc": "Discussions →",
    "cta.arch": "Architecture deep dive",
    "cta.cli": "CLI reference",
  };

  var zh = {
    "nav.guide": "配置指南",

    "guide.badge": "配置 · MCP · Skills · Memory",
    "guide.title.line1": "五分钟",
    "guide.title.line2": "配置完 Reasonix",
    "guide.sub":
      "一个全局 JSON 文件 <code>~/.reasonix/config.json</code>，加上项目级 <code>.reasonix/</code> 下的覆盖。这一页把每个 key、每条斜杠命令、以及 skills / memory / hooks 在磁盘上的形状全部讲清楚。",

    "guide.toc.title": "本页目录",
    "guide.toc.config": "config.json",
    "guide.toc.mcp": "MCP 服务器",
    "guide.toc.skills": "Skills",
    "guide.toc.memory": "Memory",
    "guide.toc.hooks": "Hooks",
    "guide.toc.perms": "权限",
    "guide.toc.search": "Web 搜索",
    "guide.toc.index": "语义索引",

    "th.cmd": "命令",
    "th.what": "作用",

    "cfg.title": "config.json 配置文件",
    "cfg.body1":
      "Reasonix 只从 <code>~/.reasonix/config.json</code> 读取全局配置（Windows：<code>%USERPROFILE%\\.reasonix\\config.json</code>）。首次运行会自动创建，之后随便手改。CLI 加 <code>--no-config</code> 即可跳过，CI 友好。",
    "cfg.body2":
      "项目级覆盖放在 <code>&lt;project&gt;/.reasonix/</code> 下——skills、memory、settings.json（hooks）都遵守这个目录。同名时项目级覆盖全局。",
    "cfg.shape": "顶层 key",
    "cfg.k.lang": "界面语言：en | zh",
    "cfg.k.preset": "auto | flash | pro",
    "cfg.k.editmode": "review | auto | yolo",
    "cfg.k.effort": "high | max",
    "cfg.k.theme": "light | dark | auto",
    "cfg.k.search": "启用 web_search / web_fetch 工具",
    "cfg.k.engine": "mojeek | searxng",
    "cfg.k.mcp": "MCP 服务器列表",
    "cfg.k.mcpoff": "启动时跳过的服务器名",
    "cfg.k.projects": "按工作区的覆盖配置",
    "cfg.k.semantic": "`reasonix index` 用的 embedding 提供方",
    "cfg.callout.tag": "信任挡位",
    "cfg.callout.body":
      "<code>editMode</code> 是整个会话的唯一一档信任旋钮。<code>review</code> 队列改动 + 拦截 shell；<code>auto</code> 直接落盘 + 继续拦截 shell；<code>yolo</code> 两个都不拦——只在沙箱里用。",

    "mcp.title": "MCP 服务器",
    "mcp.body1":
      "Reasonix 原生支持 Model Context Protocol。<code>config.mcp</code> 里每条都是一个字符串——和 <code>--mcp</code> CLI flag 用同一个 parser。支持三种传输。",
    "mcp.h.stdio": "Stdio（子进程）",
    "mcp.body.stdio":
      "格式：<code>name=command arg1 arg2</code>。<code>name=</code> 前缀会给该服务器暴露的所有工具加命名空间。args 走 shell 风格的分词，含空格的部分要加引号。",
    "mcp.h.sse": "SSE（HTTP）",
    "mcp.body.sse":
      "纯 <code>http://</code> / <code>https://</code> URL 走 HTTP+SSE，向后兼容。匿名条目（不带 <code>name=</code>）能用，但之后没法按名字开关。",
    "mcp.h.streamable": "Streamable HTTP（2025-03 规范）",
    "mcp.body.streamable":
      "用 <code>streamable+</code> 前缀显式开启。",
    "mcp.h.cli": "CLI flag 与斜杠命令",
    "mcp.cmd.hub": "打开交互式 MCP 中心。",
    "mcp.cmd.disable":
      "写到 <code>mcpDisabled</code>；下次启动生效。",
    "mcp.cmd.enable": "重新启用一个被禁用的服务器。",
    "mcp.cmd.recon":
      "重连一个在线服务器，并增量注册新工具。",

    "sk.title": "Skills",
    "sk.body1":
      "Skill 是 markdown 写的剧本，模型可以调用（<code>/skill &lt;name&gt;</code>）。名称+描述会钉进 prompt，body 按需加载。同名时项目级覆盖全局。",
    "sk.h.layout": "目录布局",
    "sk.body.layout":
      "两种等价的形式：扁平的 <code>&lt;name&gt;.md</code>，或者 <code>&lt;name&gt;/SKILL.md</code> 文件夹（需要附带资源时用后者）。",
    "sk.h.fm": "Frontmatter",
    "sk.fm.desc": "审查 git log，看看有没有安全相关的危险信号。",
    "sk.fm.runas": "inline | subagent",
    "sk.fm.tools": "subagent 的工具白名单",
    "sk.fm.model": "subagent 的模型覆盖",
    "sk.body.task": "任务",
    "sk.body.s1": "拉最近 20 个 commit。",
    "sk.body.s2":
      "标记 commit message 里出现 password / secret / token 的提交。",
    "sk.body.s3": "汇总结果。",
    "sk.fm.f.name":
      "1–64 字符：alnum、<code>_</code>、<code>-</code>，中间允许 <code>.</code>。默认取文件名 stem。",
    "sk.fm.f.desc":
      "一行。在 <code>/skill list</code> 里展示。",
    "sk.fm.f.runas":
      "<code>inline</code>（默认）：body 进父循环的日志。<code>subagent</code>：起一个隔离子循环，只回传最终结果。",
    "sk.fm.f.tools":
      "逗号分隔的工具字面名。仅 subagent 生效——给子循环的 tool registry 圈定范围。",
    "sk.fm.f.model":
      "仅 subagent 生效。必须以 <code>deepseek-</code> 开头，否则忽略。",
    "sk.h.cmds": "斜杠命令",
    "sk.cmd.list": "按 scope 列出全部 skill。",
    "sk.cmd.new":
      "在项目 scope 下生成 stub。加 <code>--global</code> 可写到 <code>~/.reasonix/skills</code>。",
    "sk.cmd.show": "打印完整 body。",
    "sk.cmd.run":
      "运行它。后续 args 会作为单一字符串拼到 body 之后。",

    "mem.title": "Memory",
    "mem.body1":
      "Memory 是用户私有的知识，钉进不可变前缀——所以 agent 每回合都自动读到，不用再重新 prime。两个 scope：<em>global</em>（关于你的、跨项目事实）与 <em>project</em>（按工作区的上下文）。和提交进仓库的 <code>REASONIX.md</code> 不是一回事。",
    "mem.h.layout": "目录布局",
    "mem.idx": "索引——会被钉进前缀",
    "mem.proj": "sha1(absRoot)[0..16]",
    "mem.h.entry": "条目格式",
    "mem.f.desc": "用户是资深后端，第一次接触 React。",
    "mem.f.type":
      "user | feedback | project | reference",
    "mem.f.body": "正文——真正要记的事实，纯 markdown。",
    "mem.body.types":
      "<strong>四种类型：</strong><code>user</code>（用户是谁）、<code>feedback</code>（修正/偏好）、<code>project</code>（计划/截止/动机）、<code>reference</code>（外部系统该去哪里查）。",
    "mem.h.cmds": "斜杠命令",
    "mem.cmd.list": "列出两个 scope 的全部条目。",
    "mem.cmd.show": "展示 body。scope 自动解析。",
    "mem.cmd.forget": "删一条。",
    "mem.cmd.clear":
      "清空整个 scope。必须显式加 <code>confirm</code>。",
    "mem.body.write":
      "<strong>怎么写入：</strong>直接在对话里说（“记一下我用 Vitest，不用 Jest”）。模型会调用 <code>scaffold_memory</code> 工具起草一条文件，等你 <code>/apply</code> 落盘。",

    "hk.title": "Hooks",
    "hk.body1":
      "Hooks 是 harness 在生命周期事件上触发的 shell 命令。配置写在 <code>settings.json</code>，不是 <code>config.json</code>。先项目级，再全局。",
    "hk.h.where": "放在哪里",
    "hk.path.proj": "项目 scope",
    "hk.path.glob": "全局 scope",
    "hk.h.shape": "Schema",
    "hk.ex.audit":
      "在风险工具调用执行前先审计一下",
    "hk.h.events": "事件",
    "hk.ev.pre":
      "工具执行前。<strong>会拦截：</strong>exit 2 阻断；exit 0 通过。默认 5 秒超时。",
    "hk.ev.post":
      "工具执行后。不拦截；非零仅 warn。默认 30 秒。",
    "hk.ev.usr":
      "用户输入处理前。<strong>会拦截</strong>（exit 2 把这条消息阻断）。",
    "hk.ev.stop":
      "<code>/quit</code> 或会话退出时触发。不拦截。",
    "hk.h.payload": "Stdin 负载",
    "hk.body.payload":
      "每个 hook 在 stdin 上收到一段 JSON，描述事件：",

    "perm.title": "权限",
    "perm.body1":
      "Shell 命令按工作区拦截。Agent 第一次跑某条命令时弹交互式 <em>本次允许 / 永久允许 / 拒绝</em>；选“永久允许”就把这条精确前缀写进 <code>config.json</code> 的对应项目下。",
    "perm.body.exact":
      "<strong>trim 后做精确匹配。</strong>光列 <code>git</code> 并<em>不</em>覆盖 <code>git push origin main</code>——你想放行的每个前缀都得单独列。",
    "perm.cmd.list": "查看本项目的白名单。",
    "perm.cmd.add": "新增一条 shell 前缀。",
    "perm.cmd.rm": "按名字或下标删除。",
    "perm.cmd.clear":
      "全清。必须显式加 <code>confirm</code>。",

    "ws.title": "Web 搜索",
    "ws.body1":
      "<code>web_search</code> + <code>web_fetch</code> 开箱即用。默认走 <strong>Mojeek</strong>（无需配置）；想完全控制上游引擎就切到自托管 <strong>SearXNG</strong>。",
    "ws.body.json": "等价的 <code>config.json</code>：",
    "ws.body.start": "本机起一个 SearXNG：",

    "ix.title": "语义索引",
    "ix.body1":
      "<code>reasonix index</code> 会构建一份 embedding 索引供 agent 查询。挑一个 embedding 提供方：",
    "ix.body.swap":
      "切换只需要改 <code>provider</code>。本地 Ollama 免费且离线；OpenAI-compat 可以指向任何兼容的 embedding API。",

    "cta.title": "还有疑问？",
    "cta.sub":
      "去开个 discussion，或者从 <code>good first issue</code> 入坑——贡献者墙上每一张头像都是这么开始的。",
    "cta.disc": "讨论区 →",
    "cta.arch": "架构深度文档",
    "cta.cli": "CLI 参考",
  };

  var DICT = { en: en, zh: zh };

  function applyGuide(lang) {
    var dict = DICT[lang] || DICT.en;
    document.querySelectorAll("[data-i18n]").forEach(function (el) {
      var key = el.getAttribute("data-i18n");
      if (dict[key] !== undefined) el.innerHTML = dict[key];
    });
  }

  // Re-apply on first load and every language change.
  applyGuide(R.lang());
  R.onLangChange(applyGuide);

  // Scrollspy — highlight the current section's TOC entry.
  var sections = Array.prototype.slice.call(
    document.querySelectorAll(".guide-body section[id]"),
  );
  var tocLinks = Array.prototype.slice.call(
    document.querySelectorAll(".guide-toc a"),
  );
  if (sections.length && tocLinks.length && "IntersectionObserver" in window) {
    var byId = {};
    tocLinks.forEach(function (a) {
      var href = a.getAttribute("href") || "";
      var id = href.replace(/^#/, "");
      if (id) byId[id] = a;
    });
    var io = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (e) {
          var link = byId[e.target.id];
          if (!link) return;
          if (e.isIntersecting) {
            tocLinks.forEach(function (l) {
              l.classList.remove("is-active");
            });
            link.classList.add("is-active");
          }
        });
      },
      { rootMargin: "-30% 0px -60% 0px", threshold: 0 },
    );
    sections.forEach(function (s) {
      io.observe(s);
    });
  }
})();
