// Configuration explorer — MCP, Skills, Memory, Slash commands
// Tabbed interface with code panels showing real Reasonix config

const CONFIG_TABS = [
  {
    id: 'mcp',
    label: 'MCP',
    title: 'Model Context Protocol',
    cn: { zh: '外部工具服务器', en: 'External tool servers' },
    desc: {
      zh: 'MCP 是 Reasonix 接入外部能力的一等公民通道，支持 stdio / SSE / Streamable HTTP 三种传输。每个 server 的工具会以前缀合并进统一的工具 registry，对模型透明。',
      en: 'MCP is the first-class channel for plugging external capabilities into Reasonix — supports stdio, SSE, and Streamable HTTP transports. Each server\'s tools merge into the unified registry under a prefix, transparent to the model.',
    },
    bullets: [
      { zh: '一行命令挂载: --mcp \'name=cmd args\'', en: 'One-line mount: --mcp \'name=cmd args\'' },
      { zh: '所有 MCP 工具沙箱权限与原生工具一致', en: 'MCP tools share the same sandbox as built-ins' },
      { zh: '/mcp 子命令查看已挂载服务器 · 健康状态 · 工具清单', en: '`/mcp` lists mounted servers · health · tools' },
      { zh: '失败重连 · 自动 reconnect with backoff', en: 'Auto-reconnect on failure with exponential backoff' },
    ],
    files: [
      {
        name: '~/.reasonix/config.json',
        lang: 'json',
        code: `{
  "model": "deepseek-v4-flash",
  "mcpServers": {
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": { "GITHUB_TOKEN": "ghp_***" }
    },
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
    },
    "postgres": {
      "transport": "sse",
      "url": "https://mcp.internal/pg/sse"
    }
  }
}`,
      },
      {
        name: 'or via CLI flag',
        lang: 'bash',
        code: `$ reasonix code \\
    --mcp 'github=npx -y @modelcontextprotocol/server-github' \\
    --mcp 'pg=https://mcp.internal/pg/sse'`,
      },
    ],
  },
  {
    id: 'skills',
    label: 'Skills',
    title: 'Skills',
    cn: { zh: '可复用的 Markdown 剧本', en: 'Reusable Markdown playbooks' },
    desc: {
      zh: 'Skill 是一段带 frontmatter 的 Markdown，把"做某件事的方式"凝固成可调用单元。runAs: subagent 时会在隔离子 agent 里运行，allowed-tools 限制可用工具集。',
      en: 'A skill is a Markdown file with frontmatter that crystallises "how to do X" into something callable. `runAs: subagent` runs it inside an isolated sub-agent; `allowed-tools` restricts which tools it can call.',
    },
    bullets: [
      { zh: '项目级: <project>/.reasonix/skills/<name>.md', en: 'Project: <project>/.reasonix/skills/<name>.md' },
      { zh: '全局: ~/.reasonix/skills/<name>.md', en: 'Global: ~/.reasonix/skills/<name>.md' },
      { zh: '/skill new <name> 生成脚手架', en: '/skill new <name> scaffolds a template' },
      { zh: 'runAs: subagent 让 skill 跑在隔离的子循环里', en: '`runAs: subagent` runs the body in an isolated sub-loop' },
    ],
    files: [
      {
        name: '.reasonix/skills/review-pr.md',
        lang: 'md',
        code: `---
description: Review the current branch diff against main
runAs: subagent
allowed-tools: [run_command, read_file, grep_files]
---
You are a strict code reviewer. Steps:

1. Run \`git diff main..HEAD\` to get the change.
2. For each modified file, read_file to load context.
3. Output a structured review:
   - blockers (must fix)
   - suggestions (improvements)
   - nits (style)
4. Do not write files. Do not run tests.

Only focus on the code touched by this diff — don't go off-topic.`,
      },
      {
        name: 'invoke',
        lang: 'bash',
        code: `# Inside the TUI
› /skill run review-pr

# Or just ask — the model can trigger it on its own
› please review the current branch`,
      },
    ],
  },
  {
    id: 'memory',
    label: 'Memory',
    title: 'Memory',
    cn: { zh: '项目级与全局记忆', en: 'Project + global memory' },
    desc: {
      zh: 'Reasonix 把"应当记住"的内容拆成两层：仓库级的 reasonix.md（提交进 git，团队共享）与用户级的 ~/.reasonix/memory.md（个人偏好，不入库）。每次会话启动时自动注入到 prompt 头部。',
      en: 'Reasonix splits "what to remember" into two layers: repo-level `reasonix.md` (checked into git, shared with the team) and user-level `~/.reasonix/memory.md` (personal preferences, kept private). Both are injected at the top of the prompt on every session.',
    },
    bullets: [
      { zh: '<project>/reasonix.md · 项目约定 · git-tracked', en: '<project>/reasonix.md · project conventions · git-tracked' },
      { zh: '~/.reasonix/memory.md · 用户偏好 · 私有', en: '~/.reasonix/memory.md · user preferences · private' },
      { zh: '/memory edit 在 TUI 内直接编辑', en: '/memory edit opens it inside the TUI' },
      { zh: '注入位置位于 cache-stable 前缀 · 不影响命中', en: 'Injected inside the cache-stable prefix · cache hit unaffected' },
    ],
    files: [
      {
        name: '<project>/reasonix.md',
        lang: 'md',
        code: `# reasonix.md
# Reasonix loads this on every session start.

## Conventions
- Package manager is pnpm — don't suggest npm install
- Tests run via \`pnpm test --filter=affected\`
- TypeScript strict mode — no any
- Commits follow Conventional Commits

## Layout
- src/        product code
- packages/   monorepo packages
- tooling/    build scripts — don't touch without asking

## Don't
- Don't auto git commit · wait for me to confirm
- Don't bump version numbers in package.json`,
      },
      {
        name: '~/.reasonix/memory.md',
        lang: 'md',
        code: `# Personal preferences

- Reply in English · keep code comments in English
- Prefer functional style · light on classes
- Small commits · one thing at a time`,
      },
    ],
  },
  {
    id: 'config',
    label: 'Config',
    title: 'Config',
    cn: { zh: '全局与项目级配置', en: 'Global + project config' },
    desc: {
      zh: '一份 JSON 配置承载所有可调项。全局放 ~/.reasonix/config.json，每个项目可以再用 <project>/.reasonix/config.json 局部覆盖。',
      en: 'A single JSON file holds every knob. The global one lives at `~/.reasonix/config.json`; any project can override it locally with `<project>/.reasonix/config.json`.',
    },
    bullets: [
      { zh: '模型 · 推理深度 · 输出格式', en: 'Model · reasoning depth · output format' },
      { zh: 'MCP 服务器声明', en: 'MCP server declarations' },
      { zh: '主题 · 快捷键', en: 'Theme · keybindings' },
      { zh: '项目级覆盖优先于全局', en: 'Project config wins over global' },
    ],
    files: [
      {
        name: '~/.reasonix/config.json',
        lang: 'json',
        code: `{
  "apiKey": "sk-***",
  "model": "deepseek-v4-flash",
  "preset": "balanced",
  "effort": "medium",
  "theme": "ember",
  "autoApply": false,
  "approval": {
    "writeFiles": "ask",
    "runCommand": "ask",
    "webFetch": "allow"
  },
  "telemetry": false
}`,
      },
      {
        name: '<project>/.reasonix/config.json',
        lang: 'json',
        code: `{
  "model": "deepseek-v4-pro",
  "preset": "max",
  "approval": { "writeFiles": "auto" },
  "skills": ["review-pr", "release-notes"]
}`,
      },
    ],
  },
  {
    id: 'slash',
    label: 'Slash',
    title: 'Slash Commands',
    cn: { zh: 'TUI 内的快捷指令', en: 'TUI shortcut commands' },
    desc: {
      zh: '在交互式 TUI 中以 / 开头的命令直接控制 session 行为。所有命令支持 "did you mean /…?" 模糊纠错。输入 /help 查看完整列表。',
      en: 'Inside the TUI, anything starting with `/` controls the session directly. Every command supports "did you mean /…?" fuzzy correction. Type `/help` for the full list.',
    },
    bullets: [
      { zh: '/pro · /preset · /effort   — 模型与推理深度切换', en: '/pro · /preset · /effort   — switch model + reasoning depth' },
      { zh: '/plan · /apply · /discard  — 编辑审批门', en: '/plan · /apply · /discard  — edit approval gate' },
      { zh: '/mcp · /skill · /memory    — 外部能力与剧本管理', en: '/mcp · /skill · /memory    — capabilities + playbooks' },
      { zh: '/status · /stats · /replay — 会话状态与回放', en: '/status · /stats · /replay — session state + replay' },
    ],
    files: [
      {
        name: 'common commands',
        lang: 'shell',
        code: `# Reasoning depth + model
› /pro                # next turn on V4-Pro
› /preset max         # whole session on Pro
› /effort high        # think harder

# Edit approval
› /plan               # enter read-only audit gate
› /apply              # commit pending edits
› /discard            # drop all pending edits

# Capabilities
› /mcp list           # mounted MCP servers
› /skill new fix-bug  # scaffold a new skill
› /memory edit        # open reasonix.md

# Session + replay
› /status             # model · cache hit · cost
› /stats              # token + cost stats
› /replay -1          # replay the previous session
› /help               # full command reference`,
      },
    ],
  },
];

