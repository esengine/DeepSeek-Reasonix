// Configuration explorer — MCP, Skills, Memory, Slash commands
// Tabbed interface with code panels showing real Reasonix config

const CONFIG_TABS = [
  {
    id: 'mcp',
    label: 'MCP',
    title: 'Model Context Protocol',
    cn: '外部工具服务器',
    desc: 'MCP 是 Reasonix 接入外部能力的一等公民通道，支持 stdio / SSE / Streamable HTTP 三种传输。每个 server 的工具会以前缀合并进统一的工具 registry，对模型透明。',
    bullets: [
      '一行命令挂载: --mcp \'name=cmd args\'',
      '所有 MCP 工具沙箱权限与原生工具一致',
      '/mcp 子命令查看已挂载服务器 · 健康状态 · 工具清单',
      '失败重连 · 自动 reconnect with backoff',
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
    cn: '可复用的 Markdown 剧本',
    desc: 'Skill 是一段带 frontmatter 的 Markdown，把"做某件事的方式"凝固成可调用单元。runAs: subagent 时会在隔离子 agent 里运行，allowed-tools 限制可用工具集。',
    bullets: [
      '项目级: <project>/.reasonix/skills/<name>.md',
      '全局: ~/.reasonix/skills/<name>.md',
      '/skill new <name> 生成脚手架',
      'runAs: subagent 让 skill 跑在隔离的子循环里',
    ],
    files: [
      {
        name: '.reasonix/skills/review-pr.md',
        lang: 'md',
        code: `---
description: 阅读当前分支 vs main 的 diff，输出 review 意见
runAs: subagent
allowed-tools: [run_command, read_file, grep_files]
---
你是一名严格的 code reviewer。请按以下步骤工作：

1. 运行 \`git diff main..HEAD\` 获取变更
2. 对每个被修改的文件，read_file 加载上下文
3. 输出结构化 review：
   - blockers (必须修复)
   - suggestions (建议改进)
   - nits (风格)
4. 不要写文件 · 不要执行测试

只关注本次 diff 涉及的代码，不要离题。`,
      },
      {
        name: 'invoke',
        lang: 'bash',
        code: `# 在 TUI 中
› /skill run review-pr

# 或者直接当 tool 调用 —— 模型也能主动触发
› 请帮我 review 当前分支`,
      },
    ],
  },
  {
    id: 'memory',
    label: 'Memory',
    title: 'Memory',
    cn: '项目级与全局记忆',
    desc: 'Reasonix 把"应当记住"的内容拆成两层：仓库级的 reasonix.md（提交进 git，团队共享）与用户级的 ~/.reasonix/memory.md（个人偏好，不入库）。每次会话启动时自动注入到 prompt 头部。',
    bullets: [
      '<project>/reasonix.md · 项目约定 · git-tracked',
      '~/.reasonix/memory.md · 用户偏好 · 私有',
      '/memory edit 在 TUI 内直接编辑',
      '注入位置位于 cache-stable 前缀 · 不影响命中',
    ],
    files: [
      {
        name: '<project>/reasonix.md',
        lang: 'md',
        code: `# reasonix.md
# 这个文件会被 Reasonix 在每次会话启动时加载

## 项目约定
- 包管理器使用 pnpm，不要建议 npm install
- 测试运行 \`pnpm test --filter=affected\`
- TypeScript strict 模式，禁用 any
- 提交信息遵循 Conventional Commits

## 目录结构
- src/   业务代码
- packages/   monorepo 子包
- tooling/   构建脚本，未经讨论不要改

## 不要做
- 不要自动 git commit · 等我手动确认
- 不要修改 package.json 里的版本号`,
      },
      {
        name: '~/.reasonix/memory.md',
        lang: 'md',
        code: `# 个人偏好

- 中文回答 · 代码注释保持英文
- 函数式风格优先 · 少用 class
- 喜欢小步提交 · 一次只改一件事`,
      },
    ],
  },
  {
    id: 'config',
    label: 'Config',
    title: 'Config',
    cn: '全局与项目级配置',
    desc: '一份 JSON 配置承载所有可调项。全局放 ~/.reasonix/config.json，每个项目可以再用 <project>/.reasonix/config.json 局部覆盖。',
    bullets: [
      '模型 · 推理深度 · 输出格式',
      'MCP 服务器声明',
      '主题 · 快捷键',
      '项目级覆盖优先于全局',
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
    cn: 'TUI 内的快捷指令',
    desc: '在交互式 TUI 中以 / 开头的命令直接控制 session 行为。所有命令支持 "did you mean /…?" 模糊纠错。输入 /help 查看完整列表。',
    bullets: [
      '/pro · /preset · /effort   — 模型与推理深度切换',
      '/plan · /apply · /discard  — 编辑审批门',
      '/mcp · /skill · /memory    — 外部能力与剧本管理',
      '/status · /stats · /replay — 会话状态与回放',
    ],
    files: [
      {
        name: 'common commands',
        lang: 'shell',
        code: `# 推理深度与模型
› /pro                # 下一回合切到 V4-Pro
› /preset max         # 整个 session 用 Pro
› /effort high        # 强推理 (think harder)

# 编辑审批
› /plan               # 进入只读审计门
› /apply              # 写入待提交的编辑
› /discard            # 丢弃所有 pending 编辑

# 能力管理
› /mcp list           # 已挂载的 MCP server
› /skill new fix-bug  # 新建 skill 脚手架
› /memory edit        # 打开 reasonix.md

# 会话与回放
› /status             # 模型 · 缓存命中 · 成本
› /stats              # token 与费用统计
› /replay -1          # 回放上一次会话
› /help               # 完整命令参考`,
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
      // Handle key, then string-value, then number/bool
      // Tokenize by walking the line
      let result = '';
      let rest = line;
      // simple loop: match key/value pairs
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
      // frontmatter key (only when before --- ends)
      let m = line.match(/^([a-zA-Z\-]+:)(.*)$/);
      if (m) return '<span style="color:#ffb84d">' + esc(m[1]) + '</span>' + esc(m[2]);
      if (/^- /.test(line)) return '<span style="color:#a3adc6">' + esc(line) + '</span>';
      return esc(line);
    }
    if (lang === 'bash' || lang === 'shell') {
      if (/^\s*#/.test(line)) return '<span style="color:#6b7593">' + esc(line) + '</span>';
      // Walk tokens, escaping each chunk separately
      let result = '';
      let rest = line;
      // leading prompt
      let m = rest.match(/^(›\s|\$\s)/);
      if (m) {
        result += '<span style="color:#4d6bfe">' + esc(m[1]) + '</span>';
        rest = rest.slice(m[0].length);
      }
      // tokenize the remainder by whitespace, coloring slash-commands and flags
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
  const cur = CONFIG_TABS.find(t => t.id === tab) || CONFIG_TABS[0];

  return (
    <section className="section" id="config">
      <SecHead
        num="04"
        label="Configure"
        title="扩展、记忆、配置 — <em>纯文本</em>就够了。"
        sub="Reasonix 把可扩展性收敛到几个明确的目录与文件 —— 没有花哨的注册表，所有内容都是可读、可 diff、可入库的纯文本。"
      />

      <div className="config-grid">
        <div className="config-side">
          {CONFIG_TABS.map(t => (
            <div key={t.id} className={'config-tab ' + (t.id === tab ? 'on' : '')} onClick={() => setTab(t.id)}>
              <span className="config-tab-key">/{t.label.toLowerCase()}</span>
              <div>
                <div className="config-tab-title">{t.title}</div>
                <div className="config-tab-cn">{t.cn}</div>
              </div>
              <Ic.Arrow size={13}/>
            </div>
          ))}
          <div className="config-hint">
            <Ic.Sparkle size={13}/>
            <span>所有路径与命令均来自 <a href="https://github.com/esengine/DeepSeek-Reasonix" target="_blank" rel="noreferrer" style={{color:'var(--accent)', textDecoration:'none'}}>esengine/DeepSeek-Reasonix</a>。</span>
          </div>
        </div>

        <div className="config-main" key={cur.id}>
          <div className="config-main-head">
            <h3>{cur.title}<span> · {cur.cn}</span></h3>
            <p>{cur.desc}</p>
          </div>

          <ul className="config-bullets">
            {cur.bullets.map((b, i) => (
              <li key={i}>
                <span className="bullet-dot"></span>
                <span>{b}</span>
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
