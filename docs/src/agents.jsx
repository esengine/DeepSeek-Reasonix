// Three Pillars — Cache-First Loop, R1 Thought Harvest, Tool-Call Repair

const PILLARS = [
  {
    id: 'cache',
    name: 'Cache-First Loop',
    cn: { zh: '字节稳定的运行循环', en: 'A byte-stable loop' },
    badge: 'P1',
    summary: {
      zh: 'DeepSeek 的 prefix-cache 从 prompt 第 0 字节开始指纹化。Reasonix 的循环是 append-only —— 不重排、不基于 marker 做压缩 —— 让缓存前缀在每一次工具调用后都保持稳定。',
      en: 'DeepSeek\'s prefix cache fingerprints prompts from byte 0. The Reasonix loop is append-only — no reordering, no marker-based compaction — so the cached prefix survives every tool call.',
    },
    metric: '94%',
    metricLabel: { zh: 'cache hit · 长会话', en: 'cache hit · long sessions' },
    points: [
      { n: '01', label: 'append-only', desc: { zh: '消息、工具结果一律尾部追加，绝不修改历史', en: 'Messages and tool results are appended; history is never mutated' } },
      { n: '02', label: 'no marker',   desc: { zh: '不使用 cache_control 之类的标记触发器', en: 'No reliance on triggers like cache_control markers' } },
      { n: '03', label: 'stable order',desc: { zh: '工具调用顺序与时间戳完全确定性', en: 'Tool call ordering and timestamps are fully deterministic' } },
      { n: '04', label: 'prefix-survive', desc: { zh: '即使 dispatch 多次工具，前缀仍命中', en: 'Even with many tool dispatches, the prefix still hits' } },
    ],
  },
  {
    id: 'r1',
    name: 'R1 Thought Harvest',
    cn: { zh: '推理链回收', en: 'Reasoning-chain harvest' },
    badge: 'P2',
    summary: {
      zh: '当模型在 <think> 块里"想偏了"把工具调用写进了思考内容，Reasonix 会做一次扫掠（scavenge pass）把这些逃逸的 tool call 抓回来执行，不浪费推理 token。',
      en: 'When the model strays inside a <think> block and writes tool calls into its reasoning, Reasonix runs a scavenge pass to recover those escaped calls and dispatch them — no reasoning tokens go to waste.',
    },
    metric: '+38%',
    metricLabel: { zh: 'tool dispatch 回收', en: 'tool dispatch recovered' },
    points: [
      { n: '01', label: 'capture', desc: { zh: '解析 <think> 块，识别其中的 tool-call 语法', en: 'Parse <think> blocks; recognise tool-call syntax inside them' } },
      { n: '02', label: 'replay',  desc: { zh: '把抓出的调用重新走 dispatch 通道', en: 'Replay recovered calls through the dispatch path' } },
      { n: '03', label: 'effort',  desc: { zh: '/effort 控制推理深度，便宜回合可降级', en: '`/effort` tunes reasoning depth; cheap turns drop down' } },
      { n: '04', label: 'observe', desc: { zh: '所有 harvest 操作落盘到 events 日志', en: 'Every harvest action lands in the events log' } },
    ],
  },
  {
    id: 'repair',
    name: 'Tool-Call Repair',
    cn: { zh: '工具调用自愈', en: 'Tool-call repair' },
    badge: 'P3',
    summary: {
      zh: '模型生成的工具参数偶尔会有 JSON 拼写错、引号不闭合、shape 不一致的情况。Reasonix 在送入 dispatch 之前先做一轮 schema-aware 的修复，把畸形参数补好再执行。',
      en: 'Tool arguments the model produces occasionally have JSON typos, unclosed quotes, or shape mismatches. Reasonix runs a schema-aware repair pass before dispatch so malformed args still execute.',
    },
    metric: '< 0.3%',
    metricLabel: { zh: '修复后剩余工具失败率', en: 'tool failures after repair' },
    points: [
      { n: '01', label: 'parse',   desc: { zh: 'JSON5 / 容错解析，识别常见畸形写法', en: 'JSON5 / lenient parser catches common malformations' } },
      { n: '02', label: 'reshape', desc: { zh: '按 schema 重排字段名 · 修补默认值', en: 'Reshape against the schema; fill in defaults' } },
      { n: '03', label: 'retry',   desc: { zh: '修复失败时优雅回报 · 让模型自我纠正', en: 'When repair fails, report cleanly so the model self-corrects' } },
      { n: '04', label: 'log',     desc: { zh: '所有修复动作可在 reasonix replay 中回放', en: 'Every repair action is replayable via reasonix replay' } },
    ],
  },
];

function Agents() {
  const [sel, setSel] = React.useState('cache');
  const { lang } = useLang();
  const cur = PILLARS.find(a => a.id === sel) || PILLARS[0];

  return (
    <section className="section" id="agents">
      <SecHead
        num="02"
        label="Three Pillars"
        title={t({ zh: '为什么是 <em>DeepSeek</em> 原生', en: 'Why <em>DeepSeek</em>-native' }, lang)}
        sub={t({
          zh: 'Reasonix 只对接 DeepSeek，因为这套循环的不变量是按 DeepSeek 的 cache 机制设计的。同样的模型、同样的 API —— 改变的是循环的工程姿态。',
          en: 'Reasonix only targets DeepSeek because the loop\'s invariants are designed against DeepSeek\'s cache mechanics. Same model, same API — what changes is the engineering stance of the loop.',
        }, lang)}
      />

      <div className="agents">
        <div className="agent-list">
          {PILLARS.map(a => (
            <div key={a.id} className={'agent-item ' + (a.id === sel ? 'on' : '')} onClick={() => setSel(a.id)}>
              <span className="dot"></span>
              <div className="label">
                {a.name}
                <small>{t(a.cn, lang)}</small>
              </div>
              <span className="meta">{a.badge}</span>
            </div>
          ))}
        </div>
        <div className="agent-detail" key={cur.id}>
          <div className="en">{cur.name}</div>
          <h3>{t(cur.cn, lang)}</h3>
          <p>{t(cur.summary, lang)}</p>

          <div className="metric-bar">
            <b>{cur.metric}</b>
            <span>{t(cur.metricLabel, lang)}</span>
          </div>

          <div className="agent-flow">
            {cur.points.map(s => (
              <div key={s.n} className="step">
                <b>{s.n}</b>
                <em>{s.label}</em>
                <span>— {t(s.desc, lang)}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}

window.Agents = Agents;
