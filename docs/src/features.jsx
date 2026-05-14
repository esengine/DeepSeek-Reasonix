// Feature grid — editorial numbered tiles

const FEATURES = [
  {
    title: { zh: '终端原生 TUI', en: 'Terminal-native TUI' },
    en: 'TypeScript + Ink TUI',
    desc: {
      zh: '不是又一个 IDE 插件。diff 留给 git diff，文件树留给 ls —— 终端就是工作面板。',
      en: 'Not another IDE plugin. `git diff` handles diffs, `ls` handles file trees — your terminal is the workspace.',
    },
  },
  {
    title: { zh: 'V4 双档位', en: 'V4 two-tier' },
    en: 'Flash by default · /pro on demand',
    desc: {
      zh: '默认 V4-Flash 跑日常迭代控成本，/pro 单回合切到 V4-Pro，/preset max 整个 session 走 Pro。',
      en: 'V4-Flash by default for cheap iteration; `/pro` lifts a single turn to V4-Pro; `/preset max` makes the whole session run on Pro.',
    },
  },
  {
    title: { zh: 'MCP first-class', en: 'MCP first-class' },
    en: 'stdio · SSE · Streamable HTTP',
    desc: {
      zh: '一行 --mcp "name=cmd args" 接入外部服务器，工具以前缀合并进同一个 registry。',
      en: 'One line — `--mcp "name=cmd args"` — and an external server is wired in; its tools merge into the same registry under a prefix.',
    },
  },
  {
    title: { zh: '沙箱与计划门', en: 'Sandbox + plan gate' },
    en: 'Sandbox + /plan gate',
    desc: {
      zh: '所有原生工具沙箱化到启动目录；/plan 进入只读审计门，未批准前不允许写入。',
      en: 'Every built-in tool is sandboxed to the launch dir; `/plan` puts the session behind a read-only audit gate — no writes until the plan is approved.',
    },
  },
  {
    title: { zh: 'Skills 可编排', en: 'Composable skills' },
    en: 'Markdown skill scripts',
    desc: {
      zh: '.reasonix/skills/<name>.md，frontmatter 支持 runAs: subagent + allowed-tools 隔离运行。',
      en: 'Drop a Markdown file in `.reasonix/skills/<name>.md`; frontmatter supports `runAs: subagent` and `allowed-tools` for isolated execution.',
    },
  },
  {
    title: { zh: 'Replay & Events', en: 'Replay & events' },
    en: 'reasonix replay / events / stats',
    desc: {
      zh: '完整事件流落盘，可回放任意一次会话，可统计 token / cache / 成本，便于审计。',
      en: 'Every event hits disk — replay any past session, run stats on token / cache / cost, audit your loop\'s behaviour.',
    },
  },
];

function Features() {
  const { lang } = useLang();
  return (
    <section className="section" id="features">
      <SecHead
        num="03"
        label="Features"
        title={t({
          zh: '围绕 <em>DeepSeek API</em> 的工程姿态。',
          en: 'Engineering stance around the <em>DeepSeek API</em>.',
        }, lang)}
        sub={t({
          zh: '十几个工具一起构成一个看似简单的命令行 —— 但底下的每一层都在为缓存命中、成本和稳定性服务。',
          en: 'A dozen-odd tools combine into what looks like a simple CLI — but every layer beneath is working to keep cache hit, cost, and stability where they need to be.',
        }, lang)}
      />

      <div className="feat-grid">
        {FEATURES.map((f, i) => (
          <div key={f.en} className="feat">
            <div className="feat-num">F-{String(i + 1).padStart(2, '0')}</div>
            <h3>{t(f.title, lang)}</h3>
            <p>{t(f.desc, lang)}</p>
            <span className="feat-en">{f.en}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

window.Features = Features;