function syntaxHighlight(code, lang) {
  const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

  // Build a single token list per line to avoid double-wrapping spans.
  const lines = code.split('\n');
  const out = lines.map((line) => {
    if (lang === 'json') {
      let result = '';
      let rest = line;
      while (rest.length) {
        let m;
        if ((m = rest.match(/^(\s*)("(?:[^"\\]|\\.)*")(\s*:)/))) {
          result += esc(m[1]) + '<span style="color:#7ec8ff">' + esc(m[2]) + '</span>' + esc(m[3]);
          rest = rest.slice(m[0].length);
        } else if ((m = rest.match(/^("(?:[^"\\]|\\.)*")/))) {
          result += '<span style="color:#00e5a8">' + esc(m[1]) + '</span>';
          rest = rest.slice(m[0].length);
        } else if ((m = rest.match(/^(true|false|null)\b/))) {
          result += '<span style="color:#ffb84d">' + esc(m[1]) + '</span>';
          rest = rest.slice(m[0].length);
        } else if ((m = rest.match(/^(-?\d+(?:\.\d+)?)/))) {
          result += '<span style="color:#ffb84d">' + esc(m[1]) + '</span>';
          rest = rest.slice(m[0].length);
        } else {
          result += esc(rest[0]);
          rest = rest.slice(1);
        }
      }
      return result;
    }
    if (lang === 'md') {
      if (/^---$/.test(line)) return '<span style="color:#6b7593">' + esc(line) + '</span>';
      if (/^#{1,3}\s/.test(line)) return '<span style="color:#7ec8ff">' + esc(line) + '</span>';
      let m = line.match(/^([a-zA-Z\-]+:)(.*)$/);
      if (m) return '<span style="color:#ffb84d">' + esc(m[1]) + '</span>' + esc(m[2]);
      if (/^- /.test(line)) return '<span style="color:#a3adc6">' + esc(line) + '</span>';
      return esc(line);
    }
    if (lang === 'bash' || lang === 'shell') {
      if (/^\s*#/.test(line)) return '<span style="color:#6b7593">' + esc(line) + '</span>';
      let result = '';
      let rest = line;
      let m = rest.match(/^(›\s|\$\s)/);
      if (m) {
        result += '<span style="color:#4d6bfe">' + esc(m[1]) + '</span>';
        rest = rest.slice(m[0].length);
      }
      const parts = rest.split(/(\s+)/);
      for (const p of parts) {
        if (/^\s+$/.test(p)) { result += esc(p); continue; }
        if (/^\/[a-z][a-z\-]+$/i.test(p)) {
          result += '<span style="color:#7c5cff">' + esc(p) + '</span>';
        } else if (/^--?[a-z][a-zA-Z\-]+$/.test(p)) {
          result += '<span style="color:#ffb84d">' + esc(p) + '</span>';
        } else {
          result += esc(p);
        }
      }
      return result;
    }
    return esc(line);
  });
  return out.join('\n');
}

function CodePanel({ file }) {
  const [copied, setCopied] = React.useState(false);
  const copy = () => {
    navigator.clipboard?.writeText(file.code).catch(()=>{});
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <div className="code-panel">
      <div className="code-panel-head">
        <span className="code-file"><Ic.Terminal size={12}/> {file.name}</span>
        <button className={'copy-btn ' + (copied?'copied':'')} onClick={copy} style={{marginLeft:'auto'}}>
          {copied ? <Ic.Check/> : <Ic.Copy/>} {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <pre className="code-body" dangerouslySetInnerHTML={{ __html: syntaxHighlight(file.code, file.lang) }}/>
    </div>
  );
}

function Config() {
  const [tab, setTab] = React.useState('mcp');
  const { lang } = useLang();
  const cur = CONFIG_TABS.find(it => it.id === tab) || CONFIG_TABS[0];

  return (
    <section className="section" id="config">
      <SecHead
        num="04"
        label="Configure"
        title={t({
          zh: '扩展、记忆、配置 — <em>纯文本</em>就够了。',
          en: 'Extensions, memory, config — <em>plain text</em> is enough.',
        }, lang)}
        sub={t({
          zh: 'Reasonix 把可扩展性收敛到几个明确的目录与文件 —— 没有花哨的注册表，所有内容都是可读、可 diff、可入库的纯文本。',
          en: 'Reasonix collapses extensibility into a handful of well-defined directories and files. No registries, no magic — everything is readable, diffable, git-trackable plain text.',
        }, lang)}
      />

      <div className="config-grid">
        <div className="config-side">
          {CONFIG_TABS.map(it => (
            <div key={it.id} className={'config-tab ' + (it.id === tab ? 'on' : '')} onClick={() => setTab(it.id)}>
              <span className="config-tab-key">/{it.label.toLowerCase()}</span>
              <div>
                <div className="config-tab-title">{it.title}</div>
                <div className="config-tab-cn">{t(it.cn, lang)}</div>
              </div>
              <Ic.Arrow size={13}/>
            </div>
          ))}
          <div className="config-hint">
            <Ic.Sparkle size={13}/>
            <span>
              {t({
                zh: <>所有路径与命令均来自 <a href="https://github.com/esengine/DeepSeek-Reasonix" target="_blank" rel="noreferrer" style={{color:'var(--accent)', textDecoration:'none'}}>esengine/DeepSeek-Reasonix</a>。</>,
                en: <>Every path and command shown lives in <a href="https://github.com/esengine/DeepSeek-Reasonix" target="_blank" rel="noreferrer" style={{color:'var(--accent)', textDecoration:'none'}}>esengine/DeepSeek-Reasonix</a>.</>,
              }, lang)}
            </span>
          </div>
        </div>

        <div className="config-main" key={cur.id}>
          <div className="config-main-head">
            <h3>{cur.title}<span> · {t(cur.cn, lang)}</span></h3>
            <p>{t(cur.desc, lang)}</p>
          </div>

          <ul className="config-bullets">
            {cur.bullets.map((b, i) => (
              <li key={i}>
                <span className="bullet-dot"></span>
                <span>{t(b, lang)}</span>
              </li>
            ))}
          </ul>

          <div className="config-files">
            {cur.files.map((f, i) => <CodePanel key={i} file={f}/>)}
          </div>
        </div>
      </div>
    </section>
  );
}

window.Config = Config;
