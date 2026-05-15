// Roadmap — based on real Reasonix release notes & wishlist discussion

const ROADMAP = [
  {
    key: 'shipped',
    title: { zh: '已发布', en: 'Shipped' },
    state: 'done',
    items: [
      { zh: 'Cache-First Loop · prefix 字节稳定', en: 'Cache-First Loop · byte-stable prefix' },
      { zh: 'R1 Thought Harvest · 思考逃逸回收', en: 'R1 Thought Harvest · scavenge escaped tool calls' },
      { zh: 'Tool-Call Repair · 工具参数自愈', en: 'Tool-Call Repair · schema-aware self-heal' },
      { zh: 'MCP first-class (stdio / SSE / HTTP)', en: 'MCP first-class (stdio / SSE / HTTP)' },
      { zh: 'Skills · Markdown frontmatter 剧本', en: 'Skills · Markdown frontmatter scripts' },
      { zh: '原生 Tauri 桌面端', en: 'Native Tauri desktop' },
    ],
  },
  {
    key: 'v0.44.x',
    title: { zh: '迭代中', en: 'In progress' },
    state: 'now',
    items: [
      { zh: '/skill new <name> 脚手架命令', en: '/skill new <name> scaffolder' },
      { zh: 'setup-wizard 主题选择 + live preview', en: 'setup wizard theme picker + live preview' },
      { zh: '"did you mean /…?" 模糊纠错', en: '"did you mean /…?" fuzzy correction' },
      { zh: 'install-source-aware reasonix update', en: 'install-source-aware `reasonix update`' },
      { zh: 'zh-CN 覆盖扩展至卡片组件', en: 'zh-CN coverage extended to card components' },
    ],
  },
  {
    key: 'next',
    title: { zh: '路线图', en: 'Roadmap' },
    state: 'plan',
    items: [
      { zh: 'reasonix init · 项目脚手架 CLI', en: 'reasonix init · project scaffolder CLI' },
      { zh: '跨设备 context 同步', en: 'Cross-device context sync' },
      { zh: 'Plugin system (Claude .claude-plugin/ 兼容)', en: 'Plugin system (.claude-plugin/ compatible)' },
      { zh: 'Repo map · 仓库语义索引', en: 'Repo map · semantic repository index' },
      { zh: 'TUI 浅色主题', en: 'TUI light theme' },
    ],
  },
  {
    key: 'wishlist',
    title: { zh: '社区许愿', en: 'Wishlist' },
    state: 'plan',
    items: [
      { zh: '多 agent 协作 · 持久 worker', en: 'Multi-agent collaboration · persistent workers' },
      { zh: '跨 provider 编排 (codex + deepseek)', en: 'Cross-provider orchestration (codex + deepseek)' },
      { zh: 'composer 语音输入', en: 'Composer voice input' },
      { zh: '托管服务模式', en: 'Hosted service mode' },
      { zh: '更多语言 i18n 覆盖', en: 'More-language i18n coverage' },
    ],
  },
];

function Roadmap() {
  const { lang } = useLang();
  return (
    <section className="section" id="roadmap">
      <SecHead
        num="07"
        label="Roadmap"
        title={t({ zh: '<em>公开的</em>产品节奏。', en: 'A <em>public</em> product cadence.' }, lang)}
        sub={t({
          zh: '所有里程碑同步在 GitHub Discussions 的 wishlist。issue 投票影响优先级，PR 决定速度。',
          en: 'Every milestone lives in the GitHub Discussions wishlist. Issue votes shape priority; PRs decide pace.',
        }, lang)}
      />

      <div className="roadmap">
        {ROADMAP.map(c => (
          <div key={c.key} className={'rm-col ' + c.state}>
            <header>
              <span className="q">{c.key}</span>
              <h4>{t(c.title, lang)}</h4>
            </header>
            <ul>
              {c.items.map((it, idx) => <li key={idx}>{t(it, lang)}</li>)}
            </ul>
          </div>
        ))}
      </div>
    </section>
  );
}

window.Roadmap = Roadmap;
