// Hero — animated terminal showing a real Reasonix coding session

// Based on the actual reasonix code TUI: cache-first loop, SEARCH/REPLACE edits,
// tool calls sandboxed to launch dir.
const TERM_SCRIPT = [
  { t: 'cmd', text: 'npx reasonix code' },
  { t: 'out', text: '⏺ reasonix 0.42.0-3 · model: deepseek-v4-flash · workspace: ~/app', cls: 'term-info', delay: 280 },
  { t: 'out', text: '⏺ cache: 94.2% hit · session: 18m23s · cost: $0.043', cls: 'term-dim', delay: 220 },
  { t: 'blank' },
  { t: 'cmd', text: 'fix the case-sensitivity bug in findByEmail' },
  { t: 'out', text: '▸ tool<search_files>  → src/users.ts, src/users.test.ts', cls: 'term-dim', delay: 260 },
  { t: 'out', text: '▸ tool<read_file>     → src/users.ts (412 chars)', cls: 'term-dim', delay: 220 },
  { t: 'out', text: '▸ thinking …  reasoning on  · /effort high', cls: 'term-info', delay: 280 },
  { t: 'out', text: 'src/users.ts', cls: 'term-warn', delay: 180 },
  { t: 'out', text: '<<<<<<< SEARCH', cls: 'term-dim', delay: 80 },
  { t: 'out', text: '  return users.find(u => u.email === email);', delay: 80 },
  { t: 'out', text: '=======', cls: 'term-dim', delay: 80 },
  { t: 'out', text: '  const needle = email.toLowerCase();', cls: 'term-ok', delay: 80 },
  { t: 'out', text: '  return users.find(u => u.email.toLowerCase() === needle);', cls: 'term-ok', delay: 80 },
  { t: 'out', text: '>>>>>>> REPLACE', cls: 'term-dim', delay: 80 },
  { t: 'out', text: '▸ 1 pending edit · /apply to write · /discard to drop', cls: 'term-info', delay: 240 },
  { t: 'blank' },
  { t: 'cmd', text: '/apply' },
  { t: 'out', text: '✓ wrote src/users.ts · 2 lines changed', cls: 'term-ok', delay: 200 },
  { t: 'out', text: '✓ npm test users  · 14 passed · 0 failed (3.1s)', cls: 'term-ok', delay: 220 },
];

function Terminal() {
  const [lines, setLines] = React.useState([]);
  const [typing, setTyping] = React.useState('');
  const stepRef = React.useRef(0);
  const charRef = React.useRef(0);

  React.useEffect(() => {
    let cancelled = false;
    let timer;

    const run = () => {
      if (cancelled) return;
      const i = stepRef.current;
      if (i >= TERM_SCRIPT.length) {
        timer = setTimeout(() => {
          if (cancelled) return;
          stepRef.current = 0;
          charRef.current = 0;
          setLines([]);
          run();
        }, 3600);
        return;
      }
      const step = TERM_SCRIPT[i];
      if (step.t === 'cmd') {
        const text = step.text;
        if (charRef.current < text.length) {
          setTyping(text.slice(0, charRef.current + 1));
          charRef.current += 1;
          timer = setTimeout(run, 22 + Math.random() * 28);
        } else {
          setLines(prev => [...prev, { ...step, text }]);
          setTyping('');
          charRef.current = 0;
          stepRef.current += 1;
          timer = setTimeout(run, 380);
        }
      } else if (step.t === 'blank') {
        setLines(prev => [...prev, step]);
        stepRef.current += 1;
        timer = setTimeout(run, 240);
      } else {
        setLines(prev => [...prev, step]);
        stepRef.current += 1;
        timer = setTimeout(run, step.delay || 180);
      }
    };

    timer = setTimeout(run, 600);
    return () => { cancelled = true; clearTimeout(timer); };
  }, []);

  return (
    <div className="terminal" role="presentation" aria-hidden="true">
      <div className="term-head">
        <div className="term-dots"><i></i><i></i><i></i></div>
        <span className="term-title">~/app  ·  reasonix code</span>
        <div className="term-tabs">
          <span className="term-tab on">session</span>
          <span className="term-tab">events</span>
          <span className="term-tab">+</span>
        </div>
      </div>
      <div className="term-body">
        {lines.map((l, i) => {
          if (l.t === 'blank') return <div key={i} className="term-line">&nbsp;</div>;
          if (l.t === 'cmd') {
            return (
              <div key={i} className="term-line">
                <span className="term-prompt-user">›</span>
                <span className="term-cmd">{l.text}</span>
              </div>
            );
          }
          return (
            <div key={i} className={'term-line ' + (l.cls || '')}>{l.text}</div>
          );
        })}
        {typing !== '' ? (
          <div className="term-line">
            <span className="term-prompt-user">›</span>
            <span className="term-cmd">{typing}</span>
            <span className="cursor"></span>
          </div>
        ) : (
          lines.length > 0 && stepRef.current < TERM_SCRIPT.length && (
            <div className="term-line"><span className="term-prompt-user">›</span><span className="cursor"></span></div>
          )
        )}
      </div>
    </div>
  );
}

function Hero() {
  return (
    <section className="hero" id="top">
      <div className="hero-head">
        <span>§00 · Reasonix</span>
        <span className="rule"></span>
        <span className="v">v0.42.0-3 · open source</span>
      </div>
      <div className="hero-grid">
        <div>
          <h1>
            为终端而生的<br/>
            <em>DeepSeek</em> 原生<br/>
            编程 <em>Agent</em>。
          </h1>
          <p className="lede">
            Reasonix 直接对接 <b>api.deepseek.com</b>，围绕 DeepSeek 的字节稳定 prefix-cache 设计了
            append-only 的运行循环 —— 长会话能把缓存命中保持在 90%+，输入 token 成本降到 1/5。
            终端优先，留它一直跑着。
          </p>
          <div className="hero-actions">
            <a className="btn btn-primary" href="#install">
              立即开始 →
            </a>
            <a className="btn btn-ghost" href="download.html">
              下载桌面端
            </a>
          </div>
          <div className="hero-stats">
            <div className="hero-stat"><b>94<span style={{fontFamily:'var(--mono)',fontStyle:'normal',fontSize:18,marginLeft:2,color:'var(--cream-mute)'}}>%</span></b><span>Cache Hit</span></div>
            <div className="hero-stat"><b>2.5<span style={{fontFamily:'var(--mono)',fontStyle:'normal',fontSize:18,marginLeft:2,color:'var(--cream-mute)'}}>×</span></b><span>Cost Down</span></div>
            <div className="hero-stat"><b>2837</b><span>Tests</span></div>
            <div className="hero-stat"><b>MIT</b><span>License</span></div>
          </div>
        </div>
        <div style={{position:'relative'}}>
          <Terminal/>
        </div>
      </div>
    </section>
  );
}

window.Hero = Hero;
