// Roadmap — based on real Reasonix release notes & wishlist discussion

const ROADMAP = [
  {
    key: 'shipped',
    title: '已发布',
    state: 'done',
    items: [
      'Cache-First Loop · prefix 字节稳定',
      'R1 Thought Harvest · 思考逃逸回收',
      'Tool-Call Repair · 工具参数自愈',
      'MCP first-class (stdio / SSE / HTTP)',
      'Skills · Markdown frontmatter 剧本',
      '原生 Tauri 桌面端 (prerelease)',
    ],
  },
  {
    key: 'v0.30.x',
    title: '迭代中',
    state: 'now',
    items: [
      '/skill new <name> 脚手架命令',
      'setup-wizard 主题选择 + live preview',
      '"did you mean /…?" 模糊纠错',
      'install-source-aware reasonix update',
      'zh-CN 覆盖扩展至卡片组件',
    ],
  },
  {
    key: 'next',
    title: 'Roadmap',
    state: 'plan',
    items: [
      'reasonix init · 项目脚手架 CLI',
      '跨设备 context 同步',
      'Plugin system (Claude .claude-plugin/ 兼容)',
      'Repo map · 仓库语义索引',
      'TUI 浅色主题',
    ],
  },
  {
    key: 'wishlist',
    title: '社区许愿',
    state: 'plan',
    items: [
      '多 agent 协作 · 持久 worker',
      '跨 provider 编排 (codex + deepseek)',
      'composer 语音输入',
      '托管服务模式',
      '更多语言 i18n 覆盖',
    ],
  },
];

function Roadmap() {
  return (
    <section className="section" id="roadmap">
      <SecHead
        num="07"
        label="Roadmap"
        title="<em>公开的</em>产品节奏。"
        sub="所有里程碑同步在 GitHub Discussions 的 wishlist。issue 投票影响优先级，PR 决定速度。"
      />

      <div className="roadmap">
        {ROADMAP.map(c => (
          <div key={c.key} className={'rm-col ' + c.state}>
            <header>
              <span className="q">{c.key}</span>
              <h4>{c.title}</h4>
            </header>
            <ul>
              {c.items.map(i => <li key={i}>{i}</li>)}
            </ul>
          </div>
        ))}
      </div>
    </section>
  );
}

window.Roadmap = Roadmap;
