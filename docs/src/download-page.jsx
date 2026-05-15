// Dedicated /download page — hero + smart mirror grid + platform notes

const PLATFORM_NOTES = {
  mac: {
    title: 'macOS · Gatekeeper',
    en_label: 'first-launch unquarantine',
    desc: {
      zh: '安装包暂未代码签名，首次启动会被 Gatekeeper 拦下。任选其一：',
      en: 'Installers are not yet code-signed, so Gatekeeper blocks the first launch. Pick one:',
    },
    steps: [
      { cmd: 'xattr -dr com.apple.quarantine /Applications/Reasonix.app', note: { zh: '终端一行解除隔离属性', en: 'One-liner to clear the quarantine attribute' } },
      { cmd: 'right-click → Open → confirm', note: { zh: '在 Finder 中右键打开，确认一次后续不再询问', en: 'Right-click → Open → confirm once; macOS remembers afterwards' } },
    ],
  },
  win: {
    title: 'Windows · SmartScreen',
    en_label: '"unknown publisher" warning',
    desc: {
      zh: 'SmartScreen 会提示 "Unknown publisher"。需要：',
      en: 'SmartScreen flags the installer as "unknown publisher". Either:',
    },
    steps: [
      { cmd: 'More info → Run anyway', note: { zh: '点 "更多信息" 然后 "仍要运行" 即可', en: 'Click "More info", then "Run anyway"' } },
      { cmd: 'Get-AuthenticodeSignature .\\Reasonix_setup.exe', note: { zh: '可在 PowerShell 中校验文件 hash', en: 'Verify the file hash in PowerShell if you want' } },
    ],
  },
  linux: {
    title: 'Linux · AppImage',
    en_label: 'chmod +x · libfuse2',
    desc: {
      zh: 'AppImage 需要执行权限，部分发行版还要补 libfuse2：',
      en: 'AppImages need an executable bit; some distros also need libfuse2:',
    },
    steps: [
      { cmd: `chmod +x Reasonix_${window.REASONIX_VERSION}_amd64.AppImage`, note: { zh: '赋予可执行权限', en: 'Mark it executable' } },
      { cmd: 'sudo apt install libfuse2 # debian/ubuntu', note: { zh: 'AppImage 运行时依赖', en: 'AppImage runtime dependency' } },
    ],
  },
};

function PlatformNotes({ os }) {
  const { lang } = useLang();
  const n = PLATFORM_NOTES[os] || PLATFORM_NOTES.mac;
  return (
    <div className="platform-note">
      <div className="platform-note-head">
        <span className="platform-note-en">{n.en_label}</span>
        <h3>{n.title}</h3>
      </div>
      <p className="platform-note-desc">{t(n.desc, lang)}</p>
      <div className="platform-steps">
        {n.steps.map((s, i) => (
          <div key={i} className="platform-step">
            <div className="copy-block" style={{maxWidth:'none'}}>
              <span className="cmd"><span className="tok-cmt">$ </span>{s.cmd}</span>
            </div>
            <p>{t(s.note, lang)}</p>
          </div>
        ))}
      </div>
    </div>
  );
}

