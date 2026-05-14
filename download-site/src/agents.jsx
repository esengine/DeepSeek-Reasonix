// Three Pillars — Cache-First Loop, R1 Thought Harvest, Tool-Call Repair
// Replaces the previous "agents matrix" since Reasonix is one focused agent
// engineered around three invariants.

const PILLARS = [
  {
    id: 'cache',
    name: 'Cache-First Loop',
    cn: '字节稳定的运行循环',
    badge: 'P1',
    summary:
      'DeepSeek 的 prefix-cache 从 prompt 第 0 字节开始指纹化。Reasonix 的循环是 append-only —— 不重排、不基于 marker 做压缩 —— 让缓存前缀在每一次工具调用后都保持稳定。',
    metric: '94%',
    metricLabel: 'cache hit · long sessions',
    points: [
      { n: '01', label: 'append-only',      desc: '消息、工具结果一律尾部追加，绝不修改历史' },
      { n: '02', label: 'no marker',        desc: '不使用 cache_control 之类的标记触发器' },
      { n: '03', label: 'stable order',     desc: '工具调用顺序与时间戳完全确定性' },
      { n: '04', label: 'prefix-survive',   desc: '即使 dispatch 多次工具，前缀仍命中' },
    ],
  },
  {
    id: 'r1',
    name: 'R1 Thought Harvest',
    cn: '推理链回收',
    badge: 'P2',
    summary:
      '当模型在 <think> 块里"想偏了"把工具调用写进了思考内容，Reasonix 会做一次扫掠（scavenge pass）把这些逃逸的 tool call 抓回来执行，不浪费推理 token。',
    metric: '+38%',
    metricLabel: 'tool dispatch recovered',
    points: [
      { n: '01', label: 'capture',  desc: '解析 <think> 块，识别其中的 tool-call 语法' },
      { n: '02', label: 'replay',   desc: '把抓出的调用重新走 dispatch 通道' },
      { n: '03', label: 'effort',   desc: '/effort 控制推理深度，便宜回合可降级' },
      { n: '04', label: 'observe',  desc: '所有 harvest 操作落盘到 events 日志' },
    ],
  },
  {
    id: 'repair',
    name: 'Tool-Call Repair',
    cn: '工具调用自愈',
    badge: 'P3',
    summary:
      '模型生成的工具参数偶尔会有 JSON 拼写错、引号不闭合、shape 不一致的情况。Reasonix 在送入 dispatch 之前先做一轮 schema-aware 的修复，把畸形参数补好再执行。',
    metric: '< 0.3%',
    metricLabel: 'tool failures after repair',
    points: [
      { n: '01', label: 'parse',    desc: 'JSON5 / 容错解析，识别常见畸形写法' },
      { n: '02', label: 'reshape',  desc: '按 schema 重排字段名 · 修补默认值' },
      { n: '03', label: 'retry',    desc: '修复失败时优雅回报 · 让模型自我纠正' },
      { n: '04', label: 'log',      desc: '所有修复动作可在 reasonix replay 中回放' },
    ],
  },
];

function Agents() {
  const [sel, setSel] = React.useState('cache');
  const cur = PILLARS.find(a => a.id === sel) || PILLARS[0];

  return (
    <section className="section" id="agents">
      <SecHead
        num="02"
        label="Three Pillars"
        title="为什么是 <em>DeepSeek</em> 原生"
        sub="Reasonix 只对接 DeepSeek，因为这套循环的不变量是按 DeepSeek 的 cache 机制设计的。同样的模型、同样的 API —— 改变的是循环的工程姿态。"
      />

      <div className="agents">
        <div className="agent-list">
          {PILLARS.map(a => (
            <div key={a.id} className={'agent-item ' + (a.id === sel ? 'on' : '')} onClick={() => setSel(a.id)}>
              <span className="dot"></span>
              <div className="label">
                {a.name}
                <small>{a.cn}</small>
              </div>
              <span className="meta">{a.badge}</span>
            </div>
          ))}
        </div>
        <div className="agent-detail" key={cur.id}>
          <div className="en">{cur.name}</div>
          <h3>{cur.cn}</h3>
          <p>{cur.summary}</p>

          <div className="metric-bar">
            <b>{cur.metric}</b>
            <span>{cur.metricLabel}</span>
          </div>

          <div className="agent-flow">
            {cur.points.map(s => (
              <div key={s.n} className="step">
                <b>{s.n}</b>
                <em>{s.label}</em>
                <span>— {s.desc}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}

window.Agents = Agents;
