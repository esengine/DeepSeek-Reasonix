// Footer

function Footer() {
  const { lang } = useLang();
  return (
    <footer className="footer">
      <div className="footer-inner">
        <div>
          <a className="brand" href="#top" style={{textDecoration:'none', color:'inherit'}}>
            <span className="brand-mark"></span>
            <span className="brand-name">
              <b>DeepSeek-Reasonix</b>
            </span>
          </a>
          <p style={{color:'var(--cream-mute)', fontSize:13, marginTop:14, lineHeight:1.65, maxWidth:340}}>
            {t({
              zh: 'DeepSeek 原生的终端编程 Agent · 围绕 prefix-cache 稳定性设计 · 长会话长期跑。',
              en: 'DeepSeek-native AI coding agent for your terminal. Engineered around prefix-cache stability — leave it running.',
            }, lang)}
          </p>
          <div style={{display:'flex', gap:10, marginTop:18}}>
            <a className="btn btn-ghost btn-sm" href="https://github.com/esengine/DeepSeek-Reasonix" target="_blank" rel="noreferrer" aria-label="GitHub"><Ic.Github size={14}/></a>
            <a className="btn btn-ghost btn-sm" href="https://github.com/esengine/DeepSeek-Reasonix/discussions" target="_blank" rel="noreferrer">Discussions</a>
          </div>
        </div>
        <div>
          <h5>Product</h5>
          <ul>
            <li><a href="index.html#install">{t({ zh: 'CLI 安装', en: 'Install CLI' }, lang)}</a></li>
            <li><a href="download.html">{t({ zh: '桌面端', en: 'Desktop' }, lang)}</a></li>
            <li><a href="index.html#agents">{t({ zh: '三大支柱', en: 'Three pillars' }, lang)}</a></li>
            <li><a href="index.html#config">{t({ zh: '配置', en: 'Config' }, lang)}</a></li>
          </ul>
        </div>
        <div>
          <h5>Community</h5>
          <ul>
            <li><a href="https://github.com/esengine/DeepSeek-Reasonix" target="_blank" rel="noreferrer">GitHub</a></li>
            <li><a href="https://github.com/esengine/DeepSeek-Reasonix/discussions" target="_blank" rel="noreferrer">Discussions</a></li>
            <li><a href="https://github.com/esengine/DeepSeek-Reasonix/issues" target="_blank" rel="noreferrer">Issues</a></li>
            <li><a href="https://github.com/esengine/DeepSeek-Reasonix/blob/main/CONTRIBUTING.md" target="_blank" rel="noreferrer">Contributing</a></li>
          </ul>
        </div>
        <div>
          <h5>Resources</h5>
          <ul>
            <li><a href="https://github.com/esengine/DeepSeek-Reasonix#readme" target="_blank" rel="noreferrer">README</a></li>
            <li><a href="index.html#roadmap">Roadmap</a></li>
            <li><a href="https://github.com/esengine/DeepSeek-Reasonix/blob/main/CHANGELOG.md" target="_blank" rel="noreferrer">Changelog</a></li>
            <li><a href="https://platform.deepseek.com" target="_blank" rel="noreferrer">DeepSeek Platform</a></li>
          </ul>
        </div>
      </div>
      <div className="footer-bottom">
        <span>© 2026 esengine · MIT License</span>
        <span className="spacer"></span>
        <span>{t({
          zh: 'Independent open-source project · 与 DeepSeek 官方无关',
          en: 'Independent open-source project · not affiliated with DeepSeek',
        }, lang)}</span>
        <span style={{marginLeft:18}}>v{window.REASONIX_VERSION} · stable</span>
      </div>
    </footer>
  );
}

window.Footer = Footer;