function DownloadHero() {
  const { lang } = useLang();
  return (
    <section className="dl-hero">
      <div className="hero-head">
        <span>§00 · Download</span>
        <span className="rule"></span>
        <span className="v">Reasonix Desktop · {window.REASONIX_VERSION}</span>
      </div>
      <div className="dl-hero-grid">
        <div>
          <h1 dangerouslySetInnerHTML={{ __html: t({
            zh: '桌面端，<em>与 CLI 同根</em>。',
            en: 'Desktop, <em>same loop as the CLI</em>.',
          }, lang) }}/>
          <p className="lede">
            {t({
              zh: <>原生 <b>Tauri</b> 客户端 · 自带 Node runtime · 共享 <b>~/.reasonix</b> 配置与会话。多 tab 并行，右侧栏列出当前会话读过和改过的文件，底部 cost / cache / token 实时表盘。</>,
              en: <>Native <b>Tauri</b> client · bundled Node runtime · shares <b>~/.reasonix</b> config + history with the CLI. Multi-tab sessions, side panel listing files read / edited this session, live cost / cache / token meters along the bottom.</>,
            }, lang)}
          </p>
          <p className="lede-foot">
            <span style={{color:'var(--accent)'}}>※</span>{' '}
            {t({
              zh: '当前为预发布版本 · 安装包暂未代码签名 · 见下方平台提示',
              en: 'Prerelease · installers are not yet code-signed · see platform notes below',
            }, lang)}
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
  const { lang } = useLang();
  return (
    <section className="section" id="cli-alt">
      <div className="sec-meta">
        <span className="sec-num">§02</span>
        <span>· {t({ zh: 'CLI 替代方案', en: 'Or just the CLI' }, lang)}</span>
        <span className="rule"></span>
      </div>
      <div className="section-head">
        <div className="section-head-text">
          <h2 className="section-title" dangerouslySetInnerHTML={{ __html: t({
            zh: '<em>不想装桌面？</em>一行 CLI 就够。',
            en: '<em>Prefer terminal?</em> One line is enough.',
          }, lang) }}/>
        </div>
        <p className="section-sub">
          {t({
            zh: '桌面端只是 CLI 的可视化伴侣。如果你日常就在终端里，直接 npx 拉起 reasonix code 即可，缓存策略、工具协议、记忆路径完全一致。',
            en: 'The desktop is just a visual front-end. If you live in the terminal, npx the CLI directly — same cache strategy, same tool protocol, same memory paths.',
          }, lang)}
        </p>
      </div>
      <div className="copy-block" style={{maxWidth: 640}}>
        <span className="cmd"><span className="tok-cmt">$ </span>cd /path/to/my-project &amp;&amp; npx reasonix code</span>
      </div>
      <a className="btn btn-ghost" href="index.html#install" style={{marginTop: 22}}>
        {t({ zh: '查看完整安装指引 →', en: 'Full install guide →' }, lang)}
      </a>
    </section>
  );
}

function DownloadPage() {
  const [os, setOs] = React.useState(detectOS);
  const { lang } = useLang();

  return (
    <>
      <Nav active="download"/>
      <DownloadHero/>

      <section className="section" id="mirror">
        <div className="sec-meta">
          <span className="sec-num">§01</span>
          <span>· {t({ zh: 'Smart Mirror · 自动测速', en: 'Smart mirror · auto probe' }, lang)}</span>
          <span className="rule"></span>
        </div>
        <div className="section-head">
          <div className="section-head-text">
            <h2 className="section-title" dangerouslySetInnerHTML={{ __html: t({
              zh: '两路并行 <em>探测</em>，自动择优。',
              en: 'Two mirrors, <em>probed in parallel</em>.',
            }, lang) }}/>
          </div>
          <p className="section-sub">
            {t({
              zh: '页面打开瞬间同时向 Cloudflare R2 与 GitHub Releases 发起 HEAD 请求，按 TTFB 排序，把最快的链路标记为 Fastest 推给你。R2 是 CN 主路径（CF 边缘），GitHub 是国际兜底。',
              en: 'On page load we fire HEAD requests at Cloudflare R2 and GitHub Releases, rank by TTFB, and tag the winner as "Fastest". R2 is the CN-friendly path via Cloudflare\'s edge; GitHub is the global fallback.',
            }, lang)}
          </p>
        </div>
        <MirrorGrid os={os} setOs={setOs}/>
      </section>

      <section className="section" id="platform">
        <div className="sec-meta">
          <span className="sec-num">§03</span>
          <span>· {t({ zh: '平台注意事项', en: 'Platform notes' }, lang)}</span>
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

ReactDOM.createRoot(document.getElementById('root')).render(
  <LangProvider><DownloadPage/></LangProvider>
);
