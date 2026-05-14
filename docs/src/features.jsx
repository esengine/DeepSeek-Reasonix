// Feature grid — editorial numbered tiles

const FEATURES = [
  {
    title: '终端原生 TUI',
    en: 'TypeScript + Ink TUI',
    desc: '不是又一个 IDE 插件。diff 留给 git diff，文件树留给 ls —— 终端就是工作面板。',
  },
  {
    title: 'V4 双档位',
    en: 'Flash by default · /pro on demand',
    desc: '默认 V4-Flash 跑日常迭代控成本，/pro 单回合切到 V4-Pro，/preset max 整个 session 走 Pro。',
  },
  {
    title: 'MCP first-class',
    en: 'stdio · SSE · Streamable HTTP',
    desc: '一行 --mcp "name=cmd args" 接入外部服务器，工具以前缀合并进同一个 registry。',
  },
  {
    title: '沙箱与计划门',
    en: 'Sandbox + /plan gate',
    desc: '所有原生工具沙箱化到启动目录；/plan 进入只读审计门，未批准前不允许写入。',
  },
  {
    title: 'Skills 可编排',
    en: 'Markdown skill scripts',
    desc: '.reasonix/skills/<name>.md，frontmatter 支持 runAs: subagent + allowed-tools 隔离运行。',
  },
  {
    title: 'Replay & Events',
    en: 'reasonix replay / events / stats',
    desc: '完整事件流落盘，可回放任意一次会话，可统计 token / cache / 成本，便于审计。',
  },
];

function Features() {
  return (
    <section className="section" id="features">
      <SecHead
        num="03"
        label="Features"
        title="围绕 <em>DeepSeek API</em> 的工程姿态。"
        sub="十几个工具一起构成一个看似简单的命令行 —— 但底下的每一层都在为缓存命中、成本和稳定性服务。"
      />

      <div className="feat-grid">
        {FEATURES.map((f, i) => (
          <div key={f.title} className="feat">
            <div className="feat-num">F-{String(i + 1).padStart(2, '0')}</div>
            <h3>{f.title}</h3>
            <p>{f.desc}</p>
            <span className="feat-en">{f.en}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

window.Features = Features;
