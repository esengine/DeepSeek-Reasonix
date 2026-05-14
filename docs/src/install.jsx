// CLI install + verification

function CopyCmd({ cmd }) {
  const [copied, setCopied] = React.useState(false);
  const copy = () => {
    navigator.clipboard?.writeText(cmd).catch(()=>{});
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <div className="copy-block">
      <span className="cmd">
        <span className="tok-cmt">$ </span>
        {cmd}
      </span>
      <button className={'copy-btn ' + (copied?'copied':'')} onClick={copy}>
        {copied ? <Ic.Check/> : <Ic.Copy/>}
        {copied ? 'Copied' : 'Copy'}
      </button>
    </div>
  );
}

const INSTALL_TABS = [
  { id: 'npx',  label: { zh: 'npx (推荐)', en: 'npx (recommended)' },
    cmd: 'npx reasonix code',
    note: { zh: '无需全局安装，进入项目目录即用', en: 'No global install — runs in your project dir' } },
  { id: 'npm',  label: { zh: 'npm', en: 'npm' },
    cmd: 'npm install -g reasonix && reasonix code',
    note: { zh: '需要 Node ≥ 22 (或 ≥ 20.10)', en: 'Requires Node ≥ 22 (or ≥ 20.10)' } },
  { id: 'pnpm', label: { zh: 'pnpm', en: 'pnpm' },
    cmd: 'pnpm add -g reasonix && reasonix code',
    note: { zh: '全局安装速度更快', en: 'Faster global install' } },
  { id: 'src',  label: { zh: 'from source', en: 'from source' },
    cmd: 'git clone https://github.com/esengine/DeepSeek-Reasonix && cd DeepSeek-Reasonix && npm install && npm run dev code',
    note: { zh: '需要参与开发请走源码', en: 'Go via source if you want to contribute' } },
];

function Install() {
  const [tab, setTab] = React.useState('npx');
  const { lang } = useLang();
  const active = INSTALL_TABS.find(it => it.id === tab) || INSTALL_TABS[0];

  return (
    <section className="section" id="install">
      <SecHead
        num="01"
        label="Install"
        title={t({ zh: '<em>两步</em>运行，免全局安装。', en: '<em>Two steps</em>. No global install.' }, lang)}
        sub={t({
          zh: 'Node ≥ 22，支持 macOS / Linux / Windows (PowerShell · Git Bash · Windows Terminal)。首次运行内置向导会引导你粘贴 DeepSeek API Key。',
          en: 'Node ≥ 22 on macOS / Linux / Windows (PowerShell · Git Bash · Windows Terminal). The first launch walks you through pasting a DeepSeek API key.',
        }, lang)}
      />

      <div className="tabs" role="tablist">
        {INSTALL_TABS.map(it => (
          <button key={it.id} className={tab === it.id ? 'on' : ''} onClick={() => setTab(it.id)}>
            {t(it.label, lang)}
          </button>
        ))}
      </div>

      <CopyCmd cmd={active.cmd}/>
      <p style={{color:'var(--cream-mute)', fontSize:12.5, marginTop:14, fontFamily:'var(--mono)', letterSpacing:'0.04em'}}>
        // {t(active.note, lang)}
      </p>

      <div style={{display:'grid', gridTemplateColumns:'repeat(3, 1fr)', gap: 0, marginTop: 56, borderTop: '1px solid var(--rule)'}}>
        <div className="card" style={{padding:'32px 28px 32px 0', borderRight:'1px solid var(--rule)'}}>
          <div style={{fontFamily:'var(--mono)', fontSize:11, letterSpacing:'0.14em', color:'var(--cream-mute)', textTransform:'uppercase', marginBottom: 18}}>01 — API Key</div>
          <h3 style={{fontFamily:'var(--serif)', fontStyle:'italic', fontWeight:400, fontSize:26, letterSpacing:'-0.005em', margin:'0 0 10px', color:'var(--cream)'}}>
            {t({ zh: '获取 DeepSeek API Key', en: 'Get a DeepSeek API key' }, lang)}
          </h3>
          <p style={{color:'var(--cream-dim)', fontSize:14.5, marginTop:6, marginBottom:14, lineHeight:1.6}}>
            {t({
              zh: <>前往 <a href="https://platform.deepseek.com" target="_blank" rel="noreferrer" style={{color:'var(--accent)',textDecoration:'none', borderBottom:'1px solid var(--accent-line)'}}>DeepSeek 开放平台</a> 创建一个 Key，按量计费、命中缓存的 token 仅原价 1/5。</>,
              en: <>Head to the <a href="https://platform.deepseek.com" target="_blank" rel="noreferrer" style={{color:'var(--accent)',textDecoration:'none', borderBottom:'1px solid var(--accent-line)'}}>DeepSeek platform</a> and create a key. Pay-as-you-go; cached tokens bill at 1/5 of the regular rate.</>,
            }, lang)}
          </p>
          <p style={{color:'var(--cream-mute)', fontSize:11.5, marginTop:0, marginBottom:0, fontFamily:'var(--mono)', letterSpacing:'0.04em'}}>
            $0.07 /Mtok in · $0.014 /Mtok cached
          </p>
        </div>
        <div className="card" style={{padding:'32px 28px', borderRight:'1px solid var(--rule)'}}>
          <div style={{fontFamily:'var(--mono)', fontSize:11, letterSpacing:'0.14em', color:'var(--cream-mute)', textTransform:'uppercase', marginBottom: 18}}>02 — Workspace</div>
          <h3 style={{fontFamily:'var(--serif)', fontStyle:'italic', fontWeight:400, fontSize:26, letterSpacing:'-0.005em', margin:'0 0 14px', color:'var(--cream)'}}>
            {t({ zh: '进入项目目录', en: 'Enter your project' }, lang)}
          </h3>
          <div className="copy-block" style={{fontSize:13, maxWidth:'none'}}>
            <span className="cmd"><span className="tok-cmt">$ </span>cd /path/to/my-project</span>
          </div>
          <p style={{color:'var(--cream-mute)', fontSize:11.5, marginTop:14, marginBottom:0, fontFamily:'var(--mono)', letterSpacing:'0.04em'}}>
            // tools sandboxed to launch dir
          </p>
        </div>
        <div className="card" style={{padding:'32px 0 32px 28px'}}>
          <div style={{fontFamily:'var(--mono)', fontSize:11, letterSpacing:'0.14em', color:'var(--cream-mute)', textTransform:'uppercase', marginBottom: 18}}>03 — Run</div>
          <h3 style={{fontFamily:'var(--serif)', fontStyle:'italic', fontWeight:400, fontSize:26, letterSpacing:'-0.005em', margin:'0 0 14px', color:'var(--cream)'}}>
            {t({ zh: '启动 TUI 会话', en: 'Launch the TUI' }, lang)}
          </h3>
          <div className="copy-block" style={{fontSize:13, maxWidth:'none'}}>
            <span className="cmd"><span className="tok-cmt">$ </span>npx reasonix code</span>
          </div>
          <p style={{color:'var(--cream-mute)', fontSize:11.5, marginTop:14, marginBottom:0, fontFamily:'var(--mono)', letterSpacing:'0.04em'}}>
            {t({ zh: '// 首次启动向导自动注入 Key', en: '// first-launch wizard wires up the key' }, lang)}
          </p>
        </div>
      </div>
    </section>
  );
}

window.Install = Install;
window.CopyCmd = CopyCmd;
