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
  { id: 'npx',  label: 'npx (推荐)', cmd: 'npx reasonix code',                                        note: '无需全局安装，进入项目目录即用' },
  { id: 'npm',  label: 'npm',        cmd: 'npm install -g reasonix && reasonix code',                  note: '需要 Node ≥ 22 (或 ≥ 20.10)' },
  { id: 'pnpm', label: 'pnpm',       cmd: 'pnpm add -g reasonix && reasonix code',                     note: '全局安装速度更快' },
  { id: 'src',  label: 'from source',cmd: 'git clone https://github.com/esengine/DeepSeek-Reasonix && cd DeepSeek-Reasonix && npm install && npm run dev code', note: '需要参与开发请走源码' },
];

function Install() {
  const [tab, setTab] = React.useState('npx');
  const active = INSTALL_TABS.find(t => t.id === tab) || INSTALL_TABS[0];

  return (
    <section className="section" id="install">
      <SecHead
        num="01"
        label="Install"
        title="<em>两步</em>运行，免全局安装。"
        sub="Node ≥ 22，支持 macOS / Linux / Windows (PowerShell · Git Bash · Windows Terminal)。首次运行内置向导会引导你粘贴 DeepSeek API Key。"
      />

      <div className="tabs" role="tablist">
        {INSTALL_TABS.map(t => (
          <button key={t.id} className={tab === t.id ? 'on' : ''} onClick={() => setTab(t.id)}>
            {t.label}
          </button>
        ))}
      </div>

      <CopyCmd cmd={active.cmd}/>
      <p style={{color:'var(--cream-mute)', fontSize:12.5, marginTop:14, fontFamily:'var(--mono)', letterSpacing:'0.04em'}}>
        // {active.note}
      </p>

      <div style={{display:'grid', gridTemplateColumns:'repeat(3, 1fr)', gap: 0, marginTop: 56, borderTop: '1px solid var(--rule)'}}>
        <div className="card" style={{padding:'32px 28px 32px 0', borderRight:'1px solid var(--rule)'}}>
          <div style={{fontFamily:'var(--mono)', fontSize:11, letterSpacing:'0.14em', color:'var(--cream-mute)', textTransform:'uppercase', marginBottom: 18}}>01 — API Key</div>
          <h3 style={{fontFamily:'var(--serif)', fontStyle:'italic', fontWeight:400, fontSize:26, letterSpacing:'-0.005em', margin:'0 0 10px', color:'var(--cream)'}}>获取 DeepSeek API Key</h3>
          <p style={{color:'var(--cream-dim)', fontSize:14.5, marginTop:6, marginBottom:14, lineHeight:1.6}}>
            前往 <a href="https://platform.deepseek.com" target="_blank" rel="noreferrer" style={{color:'var(--accent)',textDecoration:'none', borderBottom:'1px solid var(--accent-line)'}}>DeepSeek 开放平台</a> 创建一个 Key，按量计费、命中缓存的 token 仅原价 1/5。
          </p>
          <p style={{color:'var(--cream-mute)', fontSize:11.5, marginTop:0, marginBottom:0, fontFamily:'var(--mono)', letterSpacing:'0.04em'}}>
            $0.07 /Mtok in · $0.014 /Mtok cached
          </p>
        </div>
        <div className="card" style={{padding:'32px 28px', borderRight:'1px solid var(--rule)'}}>
          <div style={{fontFamily:'var(--mono)', fontSize:11, letterSpacing:'0.14em', color:'var(--cream-mute)', textTransform:'uppercase', marginBottom: 18}}>02 — Workspace</div>
          <h3 style={{fontFamily:'var(--serif)', fontStyle:'italic', fontWeight:400, fontSize:26, letterSpacing:'-0.005em', margin:'0 0 14px', color:'var(--cream)'}}>进入项目目录</h3>
          <div className="copy-block" style={{fontSize:13, maxWidth:'none'}}>
            <span className="cmd"><span className="tok-cmt">$ </span>cd /path/to/my-project</span>
          </div>
          <p style={{color:'var(--cream-mute)', fontSize:11.5, marginTop:14, marginBottom:0, fontFamily:'var(--mono)', letterSpacing:'0.04em'}}>
            // tools sandboxed to launch dir
          </p>
        </div>
        <div className="card" style={{padding:'32px 0 32px 28px'}}>
          <div style={{fontFamily:'var(--mono)', fontSize:11, letterSpacing:'0.14em', color:'var(--cream-mute)', textTransform:'uppercase', marginBottom: 18}}>03 — Run</div>
          <h3 style={{fontFamily:'var(--serif)', fontStyle:'italic', fontWeight:400, fontSize:26, letterSpacing:'-0.005em', margin:'0 0 14px', color:'var(--cream)'}}>启动 TUI 会话</h3>
          <div className="copy-block" style={{fontSize:13, maxWidth:'none'}}>
            <span className="cmd"><span className="tok-cmt">$ </span>npx reasonix code</span>
          </div>
          <p style={{color:'var(--cream-mute)', fontSize:11.5, marginTop:14, marginBottom:0, fontFamily:'var(--mono)', letterSpacing:'0.04em'}}>
            // 首次启动向导自动注入 Key
          </p>
        </div>
      </div>
    </section>
  );
}

window.Install = Install;
window.CopyCmd = CopyCmd;
