// Footer

function Footer() {
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
            DeepSeek-native AI coding agent for your terminal. Engineered around
            prefix-cache stability — leave it running.
          </p>
          <div style={{display:'flex', gap:10, marginTop:18}}>
            <a className="btn btn-ghost btn-sm" href="https://github.com/esengine/DeepSeek-Reasonix" target="_blank" rel="noreferrer" aria-label="GitHub"><Ic.Github size={14}/></a>
            <a className="btn btn-ghost btn-sm" href="https://github.com/esengine/DeepSeek-Reasonix/discussions" target="_blank" rel="noreferrer">Discussions</a>
          </div>
        </div>
        <div>
          <h5>Product</h5>
          <ul>
            <li><a href="index.html#install">CLI 安装</a></li>
            <li><a href="download.html">桌面端</a></li>
            <li><a href="index.html#agents">三大支柱</a></li>
            <li><a href="index.html#config">配置</a></li>
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
        <span>Independent open-source project · 与 DeepSeek 官方无关</span>
        <span style={{marginLeft:18}}>v0.42.0-3 · prerelease</span>
      </div>
    </footer>
  );
}

window.Footer = Footer;
