// Dedicated /download page — hero + smart mirror grid + platform notes

function PlatformNotes({ os }) {
  const NOTES = {
    mac: {
      title: 'macOS · Gatekeeper',
      en: 'first-launch unquarantine',
      desc: '安装包暂未代码签名，首次启动会被 Gatekeeper 拦下。任选其一：',
      steps: [
        { cmd: 'xattr -dr com.apple.quarantine /Applications/Reasonix.app', note: '终端一行解除隔离属性' },
        { cmd: 'right-click → Open → 确认', note: '在 Finder 中右键打开，确认一次后续不再询问' },
      ],
    },
    win: {
      title: 'Windows · SmartScreen',
      en: '"unknown publisher" warning',
      desc: 'SmartScreen 会提示 "Unknown publisher"。需要：',
      steps: [
        { cmd: '更多信息 → 仍要运行', note: 'Click "More info" then "Run anyway" 即可' },
        { cmd: 'Get-AuthenticodeSignature .\\Reasonix_setup.exe', note: '可在 PowerShell 中校验文件 hash' },
      ],
    },
    linux: {
      title: 'Linux · AppImage',
      en: 'chmod +x · libfuse2',
      desc: 'AppImage 需要执行权限，部分发行版还要补 libfuse2：',
      steps: [
        { cmd: 'chmod +x Reasonix_0.42.0-3_amd64.AppImage', note: '赋予可执行权限' },
        { cmd: 'sudo apt install libfuse2 # debian/ubuntu', note: 'AppImage 运行时依赖' },
      ],
    },
  };
  const n = NOTES[os] || NOTES.mac;
  return (
    <div className="platform-note">
      <div className="platform-note-head">
        <span className="platform-note-en">{n.en}</span>
        <h3>{n.title}</h3>
      </div>
      <p className="platform-note-desc">{n.desc}</p>
      <div className="platform-steps">
        {n.steps.map((s, i) => (
          <div key={i} className="platform-step">
            <div className="copy-block" style={{maxWidth:'none'}}>
              <span className="cmd"><span className="tok-cmt">$ </span>{s.cmd}</span>
            </div>
            <p>{s.note}</p>
          </div>
        ))}
      </div>
    </div>
  );
}

function DownloadHero({ os, setOs }) {
  return (
    <section className="dl-hero">
      <div className="hero-head">
        <span>§00 · Download</span>
        <span className="rule"></span>
        <span className="v">Reasonix Desktop · 0.42.0-3</span>
      </div>
      <div className="dl-hero-grid">
        <div>
          <h1>
            桌面端，<em>与 CLI 同根</em>。
          </h1>
          <p className="lede">
            原生 <b>Tauri</b> 客户端 · 自带 Node runtime · 共享 <b>~/.reasonix</b> 配置与会话。
            多 tab 并行，右侧栏列出当前会话读过和改过的文件，底部 cost / cache / token 实时表盘。
          </p>
          <p className="lede-foot">
            <span style={{color:'var(--accent)'}}>※</span> 当前为预发布版本 · 安装包暂未代码签名 · 见下方平台提示
          </p>
        </div>
        <div className="dl-hero-stats">
          <div className="hero-stat"><b>2</b><span>Mirrors</span></div>
          <div className="hero-stat"><b>3</b><span>Platforms</span></div>
          <div className="hero-stat"><b>Auto</b><span>Probe</span></div>
        </div>
      </div>
    </section>
  );
}

function CliAlt() {
  return (
    <section className="section" id="cli-alt">
      <div className="sec-meta">
        <span className="sec-num">§02</span>
        <span>· CLI 替代方案</span>
        <span className="rule"></span>
      </div>
      <div className="section-head">
        <div className="section-head-text">
          <h2 className="section-title">
            <em>不想装桌面？</em>一行 CLI 就够。
          </h2>
        </div>
        <p className="section-sub">
          桌面端只是 CLI 的可视化伴侣。如果你日常就在终端里，直接 npx 拉起 reasonix code 即可，
          缓存策略、工具协议、记忆路径完全一致。
        </p>
      </div>
      <div className="copy-block" style={{maxWidth: 640}}>
        <span className="cmd"><span className="tok-cmt">$ </span>cd /path/to/my-project &amp;&amp; npx reasonix code</span>
      </div>
      <a className="btn btn-ghost" href="index.html#install" style={{marginTop: 22}}>
        查看完整安装指引 →
      </a>
    </section>
  );
}

function DownloadPage() {
  const [os, setOs] = React.useState(detectOS);

  return (
    <>
      <Nav active="download"/>
      <DownloadHero os={os} setOs={setOs}/>

      <section className="section" id="mirror">
        <div className="sec-meta">
          <span className="sec-num">§01</span>
          <span>· Smart Mirror · 自动测速</span>
          <span className="rule"></span>
        </div>
        <div className="section-head">
          <div className="section-head-text">
            <h2 className="section-title">
              两路并行 <em>探测</em>，自动择优。
            </h2>
          </div>
          <p className="section-sub">
            页面打开瞬间同时向 Cloudflare R2 与 GitHub Releases 发起 HEAD 请求，按 TTFB 排序，
            把最快的链路标记为 Fastest 推给你。R2 是 CN 主路径（CF 边缘），GitHub 是国际兜底。
          </p>
        </div>
        <MirrorGrid os={os} setOs={setOs}/>
      </section>

      <section className="section" id="platform">
        <div className="sec-meta">
          <span className="sec-num">§03</span>
          <span>· 平台注意事项</span>
          <span className="rule"></span>
        </div>
        <div className="platform-tabs">
          {[
            { id: 'mac', label: 'macOS' },
            { id: 'win', label: 'Windows' },
            { id: 'linux', label: 'Linux' },
          ].map(p => (
            <button
              key={p.id}
              className={'platform-tab ' + (os === p.id ? 'on' : '')}
              onClick={() => setOs(p.id)}
            >
              {p.label}
            </button>
          ))}
        </div>
        <PlatformNotes os={os}/>
      </section>

      <CliAlt/>
      <Footer/>
    </>
  );
}

ReactDOM.createRoot(document.getElementById('root')).render(<DownloadPage/>);
